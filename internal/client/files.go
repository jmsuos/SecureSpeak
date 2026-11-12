package client

// End-to-end encrypted file transfer.
//
//  1. The sender copies the file to its outbox and posts an offer (name, size,
//     SHA-256) inside a normal encrypted chat message (channel or private).
//  2. A recipient who clicks Download sends a pairwise-encrypted "req".
//  3. The sender answers with "start" containing a fresh random AES-256 key
//     for this transfer and recipient, then streams 48 KiB chunks encrypted
//     with AES-256-GCM (nonce = chunk index, bound to the transfer id).
//  4. The recipient acknowledges progress (flow control: max 32 chunks in
//     flight), checks the SHA-256 at the end and reports "done".
//
// The server only relays opaque chunks and never stores anything.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"securespeak/internal/e2e"
	"securespeak/internal/platform"
	"securespeak/internal/proto"
)

const (
	MaxFileSize = 4 << 30 // 4 GiB
	fileWindow  = 32
)

type fileOffer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 []byte `json:"sha256"`
	Mime   string `json:"mime,omitempty"`
}

// Media up to these sizes is loaded automatically so it can be previewed.
const (
	maxAutoImage = 16 << 20
	maxAutoVideo = 64 << 20
)

var mimeRe = regexp.MustCompile(`^[a-z]+/[a-z0-9.+-]{1,80}$`)

func cleanMime(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	if !mimeRe.MatchString(m) {
		return ""
	}
	return m
}

func autoPreview(f *fileOffer) bool {
	switch {
	case strings.HasPrefix(f.Mime, "image/") && f.Mime != "image/svg+xml":
		return f.Size <= maxAutoImage
	case strings.HasPrefix(f.Mime, "video/"):
		return f.Size <= maxAutoVideo
	}
	return false
}

type outFile struct {
	fileOffer
	path   string
	recips map[uint32]bool
}

type sendXfer struct {
	id, name string
	to       uint32
	acked    atomic.Int64
	ackCh    chan struct{}
	cancel   chan struct{}
	once     sync.Once
	finished atomic.Bool
}

func (x *sendXfer) stop() { x.once.Do(func() { close(x.cancel) }) }

type remoteOffer struct {
	fileOffer
	from uint32
}

type recvXfer struct {
	offer     *remoteOffer
	cache     bool   // auto-loaded media preview (kept in the cache folder)
	saved     string // where a cached file was saved by the user
	withdrawn bool   // the sender withdrew the file
	state     string // requesting | downloading | verifying | done | failed
	fc        *e2e.FileCipher
	chunks    int
	got       []bool
	count     int
	f         *os.File
	part      string
	final     string
	lastEmit  time.Time
}

