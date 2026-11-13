package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"

	"securespeak/internal/proto"
)

// Channel icons are public channel metadata, like channel names: the server
// stores them. Built-in icons are referenced by name ("std:home"); custom
// icons are PNGs of at most 64×64 pixels, stored by SHA-256 in
// <data>/channel-icons and fetched by clients on demand.

var stdIconRe = regexp.MustCompile(`^std:[a-z0-9-]{1,24}$`)
var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *Server) iconDir() string { return filepath.Join(s.dataDir, "channel-icons") }

// sanitizeIcon decodes an uploaded PNG and re-encodes it, so only plain
// pixels (with their transparency) are stored and passed on.
func sanitizeIcon(b []byte) ([]byte, error) {
	if len(b) == 0 || len(b) > proto.MaxIconBytes {
		return nil, errIcon("the icon file is too large (max 64 KB)")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, errIcon("the icon must be a PNG image")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > proto.MaxIconSize || cfg.Height > proto.MaxIconSize {
		return nil, errIcon("the icon must be at most 64×64 pixels")
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, errIcon("the icon must be a PNG image")
	}
	var out bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&out, toNRGBA(img)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

type errIcon string

func (e errIcon) Error() string { return string(e) }

func (s *Server) chanIcon(c *Client, ci proto.ChanIcon) {
	var stored []byte
	icon := ci.Icon
	if len(ci.PNG) > 0 {
		b, err := sanitizeIcon(ci.PNG)
		if err != nil {
			c.sendErr("icon", err.Error())
			return
		}
		stored = b
		h := sha256.Sum256(b)
		icon = "img:" + hex.EncodeToString(h[:])
	} else if icon != "" && !stdIconRe.MatchString(icon) {
		c.sendErr("icon", "unknown icon")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.channels[ci.ID]
	if ch == nil {
		c.sendErr("channel", "channel does not exist")
		return
	}
	if !s.hasLocked(c, proto.PermChannelIcon) {
		c.sendErr("permission", "you are not allowed to set channel icons")
		return
	}
	if !ch.Permanent {
		c.sendErr("icon", "only permanent channels can have an icon")
		return
	}
	if stored != nil {
		if err := os.MkdirAll(s.iconDir(), 0700); err != nil {
			c.sendErr("icon", "cannot store the icon")
			return
		}
		p := filepath.Join(s.iconDir(), icon[4:]+".png")
		if err := os.WriteFile(p+".tmp", stored, 0600); err != nil || os.Rename(p+".tmp", p) != nil {
			os.Remove(p + ".tmp")
			c.sendErr("icon", "cannot store the icon")
			return
		}
	}
	if ch.Icon == icon {
		return
	}
	ch.Icon = icon
	s.broadcastLocked(proto.TChanUpdate, s.channelInfo(ch), 0)
	s.saveChannelsLocked()
	s.gcIconsLocked()
}

func (s *Server) iconGet(c *Client, hash string) {
	if !hashRe.MatchString(hash) {
		return
	}
	b, err := os.ReadFile(filepath.Join(s.iconDir(), hash+".png"))
	if err != nil {
		b = nil
	}
	c.send(proto.TIconData, proto.IconData{Hash: hash, PNG: b})
}

// gcIconsLocked deletes stored icons no channel uses any more.
func (s *Server) gcIconsLocked() {
	used := map[string]bool{}
	for _, ch := range s.channels {
		if len(ch.Icon) > 4 && ch.Icon[:4] == "img:" {
			used[ch.Icon[4:]+".png"] = true
		}
	}
	entries, _ := os.ReadDir(s.iconDir())
	for _, e := range entries {
		if !used[e.Name()] {
			os.Remove(filepath.Join(s.iconDir(), e.Name()))
		}
	}
}
