package client

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"securespeak/internal/server"
)

func TestAutoReconnect(t *testing.T) {
	old := reconnectDelays
	reconnectDelays = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}
	t.Cleanup(func() { reconnectDelays = old })
	srv, host, port := startServer(t)
	aliceDir := t.TempDir()
	alice, aliceRec := newTestClientDir(t, aliceDir, host, port, "Alice")
	bob, _ := newTestClient(t, host, port, "Bob")

	talk := chanByName(alice, "Talk 1")
	alice.HandleCommand("join", mustJSON(map[string]any{"id": talk.ID}))
	waitFor(t, "alice in Talk 1", func() bool { return alice.State().MyChannel == talk.ID })

	// 1. the connection drops (internet trouble)
	alice.mu.Lock()
	conn := alice.sess.conn
	alice.mu.Unlock()
	conn.Close()
	waitFor(t, "disconnect marked as reconnecting", func() bool {
		return aliceRec.find("disconnected", func(m map[string]any) bool { return m["reconnecting"] == true })
	})
	waitFor(t, "reconnected", func() bool { return alice.State().Connected })
	waitFor(t, "back in Talk 1", func() bool { return alice.State().MyChannel == talk.ID })
	waitFor(t, "bob sees alice in Talk 1", func() bool { return clientByNick(bob, "Alice").Channel == talk.ID })
	if alice.State().Reconnecting != nil {
		t.Fatal("still marked as reconnecting")
	}

	// 2. the server stops (e.g. for an update) and comes back on the same port
	dataDir := srv.DataDir()
	cfg, err := server.LoadConfig(dataDir + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Listen = host + ":" + itoa(port)
	cfg.ListenUDP = host + ":" + itoa(port)
	server.SaveJSON(dataDir+"/config.json", cfg)
	srv.Close()
	waitFor(t, "alice notices", func() bool { return !alice.State().Connected })
	waitFor(t, "reconnecting state", func() bool { r := alice.State().Reconnecting; return r != nil && r.Attempt >= 2 })
	srv2, err := server.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	srv2.Logf = func(string, ...any) {}
	if err := srv2.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv2.Close)
	waitFor(t, "reconnected after server restart", func() bool { return alice.State().Connected })
	waitFor(t, "back in Talk 1 after restart", func() bool { return alice.State().MyChannel == chanByName(alice, "Talk 1").ID })
	waitFor(t, "bob also back", func() bool { return bob.State().Connected })

	// failed attempts don't spam the chat
	n := 0
	for _, e := range aliceRec.all("chat") {
		if m, ok := e.(map[string]any); ok && m["kind"] == "system" && strings.Contains(m["text"].(string), "econnect") {
			n++
		}
	}
	if n > 0 {
		t.Fatalf("%d reconnect chat messages", n)
	}

	// 3. a second copy with the same identity: the first is told and does NOT reconnect
	_, dupRec := newTestClientDir(t, aliceDir, host, port, "Alice")
	_ = dupRec
	waitFor(t, "first copy replaced", func() bool {
		return aliceRec.find("disconnected", func(m map[string]any) bool {
			r, _ := m["reason"].(string)
			return strings.Contains(r, "same identity") && m["reconnecting"] == false
		})
	})
	time.Sleep(500 * time.Millisecond)
	if alice.State().Connected || alice.State().Reconnecting != nil {
		t.Fatal("replaced copy reconnected (would fight the other copy)")
	}
	if c := clientByNick(bob, "Alice"); c.ID == 0 {
		t.Fatal("bob does not see the new Alice")
	}

	// 4. disconnecting yourself never reconnects
	bob.Disconnect()
	time.Sleep(400 * time.Millisecond)
	if bob.State().Connected || bob.State().Reconnecting != nil {
		t.Fatal("reconnected after a manual disconnect")
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
