package client

// Avatars are sent end-to-end encrypted like chat messages (kind "avatar"),
// so the server never sees them. Only JPEG, PNG and WebP are accepted; the UI
// resizes pictures to 128x128 before upload.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const MaxAvatarSize = 48 * 1024

type avatarBody struct {
	Data []byte `json:"data"`
}

func validImage(b []byte) bool {
	if len(b) < 12 || len(b) > MaxAvatarSize {
		return false
	}
	return bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) || // JPEG
		bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) || // PNG
		(bytes.HasPrefix(b, []byte("RIFF")) && string(b[8:12]) == "WEBP")
}

func imageHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// ImageMime returns the content type for a validated image.
func ImageMime(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8}):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("\x89PNG")):
		return "image/png"
	default:
		return "image/webp"
	}
}

// sendAvatarLocked sends our avatar to the given users (nil = everyone).
func (c *Client) sendAvatarLocked(s *session, to []uint32) {
	if c.avatar == nil && to != nil {
		return // nothing to tell newcomers
	}
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
	c.sendEncryptedLocked(s, "avatar", 0, chatBody{Avatar: &avatarBody{Data: c.avatar}}, recips)
}

// SetAvatar replaces (or with nil, removes) our avatar and tells everyone.
func (c *Client) SetAvatar(b []byte) error {
	if b != nil && !validImage(b) {
		return errors.New("avatar must be a JPEG, PNG or WebP image under 48 KB")
	}
	if err := c.store.SaveAvatar(b); err != nil {
		return err
	}
	c.mu.Lock()
	c.avatar = b
	c.avatarHash = ""
	if b != nil {
		c.avatarHash = imageHash(b)
	}
	if c.sess != nil {
		c.sendAvatarLocked(c.sess, nil)
	}
	c.mu.Unlock()
	c.emitState()
	return nil
}

// Avatar returns the picture of a user (0 or our own id = ourselves).
func (c *Client) Avatar(id uint32) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == 0 || (c.sess != nil && id == c.sess.myID) {
		return c.avatar
	}
	if c.sess != nil {
		if p := c.sess.peers[id]; p != nil {
			return p.avatar
		}
	}
	return nil
}
