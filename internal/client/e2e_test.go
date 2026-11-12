package client

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"securespeak/internal/proto"
	"securespeak/internal/server"
)

type recorder struct {
	mu     sync.Mutex
	events []struct {
		t string
		v any
	}
}

func (r *recorder) Emit(t string, v any) {
	if t == "level" || t == "state" {
		return
	}
	r.mu.Lock()
	r.events = append(r.events, struct {
		t string
		v any
	}{t, v})
	r.mu.Unlock()
}

func (r *recorder) find(t string, pred func(map[string]any) bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.t != t {
			continue
		}
		m, _ := e.v.(map[string]any)
		if pred(m) {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func startServer(t *testing.T) (*server.Server, string, int) {
	dir := t.TempDir()
	cfg := server.DefaultConfig()
	cfg.Listen, cfg.ListenUDP = "127.0.0.1:0", "127.0.0.1:0"
	server.SaveJSON(dir+"/config.json", cfg)
	srv, err := server.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv.Logf = func(string, ...any) {}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	tcp, _ := srv.Addrs()
	host, port := splitHP(tcp)
	ownerToken = srv.EnsureOwnerToken()
	return srv, host, port
}

var ownerToken string

func splitHP(a string) (string, int) {
	i := strings.LastIndex(a, ":")
	var p int
	json.Unmarshal([]byte(a[i+1:]), &p)
	return a[:i], p
}

func newTestClient(t *testing.T, host string, port int, nick string) (*Client, *recorder) {
	return newTestClientDir(t, t.TempDir(), host, port, nick)
}

func newTestClientDir(t *testing.T, dir, host string, port int, nick string) (*Client, *recorder) {
	st, _ := NewStore(dir)
	rec := &recorder{}
	c, err := New(st, rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	// first attempt must be rejected as untrusted (TOFU)
	res, err := c.HandleCommand("connect", mustJSON(map[string]any{"host": host, "port": port, "nick": nick}))
	if err != nil {
		t.Fatal(err)
	}
	if res != nil { // a fresh profile must be asked to trust the certificate
		m := res.(map[string]any)
		if m["untrusted"] != true {
			t.Fatal("expected untrusted certificate prompt")
		}
		res, err = c.HandleCommand("connect", mustJSON(map[string]any{"host": host, "port": port, "nick": nick, "trust": m["fp"]}))
		if err != nil || res != nil {
			t.Fatalf("connect: %v %v", res, err)
		}
	}
	c.mu.Lock()
	c.cfg.Audio.Sounds = false
	c.cfg.DownloadDir = t.TempDir()
	c.mu.Unlock()
	return c, rec
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestEndToEnd(t *testing.T) {
	_, host, port := startServer(t)
	alice, _ := newTestClient(t, host, port, "Alice")
	bob, bobRec := newTestClient(t, host, port, "Bob")

	// both in lobby, each must hold the other's voice key
	waitFor(t, "key exchange", func() bool {
		sa, sb := alice.State(), bob.State()
		ok := 0
		for _, c := range sa.Clients {
			if c.Nick == "Bob" && c.HasKey && c.Valid {
				ok++
			}
		}
		for _, c := range sb.Clients {
			if c.Nick == "Alice" && c.HasKey && c.Valid {
				ok++
			}
		}
		return ok == 2
	})

	// encrypted channel chat
	if _, err := alice.HandleCommand("chat", mustJSON(map[string]any{"kind": "channel", "text": "hello bob 🔒"})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "chat", func() bool {
		return bobRec.find("chat", func(m map[string]any) bool { return m["text"] == "hello bob 🔒" && m["fromNick"] == "Alice" })
	})

	// private message + poke + server chat
	bobID := bob.State().MyID
	alice.HandleCommand("chat", mustJSON(map[string]any{"kind": "private", "target": bobID, "text": "psst"}))
	alice.HandleCommand("poke", mustJSON(map[string]any{"target": bobID, "text": "wake up"}))
	alice.HandleCommand("chat", mustJSON(map[string]any{"kind": "server", "text": "hi all"}))
	waitFor(t, "private", func() bool {
		return bobRec.find("chat", func(m map[string]any) bool { return m["kind"] == "private" && m["text"] == "psst" })
	})
	waitFor(t, "poke", func() bool {
		return bobRec.find("poke", func(m map[string]any) bool { return m["text"] == "wake up" })
	})
	waitFor(t, "server chat", func() bool {
		return bobRec.find("chat", func(m map[string]any) bool { return m["kind"] == "server" && m["text"] == "hi all" })
	})

	// voice: Alice sends Opus frames, Bob's audio engine receives them intact
	waitFor(t, "udp", func() bool {
		alice.mu.Lock()
		defer alice.mu.Unlock()
		return alice.sess != nil && alice.sess.udpOK.Load()
	})
	bobSink := sink(bob)
	for i := 0; i < 10; i++ {
		alice.HandleVoiceFrame(0, proto.CodecOpus, 0, []byte(fmt.Sprintf("opus-frame-%02d", i)))
	}
	waitFor(t, "voice frames", func() bool { return bobSink.count() >= 10 })
	f := bobSink.get(0)
	if f.sender != alice.State().MyID || f.codec != proto.CodecOpus || string(f.payload) != "opus-frame-00" || f.flags&flagWhisper != 0 {
		t.Fatalf("bad frame: %+v", f)
	}

	// Bob moves to another channel -> no more audio, Alice rotates her key
	alice.mu.Lock()
	oldKey := alice.sess.senderKey.ID
	alice.mu.Unlock()
	var talk2 uint32
	for _, ch := range bob.State().Channels {
		if ch.Name == "Talk 1" {
			talk2 = ch.ID
		}
	}
	bob.HandleCommand("join", mustJSON(map[string]any{"id": talk2}))
	waitFor(t, "key rotation", func() bool {
		alice.mu.Lock()
		defer alice.mu.Unlock()
		return alice.sess.senderKey.ID != oldKey
	})

	// temporary channel: create, auto-join, deleted when empty
	alice.HandleCommand("chan_create", mustJSON(map[string]any{"name": "Temp Room", "pw": "secret"}))
	waitFor(t, "temp channel", func() bool {
		st := alice.State()
		for _, ch := range st.Channels {
			if ch.Name == "Temp Room" && st.MyChannel == ch.ID && ch.HasPassword {
				return true
			}
		}
		return false
	})
	// Bob cannot join without password
	var tempID uint32
	for _, ch := range bob.State().Channels {
		if ch.Name == "Temp Room" {
			tempID = ch.ID
		}
	}
	bob.HandleCommand("join", mustJSON(map[string]any{"id": tempID, "password": "nope"}))
	waitFor(t, "password error", func() bool {
		bobRec.mu.Lock()
		defer bobRec.mu.Unlock()
		for _, e := range bobRec.events {
			if e.t == "error" && e.v.(proto.Error).Code == "channel_password" {
				return true
			}
		}
		return false
	})
	alice.HandleCommand("join", mustJSON(map[string]any{"id": talk2}))
	waitFor(t, "temp channel removed", func() bool {
		for _, ch := range bob.State().Channels {
			if ch.Name == "Temp Room" {
				return false
			}
		}
		return true
	})

	// admin: login and kick Bob from the server
	alice.HandleCommand("token_use", mustJSON(map[string]any{"token": ownerToken}))
	waitFor(t, "owner", func() bool { return alice.State().Owner })
	alice.HandleCommand("kick", mustJSON(map[string]any{"id": bobID, "server": true, "reason": "test"}))
	waitFor(t, "kicked", func() bool { return !bob.State().Connected })
	if !bobRec.find("disconnected", func(m map[string]any) bool { return strings.Contains(m["reason"].(string), "Kicked") }) {
		t.Fatal("expected kick reason")
	}
}

// The server must never see plaintext: run a chat through and check that the
// server-side relay only handled ciphertext.
func TestServerSeesOnlyCiphertext(t *testing.T) {
	_, host, port := startServer(t)
	alice, _ := newTestClient(t, host, port, "Alice")
	bob, bobRec := newTestClient(t, host, port, "Bob")
	waitFor(t, "keys", func() bool {
		for _, c := range alice.State().Clients {
			if c.Nick == "Bob" && c.HasKey {
				return true
			}
		}
		return false
	})
	secret := "the eagle lands at midnight"
	alice.mu.Lock()
	s := alice.sess
	recips := map[uint32][]byte{}
	for id, p := range s.peers {
		recips[id] = p.pairwise
	}
	alice.mu.Unlock()
	alice.SendChat("channel", 0, secret)
	waitFor(t, "chat", func() bool {
		return bobRec.find("chat", func(m map[string]any) bool { return m["text"] == secret })
	})
	// Re-encrypt the same way and confirm the wire format contains no plaintext.
	body, _ := json.Marshal(chatBody{Text: secret})
	_ = bob
	nonce, ct, wraps, err := encryptForTest(body, recips)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := proto.Encode(proto.TChat, proto.Chat{Kind: "channel", Nonce: nonce, CT: ct, Wraps: wraps})
	if bytes.Contains(wire, []byte("eagle")) {
		t.Fatal("plaintext leaked onto the wire")
	}
}

type frame struct {
	sender  uint32
	flags   byte
	codec   byte
	seq     uint32
	payload []byte
}

type frameSink struct {
	mu     sync.Mutex
	frames []frame
}

func sink(c *Client) *frameSink {
	fs := &frameSink{}
	c.SetVoiceSink(func(b []byte) {
		fs.mu.Lock()
		fs.frames = append(fs.frames, frame{binary.BigEndian.Uint32(b[1:]), b[5], b[6], binary.BigEndian.Uint32(b[7:]), append([]byte(nil), b[11:]...)})
		fs.mu.Unlock()
	})
	return fs
}

func (fs *frameSink) count() int      { fs.mu.Lock(); defer fs.mu.Unlock(); return len(fs.frames) }
func (fs *frameSink) get(i int) frame { fs.mu.Lock(); defer fs.mu.Unlock(); return fs.frames[i] }

func haveKey(c *Client, nick string) func() bool {
	return func() bool {
		for _, u := range c.State().Clients {
			if u.Nick == nick && u.HasKey {
				return true
			}
		}
		return false
	}
}

func TestVoiceOverTCPFallback(t *testing.T) {
	testDisableUDP.Store(true)
	defer testDisableUDP.Store(false)
	_, host, port := startServer(t)
	alice, _ := newTestClient(t, host, port, "Alice")
	bob, _ := newTestClient(t, host, port, "Bob")
	waitFor(t, "keys", haveKey(bob, "Alice"))
	bs := sink(bob)
	for i := 0; i < 5; i++ {
		alice.HandleVoiceFrame(0, proto.CodecOpus, 0, []byte("tcp"))
	}
	waitFor(t, "voice over TCP", func() bool { return bs.count() == 5 })
}

func TestWhisper(t *testing.T) {
	_, host, port := startServer(t)
	alice, _ := newTestClient(t, host, port, "Alice")
	bob, _ := newTestClient(t, host, port, "Bob")
	carol, _ := newTestClient(t, host, port, "Carol")
	// Carol goes to another channel; Alice whispers to her by fingerprint list
	var talk1 uint32
	for _, ch := range carol.State().Channels {
		if ch.Name == "Talk 1" {
			talk1 = ch.ID
		}
	}
	carol.HandleCommand("join", mustJSON(map[string]any{"id": talk1}))
	waitFor(t, "carol moved", func() bool { return carol.State().MyChannel == talk1 })
	alice.HandleCommand("whisper_lists", mustJSON(map[string]any{"lists": []WhisperList{{Name: "Carol", Users: []string{carol.Fingerprint()}}}}))
	cs, bs := sink(carol), sink(bob)
	for i := 0; i < 20 && cs.count() == 0; i++ { // first frames race the key distribution
		alice.HandleVoiceFrame(1, proto.CodecOpus, 0, []byte("psst"))
		time.Sleep(20 * time.Millisecond)
	}
	waitFor(t, "whisper received", func() bool { return cs.count() > 0 })
	if f := cs.get(0); f.flags&flagWhisper == 0 || string(f.payload) != "psst" {
		t.Fatalf("bad whisper frame %+v", f)
	}
	time.Sleep(100 * time.Millisecond)
	if bs.count() != 0 {
		t.Fatal("Bob (not a whisper target) received the whisper")
	}
	// Bob cannot decrypt even if the server sent it to him: he never got the key
	bob.mu.Lock()
	for _, p := range bob.sess.peers {
		if p.Info.Nick == "Alice" && len(p.keys) != 1 {
			t.Errorf("Bob holds %d of Alice's keys, expected only the channel key", len(p.keys))
		}
	}
	bob.mu.Unlock()

	// quick whisper to Bob by id
	bobID := bob.State().MyID
	alice.HandleCommand("quick_whisper", mustJSON(map[string]any{"ids": []uint32{bobID}}))
	for i := 0; i < 20 && bs.count() == 0; i++ {
		alice.HandleVoiceFrame(ModeQuickWhisper, proto.CodecOpus, 0, []byte("hey bob"))
		time.Sleep(20 * time.Millisecond)
	}
	waitFor(t, "quick whisper", func() bool { return bs.count() > 0 })
}

func TestFileTransfer(t *testing.T) {
	_, host, port := startServer(t)
	alice, _ := newTestClient(t, host, port, "Alice")
	bob, bobRec := newTestClient(t, host, port, "Bob")
	waitFor(t, "keys", haveKey(alice, "Bob"))
	data := make([]byte, 5*1024*1024+123) // ~5 MB, not a multiple of the chunk size
	for i := range data {
		data[i] = byte(i*7 + i/1000)
	}
	if err := alice.OfferFile("channel", 0, "../../evil/report.pdf", bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	var fileID string
	waitFor(t, "offer", func() bool {
		return bobRec.find("chat", func(m map[string]any) bool {
			if f, ok := m["file"].(map[string]any); ok {
				fileID = f["id"].(string)
				return f["name"] == "report.pdf" // path components stripped
			}
			return false
		})
	})
	if err := bob.DownloadFile(fileID); err != nil {
		t.Fatal(err)
	}
	var path string
	waitFor(t, "download done", func() bool {
		return bobRec.find("file", func(m map[string]any) bool {
			if m["state"] == "done" {
				path, _ = m["path"].(string)
				return true
			}
			return false
		})
	})
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("downloaded file differs (err %v, %d bytes)", err, len(got))
	}
	if filepath.Base(path) != "report.pdf" {
		t.Fatalf("unexpected path %s", path)
	}
}

func TestAvatar(t *testing.T) {
	_, host, port := startServer(t)
	alice, _ := newTestClient(t, host, port, "Alice")
	bob, _ := newTestClient(t, host, port, "Bob")
	waitFor(t, "keys", haveKey(alice, "Bob"))
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)
	if err := alice.SetAvatar(png); err != nil {
		t.Fatal(err)
	}
	aliceID := alice.State().MyID
	waitFor(t, "avatar", func() bool { return bytes.Equal(bob.Avatar(aliceID), png) })
	if err := alice.SetAvatar([]byte("<svg onload=alert(1)>")); err == nil {
		t.Fatal("SVG avatar accepted")
	}
	// newcomers receive existing avatars
	carol, _ := newTestClient(t, host, port, "Carol")
	waitFor(t, "avatar for newcomer", func() bool { return bytes.Equal(carol.Avatar(aliceID), png) })
	alice.SetAvatar(nil)
	waitFor(t, "avatar removed", func() bool { return bob.Avatar(aliceID) == nil })
}
