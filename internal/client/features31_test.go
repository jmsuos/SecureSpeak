package client

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"securespeak/internal/proto"
)

func TestServerPasswordFromApp(t *testing.T) {
	_, host, port := startServer(t)
	alice, aliceRec := newTestClient(t, host, port, "Alice")
	bob, bobRec := newTestClient(t, host, port, "Bob")
	alice.HandleCommand("token_use", mustJSON(map[string]any{"token": ownerToken}))
	waitFor(t, "owner", func() bool { return alice.State().Owner })

	// a guest may not change it
	bob.HandleCommand("server_password", mustJSON(map[string]any{"password": "hunter2"}))
	waitFor(t, "guest denied", hasErr(bobRec, "permission", ""))

	alice.HandleCommand("server_password", mustJSON(map[string]any{"password": "hunter2"}))
	waitFor(t, "password set", func() bool { return bob.State().ServerHasPw })
	if !bob.State().Connected {
		t.Fatal("connected user was dropped")
	}
	_ = aliceRec

	// new connections need the password
	st, _ := NewStore(t.TempDir())
	carol, _ := New(st, &recorder{})
	t.Cleanup(carol.Close)
	res, _ := carol.HandleCommand("connect", mustJSON(map[string]any{"host": host, "port": port, "nick": "Carol"}))
	fp := res.(map[string]any)["fp"]
	if _, err := carol.HandleCommand("connect", mustJSON(map[string]any{"host": host, "port": port, "nick": "Carol", "trust": fp})); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("connected without password: %v", err)
	}
	if _, err := carol.HandleCommand("connect", mustJSON(map[string]any{"host": host, "port": port, "nick": "Carol", "password": "hunter2"})); err != nil {
		t.Fatalf("correct password rejected: %v", err)
	}
	carol.Disconnect()

	alice.HandleCommand("server_password", mustJSON(map[string]any{"remove": true}))
	waitFor(t, "password removed", func() bool { return !bob.State().ServerHasPw })
	if _, err := carol.HandleCommand("connect", mustJSON(map[string]any{"host": host, "port": port, "nick": "Carol"})); err != nil {
		t.Fatalf("still needs a password after removal: %v", err)
	}
}

type videoSink struct {
	mu     sync.Mutex
	frames [][]byte
}

func vsink(c *Client) *videoSink {
	vs := &videoSink{}
	c.SetVoiceSink(func(b []byte) {
		if b[0] == 2 {
			vs.mu.Lock()
			vs.frames = append(vs.frames, append([]byte(nil), b...))
			vs.mu.Unlock()
		}
	})
	return vs
}

func (v *videoSink) n() int { v.mu.Lock(); defer v.mu.Unlock(); return len(v.frames) }

func TestScreenShare(t *testing.T) {
	_, host, port := startServer(t)
	alice, aliceRec := newTestClient(t, host, port, "Alice")
	bob, _ := newTestClient(t, host, port, "Bob")
	carol, carolRec := newTestClient(t, host, port, "Carol")
	waitFor(t, "keys", haveKey(bob, "Alice"))
	waitFor(t, "keys", haveKey(carol, "Alice"))
	aliceID := alice.State().MyID

	if _, err := alice.HandleCommand("share", mustJSON(map[string]any{"on": true})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sharing visible", func() bool { return clientByNick(bob, "Alice").Sharing })

	bs, cs := vsink(bob), vsink(carol)
	bob.HandleCommand("watch", mustJSON(map[string]any{"id": aliceID, "on": true}))
	waitFor(t, "keyframe request to sharer", func() bool {
		return aliceRec.find("keyframe", func(map[string]any) bool { return true })
	})
	big := bytes.Repeat([]byte("frame-data-"), 40000) // ~440 KB keyframe
	alice.HandleVideoFrame(1, proto.CodecVP9, big)
	alice.HandleVideoFrame(0, proto.CodecVP9, []byte("delta"))
	waitFor(t, "frames at watcher", func() bool { return bs.n() == 2 })
	f := bs.frames[0]
	if f[0] != 2 || binary.BigEndian.Uint32(f[1:]) != aliceID || f[5] != 1 || f[6] != proto.CodecVP9 || !bytes.Equal(f[7:], big) {
		t.Fatal("watcher got a corrupted frame")
	}
	time.Sleep(150 * time.Millisecond)
	if cs.n() != 0 {
		t.Fatal("non-watcher received the screen share")
	}

	// watching from another channel is refused
	var talk1 uint32
	for _, ch := range carol.State().Channels {
		if ch.Name == "Talk 1" {
			talk1 = ch.ID
		}
	}
	carol.HandleCommand("join", mustJSON(map[string]any{"id": talk1}))
	waitFor(t, "carol moved", func() bool { return carol.State().MyChannel == talk1 })
	carol.HandleCommand("watch", mustJSON(map[string]any{"id": aliceID, "on": true}))
	waitFor(t, "watch refused", hasErr(carolRec, "screen", ""))

	// stop
	alice.HandleCommand("share", mustJSON(map[string]any{"on": false}))
	waitFor(t, "sharing stopped", func() bool { return !clientByNick(bob, "Alice").Sharing })
	alice.HandleVideoFrame(1, proto.CodecVP9, []byte("after stop"))
	time.Sleep(150 * time.Millisecond)
	if bs.n() != 2 {
		t.Fatal("frames delivered after sharing stopped")
	}
}

func TestMediaPreviewAutoLoad(t *testing.T) {
	_, host, port := startServer(t)
	alice, _ := newTestClient(t, host, port, "Alice")
	bob, bobRec := newTestClient(t, host, port, "Bob")
	waitFor(t, "keys", haveKey(alice, "Bob"))
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 200000)...)
	if err := alice.OfferFile("channel", 0, "Screenshot.png", "image/png", bytes.NewReader(png)); err != nil {
		t.Fatal(err)
	}
	alice.OfferFile("channel", 0, "notes.pdf", "application/pdf", bytes.NewReader([]byte("%PDF-1.4")))
	var id string
	waitFor(t, "auto-loaded preview", func() bool {
		return bobRec.find("file", func(m map[string]any) bool {
			if m["state"] == "done" && m["cached"] == true {
				id = m["id"].(string)
				return true
			}
			return false
		})
	})
	path, ok := bob.MediaFile(id)
	if !ok || !strings.HasPrefix(path, bob.store.CacheDir()) {
		t.Fatalf("preview not in cache: %q", path)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, png) {
		t.Fatal("cached preview differs")
	}
	// the PDF must not be fetched automatically
	time.Sleep(200 * time.Millisecond)
	if bobRec.find("file", func(m map[string]any) bool { return m["name"] == "notes.pdf" }) {
		t.Fatal("non-media file was downloaded automatically")
	}
	res, err := bob.SaveCachedFile(id)
	if err != nil {
		t.Fatal(err)
	}
	saved := res.(map[string]any)["path"].(string)
	if got, _ := os.ReadFile(saved); !bytes.Equal(got, png) || strings.HasPrefix(saved, bob.store.CacheDir()) {
		t.Fatalf("save failed: %s", saved)
	}
	// sender can preview its own upload
	if p, ok := alice.MediaFile(id); !ok || p == "" {
		t.Fatal("sender has no preview of its own file")
	}
}
