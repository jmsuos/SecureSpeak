package client

// Custom sounds and the leave message.
//
//   - Your own join/leave sound: others hear it when you connect to or leave
//     the server. You announce the SHA-256 of your sounds (end-to-end
//     encrypted, kind "sndann"); peers that don't have a sound yet ask you
//     for it ("sndreq") and you send it end-to-end encrypted to just them
//     ("sound"). Everyone keeps received sounds by hash in <data>/sounds.
//   - App sounds: replace SecureSpeak's own notification sounds locally.
//   - Leave message: sent end-to-end encrypted to everyone ("bye") right
//     before you disconnect, shown as "Alice left the server (message)".

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	MaxSoundSize   = 512 * 1024
	MaxLeaveMsgLen = 80
	peerSoundGrace = 15 * time.Second // play a join sound only this long after someone joined
)

// AppSoundNames are SecureSpeak's notification sounds that can be replaced.
var AppSoundNames = []string{"connect", "disconnect", "join", "leave", "move", "msg", "poke", "file", "mute", "unmute", "error"}

var soundHashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// SoundRef is a stored sound: content hash plus the original file name.
type SoundRef struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
}

type soundAnnounce struct {
	Join  string `json:"join,omitempty"`
	Leave string `json:"leave,omitempty"`
}

type soundData struct {
	Hash string `json:"hash"`
	Data []byte `json:"data,omitempty"`
}

// SoundMime returns the content type of a WAV or MP3 file, or "".
func SoundMime(b []byte) string {
	switch {
	case len(b) > 12 && bytes.HasPrefix(b, []byte("RIFF")) && string(b[8:12]) == "WAVE":
		return "audio/wav"
	case len(b) > 3 && bytes.HasPrefix(b, []byte("ID3")):
		return "audio/mpeg"
	case len(b) > 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0:
		return "audio/mpeg"
	}
	return ""
}

func validSound(b []byte) bool { return len(b) <= MaxSoundSize && SoundMime(b) != "" }

func soundHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (c *Client) soundDir() string { return filepath.Join(c.store.Dir(), "sounds") }

func (c *Client) haveSound(hash string) bool {
	if !soundHashRe.MatchString(hash) {
		return false
	}
	_, err := os.Stat(filepath.Join(c.soundDir(), hash))
	return err == nil
}

func (c *Client) storeSound(b []byte) (string, error) {
	if !validSound(b) {
		return "", errors.New("sounds must be a .wav or .mp3 file of at most 512 KB")
	}
	h := soundHash(b)
	if c.haveSound(h) {
		return h, nil
	}
	if err := os.MkdirAll(c.soundDir(), 0700); err != nil {
		return "", err
	}
	p := filepath.Join(c.soundDir(), h)
	if err := os.WriteFile(p+".tmp", b, 0600); err != nil {
		return "", err
	}
	return h, os.Rename(p+".tmp", p)
}

// SoundFile returns a stored sound by hash (served to the UI at /sound).
func (c *Client) SoundFile(hash string) []byte {
	if !soundHashRe.MatchString(hash) {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(c.soundDir(), hash))
	if err != nil || soundHash(b) != hash {
		return nil
	}
	return b
}

// setSound handles "sound_set": slot is "join", "leave" or "app:<name>".
// Empty data removes the custom sound.
func (c *Client) setSound(slot, name string, data []byte) error {
	var ref *SoundRef
	if len(data) > 0 {
		h, err := c.storeSound(data)
		if err != nil {
			return err
		}
		name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
		if r := []rune(name); len(r) > 60 {
			name = string(r[:60])
		}
		ref = &SoundRef{Hash: h, Name: name}
	}
	c.mu.Lock()
	switch {
	case slot == "join":
		c.cfg.JoinSound = ref
	case slot == "leave":
		c.cfg.LeaveSound = ref
	case strings.HasPrefix(slot, "app:"):
		n := slot[4:]
		known := false
		for _, x := range AppSoundNames {
			known = known || x == n
		}
		if !known {
			c.mu.Unlock()
			return errors.New("unknown sound")
		}
		if c.cfg.AppSounds == nil {
			c.cfg.AppSounds = map[string]SoundRef{}
		}
		if ref == nil {
			delete(c.cfg.AppSounds, n)
		} else {
			c.cfg.AppSounds[n] = *ref
		}
	default:
		c.mu.Unlock()
		return errors.New("unknown sound")
	}
	c.saveLocked()
	if c.sess != nil && (slot == "join" || slot == "leave") {
		c.announceSoundsLocked(c.sess, nil)
	}
	c.mu.Unlock()
	c.emitState()
	return nil
}

func (c *Client) setLeaveMessage(text string) error {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if r := []rune(text); len(r) > MaxLeaveMsgLen {
		return fmt.Errorf("the leave message can be at most %d characters", MaxLeaveMsgLen)
	}
	c.mu.Lock()
	c.cfg.LeaveMessage = text
	c.saveLocked()
	c.mu.Unlock()
	c.emitState()
	return nil
}