type fileCtl struct {
	Op     string `json:"op"` // req | start | ack | done | cancel | withdraw
	ID     string `json:"id"`
	Key    []byte `json:"key,omitempty"`
	Chunks int    `json:"chunks,omitempty"`
	N      int    `json:"n,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func chunkCount(size int64) int {
	return int((size + proto.FileChunkSize - 1) / proto.FileChunkSize)
}

func randomID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// SanitizeFileName turns an untrusted name into a safe base name.
func SanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if r := []rune(name); len(r) > 150 {
		ext := filepath.Ext(name)
		if len([]rune(ext)) > 20 {
			ext = ""
		}
		name = string(r[:150-len([]rune(ext))]) + ext
	}
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "LPT1", "LPT2", "LPT3":
		name = "_" + name
	}
	if name == "" {
		name = "file"
	}
	return name
}

func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		_, err1 := os.Stat(p)
		_, err2 := os.Stat(p + ".part")
		if errors.Is(err1, os.ErrNotExist) && errors.Is(err2, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
}

func (c *Client) downloadDirLocked() string {
	if c.cfg.DownloadDir != "" {
		return c.cfg.DownloadDir
	}
	return filepath.Join(platform.DownloadDir(), "SecureSpeak")
}

func (c *Client) emitFile(ev map[string]any) { c.em.Emit("file", ev) }

// OfferFile stores a copy of the upload and offers it in the chat.
func (c *Client) OfferFile(kind string, target uint32, name, mime string, r io.Reader) error {
	mime = cleanMime(mime)
	if kind != "channel" && kind != "private" {
		return errors.New("files can be sent to a channel or a user")
	}
	name = SanitizeFileName(name)
	c.mu.Lock()
	if c.sess == nil {
		c.mu.Unlock()
		return errors.New("not connected")
	}
	c.mu.Unlock()

	id := randomID()
	dir := c.store.OutboxDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, MaxFileSize+1))
	f.Close()
	if err == nil && n > MaxFileSize {
		err = fmt.Errorf("file too large (max %d GiB)", MaxFileSize>>30)
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	off := &outFile{fileOffer: fileOffer{ID: id, Name: name, Size: n, SHA256: h.Sum(nil), Mime: mime}, path: path}
	return c.withSession(func(s *session) error {
		tgt, recips, err := c.sendBodyLocked(s, kind, target, chatBody{File: &off.fileOffer})
		if err != nil {
			os.Remove(path)
			return err
		}
		off.recips = recips
		s.offers[id] = off
		c.em.Emit("chat", map[string]any{
			"kind": kind, "from": s.myID, "fromNick": s.me.Nick, "target": tgt, "ts": time.Now().UnixMilli(),
			"self": true, "recipients": len(recips),
			"file": map[string]any{"id": id, "name": name, "size": n, "mime": mime, "own": true},
		})
		return nil
	})
}

func (c *Client) registerOfferLocked(s *session, p *Peer, f *fileOffer) map[string]any {
	if len(f.ID) == 0 || len(f.ID) > 64 || f.Size < 0 || f.Size > MaxFileSize || len(f.SHA256) != 32 {
		return nil
	}
	ro := &remoteOffer{fileOffer: *f, from: p.Info.ID}
	ro.Name = SanitizeFileName(f.Name)
	ro.Mime = cleanMime(f.Mime)
	s.remoteOffers[f.ID] = ro
	auto := !c.cfg.NoAutoMedia && autoPreview(&ro.fileOffer)
	if auto && s.recvs[f.ID] == nil {
		c.requestLocked(s, ro, true)
	}
	return map[string]any{"id": f.ID, "name": ro.Name, "size": f.Size, "mime": ro.Mime, "auto": auto}
}

func (c *Client) sendCtlLocked(s *session, to uint32, m fileCtl) {
	p := s.peers[to]
	if p == nil || !p.Valid {
		return
	}
	raw, _ := json.Marshal(m)
	blob, err := e2e.Seal(p.pairwise, raw, e2e.CtlAAD(s.myID, to))
	if err != nil {
		return
	}
	s.send(proto.TFileCtl, proto.FileCtl{To: to, Blob: blob})
}

// DownloadFile requests a file that was offered to us.
func (c *Client) DownloadFile(id string) error {
	return c.withSession(func(s *session) error {
		ro := s.remoteOffers[id]
		if ro == nil {
			return errors.New("this file is no longer available")
		}
		p := s.peers[ro.from]
		if p == nil {
			return errors.New("the sender is no longer online")
		}
		if rx := s.recvs[id]; rx != nil && (rx.state == "requesting" || rx.state == "downloading" || rx.state == "verifying") {
			return nil
		}
		c.requestLocked(s, ro, false)
		return nil
	})
}

func (c *Client) requestLocked(s *session, ro *remoteOffer, cache bool) {
	s.recvs[ro.ID] = &recvXfer{offer: ro, state: "requesting", cache: cache}
	c.sendCtlLocked(s, ro.from, fileCtl{Op: "req", ID: ro.ID})
	nick := ""
	if p := s.peers[ro.from]; p != nil {
		nick = p.Info.Nick
	}
	c.emitFile(map[string]any{"id": ro.ID, "dir": "in", "state": "requesting", "name": ro.Name, "size": ro.Size, "peer": nick, "cached": cache})
}

// MediaFile returns the local path of a file we offered or finished
// receiving, for previews.
func (c *Client) MediaFile(id string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sess
	if s == nil {
		return "", false
	}
	if off := s.offers[id]; off != nil {
		return off.path, true
	}
	if rx := s.recvs[id]; rx != nil && rx.state == "done" && !rx.withdrawn {
		return rx.final, true
	}
	return "", false
}

// SaveCachedFile copies an auto-loaded preview into the download folder.
func (c *Client) SaveCachedFile(id string) (any, error) {
	c.mu.Lock()
	s := c.sess
	var rx *recvXfer
	if s != nil {
		rx = s.recvs[id]
	}
	if rx == nil || rx.state != "done" {
		c.mu.Unlock()
		return nil, errors.New("the file is not available")
	}
	if !rx.cache || rx.saved != "" {
		p := rx.final
		if rx.saved != "" {
			p = rx.saved
		}
		c.mu.Unlock()
		return map[string]any{"path": p}, nil
	}
	dir := c.downloadDirLocked()
	src, name := rx.final, rx.offer.Name
	c.mu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	dst := uniquePath(dir, name)
	in, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	rx.saved = dst
	c.mu.Unlock()
	c.emitFile(map[string]any{"id": id, "dir": "in", "state": "done", "name": name, "size": rx.offer.Size, "bytes": rx.offer.Size, "path": dst, "cached": true, "saved": true})
	return map[string]any{"path": dst}, nil
}

// CancelFile withdraws a file we offered (telling every recipient), or
// cancels an incoming download.
func (c *Client) CancelFile(id string) error {
	return c.withSession(func(s *session) error {
		if off := s.offers[id]; off != nil {
			c.withdrawLocked(s, off)
			return nil
		}
		if rx := s.recvs[id]; rx != nil && rx.state != "done" && rx.state != "failed" {
			c.sendCtlLocked(s, rx.offer.from, fileCtl{Op: "cancel", ID: id, Reason: "cancelled by receiver"})
			c.failRecvLocked(s, rx, "cancelled")
		}
		for k, x := range s.sends {
			if x.id == id {
				c.sendCtlLocked(s, x.to, fileCtl{Op: "cancel", ID: id, Reason: "cancelled by sender"})
				x.stop()
				delete(s.sends, k)
			}
		}
		return nil
	})
}

// withdrawLocked stops all transfers of an offer, deletes our copy and tells
// everyone it was offered to (end-to-end encrypted, like all file control).
func (c *Client) withdrawLocked(s *session, off *outFile) {
	for k, x := range s.sends {
		if x.id == off.ID {
			x.stop()
			delete(s.sends, k)
		}
	}
	for to := range off.recips {
		c.sendCtlLocked(s, to, fileCtl{Op: "withdraw", ID: off.ID})
	}
	delete(s.offers, off.ID)
	os.Remove(off.path)
	c.emitFile(map[string]any{"id": off.ID, "dir": "out", "state": "withdrawn", "name": off.Name})
}

// DownloadedPath returns where a finished download was saved.
func (c *Client) DownloadedPath(id string) (string, error) {
	var p string
	err := c.withSession(func(s *session) error {
		rx := s.recvs[id]
		if rx == nil || rx.state != "done" {
			return errors.New("file not downloaded")
		}
		p = rx.final
		if rx.cache {
			if rx.saved == "" {
				return errors.New("save the file first")
			}
			p = rx.saved
		}
		return nil
	})
	return p, err
}

func (c *Client) failRecvLocked(s *session, rx *recvXfer, reason string) {
	if rx.f != nil {
		rx.f.Close()
		os.Remove(rx.part)
		rx.f = nil
	}
	rx.state = "failed"
	c.emitFile(map[string]any{"id": rx.offer.ID, "dir": "in", "state": "failed", "name": rx.offer.Name, "error": reason})
}

func (c *Client) handleFileCtlLocked(s *session, fc proto.FileCtl) {
	p := s.peers[fc.From]
	if p == nil || !p.Valid {
		return
	}
	raw, err := e2e.Open(p.pairwise, fc.Blob, e2e.CtlAAD(fc.From, s.myID))
	if err != nil {
		return
	}
	var m fileCtl
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	switch m.Op {
	case "req":
		off := s.offers[m.ID]
		if off == nil || !off.recips[fc.From] {
			c.sendCtlLocked(s, fc.From, fileCtl{Op: "cancel", ID: m.ID, Reason: "file is no longer offered"})
			return
		}
		k := m.ID + "/" + fmt.Sprint(fc.From)
		if s.sends[k] != nil {
			return
		}
		x := &sendXfer{id: m.ID, name: off.Name, to: fc.From, ackCh: make(chan struct{}, 1), cancel: make(chan struct{})}
		s.sends[k] = x
		go c.sendFile(s, off, x, p.Info.Nick)
	case "start":
		rx := s.recvs[m.ID]
		if rx == nil || rx.offer.from != fc.From || rx.state != "requesting" {
			return
		}
		if m.Chunks != chunkCount(rx.offer.Size) {
			c.failRecvLocked(s, rx, "sender announced a wrong size")
			return
		}
		fcph, err := e2e.NewFileCipher(m.Key)
		if err != nil {
			c.failRecvLocked(s, rx, "bad key")
			return
		}
		dir := c.downloadDirLocked()
		if rx.cache {
			dir = c.store.CacheDir()
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			c.failRecvLocked(s, rx, err.Error())
			return
		}
		rx.final = uniquePath(dir, rx.offer.Name)
		rx.part = rx.final + ".part"
		f, err := os.OpenFile(rx.part, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0600)
		if err != nil {
			c.failRecvLocked(s, rx, err.Error())
			return
		}
		rx.f, rx.fc, rx.chunks, rx.got, rx.state = f, fcph, m.Chunks, make([]bool, m.Chunks), "downloading"
		c.emitFile(map[string]any{"id": m.ID, "dir": "in", "state": "downloading", "name": rx.offer.Name, "size": rx.offer.Size, "bytes": 0, "peer": p.Info.Nick})
		if rx.chunks == 0 {
			c.finishRecvLocked(s, rx)
		}
	case "ack", "done":
		x := s.sends[m.ID+"/"+fmt.Sprint(fc.From)]
		if x == nil {
			return
		}
		if m.Op == "done" {
			x.finished.Store(true)
			x.acked.Store(int64(1 << 40))
		} else if int64(m.N) > x.acked.Load() {
			x.acked.Store(int64(m.N))
		}
		select {
		case x.ackCh <- struct{}{}:
		default:
		}
	case "withdraw":
		ro := s.remoteOffers[m.ID]
		if ro == nil || ro.from != fc.From {
			return
		}
		delete(s.remoteOffers, m.ID)
		kept := false
		if rx := s.recvs[m.ID]; rx != nil {
			switch {
			case rx.state == "done" && rx.cache:
				os.Remove(rx.final) // auto-loaded preview: remove it
				kept = rx.saved != ""
			case rx.state == "done":
				kept = true // the user already downloaded it; that copy is theirs
			case rx.f != nil:
				rx.f.Close()
				os.Remove(rx.part)
				rx.f = nil
			}
			rx.withdrawn = true
			if !kept {
				rx.state = "withdrawn"
			}
		}
		c.emitFile(map[string]any{"id": m.ID, "dir": "in", "state": "withdrawn", "name": ro.Name, "kept": kept})
	case "cancel":
		if rx := s.recvs[m.ID]; rx != nil && rx.offer.from == fc.From && (rx.state == "requesting" || rx.state == "downloading") {
			c.failRecvLocked(s, rx, m.Reason)
		}
		k := m.ID + "/" + fmt.Sprint(fc.From)
		if x := s.sends[k]; x != nil {
			x.stop()
			delete(s.sends, k)
			c.emitFile(map[string]any{"id": m.ID, "dir": "out", "state": "failed", "name": x.name, "peer": p.Info.Nick, "peerId": fc.From, "error": m.Reason})
		}
	}
}

func (c *Client) handleFileDataLocked(s *session, fd proto.FileData) {
	rx := s.recvs[fd.ID]
	if rx == nil || rx.state != "downloading" || rx.offer.from != fd.From || fd.Idx < 0 || fd.Idx >= rx.chunks || rx.got[fd.Idx] {
		return
	}
	pt, err := rx.fc.Open(fd.ID, uint32(fd.Idx), fd.Data)
	if err != nil {
		c.sendCtlLocked(s, fd.From, fileCtl{Op: "cancel", ID: fd.ID, Reason: "corrupted chunk"})
		c.failRecvLocked(s, rx, "a chunk failed authentication")
		return
	}
	want := int64(proto.FileChunkSize)
	if fd.Idx == rx.chunks-1 {
		want = rx.offer.Size - int64(fd.Idx)*proto.FileChunkSize
	}
	if int64(len(pt)) != want {
		c.failRecvLocked(s, rx, "wrong chunk size")
		return
	}
	if _, err := rx.f.WriteAt(pt, int64(fd.Idx)*proto.FileChunkSize); err != nil {
		c.sendCtlLocked(s, fd.From, fileCtl{Op: "cancel", ID: fd.ID, Reason: "receiver could not write the file"})
		c.failRecvLocked(s, rx, err.Error())
		return
	}
	rx.got[fd.Idx] = true
	rx.count++
	if rx.count%8 == 0 || rx.count == rx.chunks {
		c.sendCtlLocked(s, fd.From, fileCtl{Op: "ack", ID: fd.ID, N: rx.count})
	}
	if time.Since(rx.lastEmit) > 200*time.Millisecond {
		rx.lastEmit = time.Now()
		c.emitFile(map[string]any{"id": fd.ID, "dir": "in", "state": "downloading", "name": rx.offer.Name, "size": rx.offer.Size, "bytes": min(rx.offer.Size, int64(rx.count)*proto.FileChunkSize)})
	}
	if rx.count == rx.chunks {
		c.finishRecvLocked(s, rx)
	}
}

func (c *Client) finishRecvLocked(s *session, rx *recvXfer) {
	rx.state = "verifying"
	f := rx.f
	rx.f = nil
	id, from := rx.offer.ID, rx.offer.from
	if c.closing { // shutting down: drop the unfinished download
		f.Close()
		os.Remove(rx.part)
		rx.state = "failed"
		return
	}
	c.bg.Add(1)
	go func() {
		defer c.bg.Done()
		h := sha256.New()
		f.Seek(0, io.SeekStart)
		_, err := io.Copy(h, f)
		f.Close()
		ok := err == nil && string(h.Sum(nil)) == string(rx.offer.SHA256)
		if ok {
			err = os.Rename(rx.part, rx.final)
			ok = err == nil
		} else {
			os.Remove(rx.part)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if rx.withdrawn { // withdrawn by the sender while we were verifying
			if ok {
				os.Remove(rx.final)
			}
			return
		}
		if !ok {
			rx.state = "failed"
			msg := "checksum mismatch"
			if err != nil {
				msg = err.Error()
			}
			c.emitFile(map[string]any{"id": id, "dir": "in", "state": "failed", "name": rx.offer.Name, "error": msg})
			if c.sess == s {
				c.sendCtlLocked(s, from, fileCtl{Op: "cancel", ID: id, Reason: "receiver: " + msg})
			}
			return
		}
		rx.state = "done"
		ev := map[string]any{"id": id, "dir": "in", "state": "done", "name": rx.offer.Name, "size": rx.offer.Size, "bytes": rx.offer.Size, "cached": rx.cache}
		if !rx.cache {
			ev["path"] = rx.final
		}
		c.emitFile(ev)
		if c.sess == s {
			c.sendCtlLocked(s, from, fileCtl{Op: "done", ID: id})
		}
	}()
}

func (c *Client) sendFile(s *session, off *outFile, x *sendXfer, peerNick string) {
	key := e2e.RandomKey()
	fc, _ := e2e.NewFileCipher(key)
	chunks := chunkCount(off.Size)
	ev := func(state string, sent int64, errMsg string) {
		e := map[string]any{"id": off.ID, "dir": "out", "state": state, "name": off.Name, "size": off.Size, "bytes": sent, "peer": peerNick, "peerId": x.to}
		if errMsg != "" {
			e["error"] = errMsg
		}
		c.emitFile(e)
	}
	fail := func(reason string) {
		c.mu.Lock()
		if c.sess == s {
			c.sendCtlLocked(s, x.to, fileCtl{Op: "cancel", ID: off.ID, Reason: reason})
			delete(s.sends, off.ID+"/"+fmt.Sprint(x.to))
		}
		c.mu.Unlock()
		ev("failed", 0, reason)
	}
	f, err := os.Open(off.path)
	if err != nil {
		fail("file no longer available")
		return
	}
	defer f.Close()
	c.mu.Lock()
	if c.sess != s {
		c.mu.Unlock()
		return
	}
	c.sendCtlLocked(s, x.to, fileCtl{Op: "start", ID: off.ID, Key: key, Chunks: chunks})
	c.mu.Unlock()
	ev("sending", 0, "")

	buf := make([]byte, proto.FileChunkSize)
	last := time.Now()
	for idx := 0; idx < chunks; idx++ {
		for int64(idx)-x.acked.Load() >= fileWindow {
			select {
			case <-x.ackCh:
			case <-x.cancel:
				return
			case <-s.done:
				return
			case <-time.After(60 * time.Second):
				fail("receiver stopped responding")
				return
			}
		}
		select {
		case <-x.cancel:
			return
		default:
		}
		n, err := f.ReadAt(buf, int64(idx)*proto.FileChunkSize)
		if err != nil && err != io.EOF {
			fail("read error")
			return
		}
		ct := fc.Seal(off.ID, uint32(idx), buf[:n])
		if !s.sendBlocking(proto.TFileData, proto.FileData{To: x.to, ID: off.ID, Idx: idx, Data: ct}) {
			return
		}
		if time.Since(last) > 250*time.Millisecond {
			last = time.Now()
			ev("sending", min(off.Size, x.acked.Load()*proto.FileChunkSize), "")
		}
	}
	// wait for the receiver's verification
	deadline := time.After(10 * time.Minute)
	for !x.finished.Load() {
		select {
		case <-x.ackCh:
		case <-x.cancel:
			return
		case <-s.done:
			return
		case <-deadline:
			fail("receiver did not confirm")
			return
		}
	}
	c.mu.Lock()
	delete(s.sends, off.ID+"/"+fmt.Sprint(x.to))
	c.mu.Unlock()
	ev("done", off.Size, "")
}

// peerGoneFilesLocked aborts transfers with a user who left.
func (c *Client) peerGoneFilesLocked(s *session, id uint32) {
	for k, x := range s.sends {
		if x.to == id {
			x.stop()
			delete(s.sends, k)
		}
	}
	for _, rx := range s.recvs {
		if rx.offer.from == id && (rx.state == "requesting" || rx.state == "downloading") {
			c.failRecvLocked(s, rx, "the sender went offline")
		}
	}
	for fid, ro := range s.remoteOffers {
		if ro.from == id {
			delete(s.remoteOffers, fid)
		}
	}
}

func (c *Client) cleanupFilesLocked(s *session) {
	for _, x := range s.sends {
		x.stop()
	}
	for _, rx := range s.recvs {
		if rx.state == "requesting" || rx.state == "downloading" {
			c.failRecvLocked(s, rx, "disconnected")
		}
	}
	for _, off := range s.offers {
		os.Remove(off.path)
	}
}
