package client

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sync"
	"time"

	"securespeak/internal/proto"
)

// Custom channel icons are fetched from the server by SHA-256 on first use
// and kept in memory (they are at most 64×64 PNGs).

var iconHashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

type iconCache struct {
	mu   sync.Mutex
	data map[string][]byte
	wait map[string][]chan struct{}
}

func (ic *iconCache) init() {
	if ic.data == nil {
		ic.data = map[string][]byte{}
		ic.wait = map[string][]chan struct{}{}
	}
}

// put stores an icon if its content matches the hash, and wakes waiters.
func (ic *iconCache) put(d proto.IconData) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.init()
	sum := sha256.Sum256(d.PNG)
	if len(d.PNG) > 0 && len(d.PNG) <= proto.MaxIconBytes && hex.EncodeToString(sum[:]) == d.Hash {
		ic.data[d.Hash] = d.PNG
	}
	for _, ch := range ic.wait[d.Hash] {
		close(ch)
	}
	delete(ic.wait, d.Hash)
}

// ChannelIcon returns a custom channel icon (PNG), fetching it from the
// server if needed.
func (c *Client) ChannelIcon(hash string) []byte {
	if !iconHashRe.MatchString(hash) {
		return nil
	}
	ic := &c.icons
	ic.mu.Lock()
	ic.init()
	if b := ic.data[hash]; b != nil {
		ic.mu.Unlock()
		return b
	}
	w := make(chan struct{})
	first := len(ic.wait[hash]) == 0
	ic.wait[hash] = append(ic.wait[hash], w)
	ic.mu.Unlock()
	if first {
		c.mu.Lock()
		s := c.sess
		c.mu.Unlock()
		if s == nil {
			ic.put(proto.IconData{Hash: hash})
			return nil
		}
		s.send(proto.TIconGet, proto.IconData{Hash: hash})
	}
	select {
	case <-w:
	case <-time.After(8 * time.Second):
		ic.mu.Lock()
		delete(ic.wait, hash) // let the next request ask again
		ic.mu.Unlock()
	}
	ic.mu.Lock()
	defer ic.mu.Unlock()
	return ic.data[hash]
}

// setChannelIcon handles the UI's "chan_icon" command.
func (c *Client) setChannelIcon(id uint32, icon string, png []byte) error {
	if len(png) > proto.MaxIconBytes {
		return errors.New("the icon is too large")
	}
	if len(png) > 0 {
		sum := sha256.Sum256(png)
		c.icons.put(proto.IconData{Hash: hex.EncodeToString(sum[:]), PNG: png}) // no round trip for our own upload
	}
	return c.withSession(func(s *session) error {
		s.send(proto.TChanIcon, proto.ChanIcon{ID: id, Icon: icon, PNG: png})
		return nil
	})
}