// recipsLocked returns pairwise keys for the given peers (nil = everyone).
func recipsLocked(s *session, to []uint32) map[uint32][]byte {
	recips := map[uint32][]byte{}
	for id, p := range s.peers {
		if !p.Valid {
			continue
		}
		if to == nil {
			recips[id] = p.pairwise
			continue
		}
		for _, t := range to {
			if t == id {
				recips[id] = p.pairwise
			}
		}
	}
	return recips
}

// announceSoundsLocked tells peers (nil = everyone) which join/leave sounds we use.
func (c *Client) announceSoundsLocked(s *session, to []uint32) {
	a := soundAnnounce{}
	if c.cfg.JoinSound != nil {
		a.Join = c.cfg.JoinSound.Hash
	}
	if c.cfg.LeaveSound != nil {
		a.Leave = c.cfg.LeaveSound.Hash
	}
	if a.Join == "" && a.Leave == "" && to != nil {
		return
	}
	c.sendEncryptedLocked(s, "sndann", 0, chatBody{Sounds: &a}, recipsLocked(s, to))
}

// receiveSoundMsgLocked handles sndann / sndreq / sound / bye from a peer.
func (c *Client) receiveSoundMsgLocked(s *session, p *Peer, kind string, b chatBody) {
	switch kind {
	case "sndann":
		if b.Sounds == nil {
			return
		}
		p.sndJoin, p.sndLeave = cleanHash(b.Sounds.Join), cleanHash(b.Sounds.Leave)
		for _, h := range []string{p.sndJoin, p.sndLeave} {
			if h != "" && !c.haveSound(h) && !p.sndAsked[h] {
				if p.sndAsked == nil {
					p.sndAsked = map[string]bool{}
				}
				p.sndAsked[h] = true
				c.sendEncryptedLocked(s, "sndreq", p.Info.ID, chatBody{SoundReq: &soundData{Hash: h}}, recipsLocked(s, []uint32{p.Info.ID}))
			}
		}
		c.maybePlayJoinLocked(p)
	case "sndreq":
		if b.SoundReq == nil {
			return
		}
		h := b.SoundReq.Hash
		mine := (c.cfg.JoinSound != nil && c.cfg.JoinSound.Hash == h) || (c.cfg.LeaveSound != nil && c.cfg.LeaveSound.Hash == h)
		if !mine || p.sndSent[h] {
			return // only our own announced sounds, once per person
		}
		if p.sndSent == nil {
			p.sndSent = map[string]bool{}
		}
		p.sndSent[h] = true
		if data := c.SoundFile(h); data != nil {
			c.sendEncryptedLocked(s, "sound", p.Info.ID, chatBody{Sound: &soundData{Hash: h, Data: data}}, recipsLocked(s, []uint32{p.Info.ID}))
		}
	case "sound":
		if b.Sound == nil || !p.sndAsked[b.Sound.Hash] || soundHash(b.Sound.Data) != b.Sound.Hash {
			return
		}
		if _, err := c.storeSound(b.Sound.Data); err == nil {
			c.maybePlayJoinLocked(p)
		}
	case "bye":
		text := strings.TrimSpace(strings.ReplaceAll(b.Text, "\n", " "))
		if r := []rune(text); len(r) > MaxLeaveMsgLen {
			text = string(r[:MaxLeaveMsgLen])
		}
		p.leaveMsg = text
	}
}

func cleanHash(h string) string {
	if soundHashRe.MatchString(h) {
		return h
	}
	return ""
}

// maybePlayJoinLocked plays a peer's join sound once, shortly after they joined.
func (c *Client) maybePlayJoinLocked(p *Peer) {
	if p.joinedAt.IsZero() || p.joinPlayed || p.sndJoin == "" || time.Since(p.joinedAt) > peerSoundGrace || !c.haveSound(p.sndJoin) {
		return
	}
	p.joinPlayed = true
	c.emitPeerSoundLocked(p, p.sndJoin)
}

func (c *Client) emitPeerSoundLocked(p *Peer, hash string) {
	if c.cfg.NoPeerSounds || !c.cfg.Audio.Sounds || c.deaf || p.localMute {
		return
	}
	c.em.Emit("peer_sound", map[string]any{"id": p.Info.ID, "hash": hash})
}

// sayGoodbye sends our leave message to everyone and waits briefly until it
// has been written, so it arrives before the connection closes.
func (c *Client) sayGoodbye(s *session) {
	c.mu.Lock()
	msg := c.cfg.LeaveMessage
	if msg == "" || c.sess != s {
		c.mu.Unlock()
		return
	}
	c.sendEncryptedLocked(s, "bye", 0, chatBody{Text: msg}, recipsLocked(s, nil))
	c.mu.Unlock()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for len(s.out) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond) // let the writer flush
}
