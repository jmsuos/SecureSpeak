package client

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func fakeMP3(tag string) []byte {
	return append([]byte("ID3\x04\x00\x00"), bytes.Repeat([]byte(tag), 200)...)
}
func fakeWAV() []byte {
	b := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	return append(b, bytes.Repeat([]byte{1, 2}, 300)...)
}

func TestJoinLeaveSoundsAndLeaveMessage(t *testing.T) {
	_, host, port := startServer(t)
	bob, bobRec := newTestClient(t, host, port, "Bob")
	bob.mu.Lock()
	bob.cfg.Audio.Sounds = true
	bob.mu.Unlock()

	aliceDir := t.TempDir()
	alice, aliceRec := newTestClientDir(t, aliceDir, host, port, "Alice")
	join, leave := fakeMP3("j"), fakeWAV()
	if _, err := alice.HandleCommand("sound_set", mustJSON(map[string]any{"slot": "join", "name": "C:\\sounds\\hello.mp3", "data": join})); err != nil {
		t.Fatal(err)
	}
	if _, err := alice.HandleCommand("sound_set", mustJSON(map[string]any{"slot": "leave", "name": "bye.wav", "data": leave})); err != nil {
		t.Fatal(err)
	}
	if _, err := alice.HandleCommand("leave_msg", mustJSON(map[string]any{"text": "Goodnight everyone!"})); err != nil {
		t.Fatal(err)
	}
	if st := alice.State(); st.JoinSound == nil || st.JoinSound.Name != "hello.mp3" || st.LeaveMessage != "Goodnight everyone!" {
		t.Fatalf("state: %+v %q", st.JoinSound, st.LeaveMessage)
	}
	// invalid uploads
	if _, err := alice.HandleCommand("sound_set", mustJSON(map[string]any{"slot": "join", "data": []byte("<html>not audio")})); err == nil {
		t.Fatal("non-audio accepted")
	}
	if _, err := alice.HandleCommand("sound_set", mustJSON(map[string]any{"slot": "join", "data": fakeMP3(strings.Repeat("x", 3000))})); err == nil {
		t.Fatal("oversized sound accepted")
	}
	if _, err := alice.HandleCommand("leave_msg", mustJSON(map[string]any{"text": strings.Repeat("x", 81)})); err == nil {
		t.Fatal("long leave message accepted")
	}
	if _, err := alice.HandleCommand("sound_set", mustJSON(map[string]any{"slot": "app:../../x", "data": join})); err == nil {
		t.Fatal("unknown app sound accepted")
	}

	// reconnect: Bob hears Alice's entrance sound (fetched from her, E2E)
	alice.Disconnect()
	waitFor(t, "alice gone", func() bool { return clientByNick(bob, "Alice").ID == 0 })
	joinHash, leaveHash := soundHash(join), soundHash(leave)
	alice.HandleCommand("connect", mustJSON(map[string]any{"host": host, "port": port, "nick": "Alice"}))
	waitFor(t, "bob plays alice's join sound", func() bool {
		return bobRec.find("peer_sound", func(m map[string]any) bool { return m["hash"] == joinHash })
	})
	if !bytes.Equal(bob.SoundFile(joinHash), join) {
		t.Fatal("bob does not have the join sound")
	}
	waitFor(t, "bob prefetched the leave sound", func() bool { return bob.SoundFile(leaveHash) != nil })

	// leave: message and exit sound
	alice.Disconnect()
	waitFor(t, "leave message shown", func() bool {
		return bobRec.find("chat", func(m map[string]any) bool {
			s, _ := m["text"].(string)
			return s == "Alice disconnected (Goodnight everyone!)"
		})
	})
	waitFor(t, "bob plays alice's exit sound", func() bool {
		return bobRec.find("peer_sound", func(m map[string]any) bool { return m["hash"] == leaveHash })
	})

	// Bob can switch other people's sounds off
	bob.HandleCommand("peer_sounds", mustJSON(map[string]any{"on": false}))
	n := len(bobRec.all("peer_sound"))
	alice.HandleCommand("connect", mustJSON(map[string]any{"host": host, "port": port, "nick": "Alice"}))
	waitFor(t, "alice back", func() bool { return clientByNick(bob, "Alice").ID != 0 })
	alice.Disconnect()
	waitFor(t, "alice gone again", func() bool { return clientByNick(bob, "Alice").ID == 0 })
	if len(bobRec.all("peer_sound")) != n {
		t.Fatal("peer sound played while switched off")
	}
	_ = aliceRec

	// app sounds are local replacements
	if _, err := bob.HandleCommand("sound_set", mustJSON(map[string]any{"slot": "app:poke", "name": "boing.mp3", "data": join})); err != nil {
		t.Fatal(err)
	}
	if bob.State().AppSounds["poke"].Hash != joinHash {
		t.Fatal("app sound not stored")
	}
	bob.HandleCommand("sound_set", mustJSON(map[string]any{"slot": "app:poke"}))
	if _, ok := bob.State().AppSounds["poke"]; ok {
		t.Fatal("app sound not reset")
	}
}

func TestWatchTogetherRelay(t *testing.T) {
	_, host, port := startServer(t)
	alice, _ := newTestClient(t, host, port, "Alice")
	bob, bobRec := newTestClient(t, host, port, "Bob")
	carol, carolRec := newTestClient(t, host, port, "Carol")
	_ = bob
	// Carol goes to another channel
	var other uint32
	for _, ch := range carol.State().Channels {
		if ch.Name == "Talk 1" {
			other = ch.ID
		}
	}
	carol.HandleCommand("join", mustJSON(map[string]any{"id": other}))
	waitFor(t, "carol moved", func() bool { return clientByNick(alice, "Carol").Channel == other })

	data := map[string]any{"op": "start", "v": "dQw4w9WgXcQ", "t": 12.5, "p": true}
	if _, err := alice.HandleCommand("watch_send", mustJSON(map[string]any{"data": data})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "bob gets the watch message", func() bool {
		return bobRec.find("watch", func(m map[string]any) bool {
			var d map[string]any
			raw, _ := json.Marshal(m["data"])
			json.Unmarshal(raw, &d)
			return d["v"] == "dQw4w9WgXcQ" && m["fromNick"] == "Alice"
		})
	})
	if len(carolRec.all("watch")) != 0 {
		t.Fatal("watch message leaked to another channel")
	}
	if _, err := alice.HandleCommand("watch_send", mustJSON(map[string]any{"data": strings.Repeat("x", 5000)})); err == nil {
		t.Fatal("oversized watch message accepted")
	}
}

func (r *recorder) all(t string) []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []any
	for _, e := range r.events {
		if e.t == t {
			out = append(out, e.v)
		}
	}
	return out
}
