package client

import (
	"bytes"
	"os"
	"testing"
)

func fileEvent(r *recorder, name, dir, state string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if m, ok := e.v.(map[string]any); ok && e.t == "file" && m["name"] == name && m["dir"] == dir && m["state"] == state {
			return m
		}
	}
	return nil
}

func offerID(r *recorder, name string) string {
	var id string
	r.find("chat", func(m map[string]any) bool {
		if f, ok := m["file"].(map[string]any); ok && f["name"] == name {
			id = f["id"].(string)
			return true
		}
		return false
	})
	return id
}

func TestWithdrawFile(t *testing.T) {
	_, host, port := startServer(t)
	alice, aliceRec := newTestClient(t, host, port, "Alice")
	bob, bobRec := newTestClient(t, host, port, "Bob")
	waitFor(t, "keys", haveKey(alice, "Bob"))

	// 1. never downloaded
	alice.OfferFile("channel", 0, "plan.pdf", "application/pdf", bytes.NewReader([]byte("%PDF-1.4 plan")))
	waitFor(t, "offer", func() bool { return offerID(bobRec, "plan.pdf") != "" })
	pdf := offerID(bobRec, "plan.pdf")
	if err := alice.CancelFile(pdf); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sender sees withdrawn", func() bool { return fileEvent(aliceRec, "plan.pdf", "out", "withdrawn") != nil })
	waitFor(t, "recipient told", func() bool { return fileEvent(bobRec, "plan.pdf", "in", "withdrawn") != nil })
	if err := bob.DownloadFile(pdf); err == nil {
		t.Fatal("withdrawn file could still be requested")
	}

	// 2. auto-loaded preview is removed
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 5000)...)
	alice.OfferFile("channel", 0, "shot.png", "image/png", bytes.NewReader(png))
	waitFor(t, "preview cached", func() bool { return fileEvent(bobRec, "shot.png", "in", "done") != nil })
	pngID := offerID(bobRec, "shot.png")
	cached, ok := bob.MediaFile(pngID)
	if !ok {
		t.Fatal("no preview")
	}
	alice.CancelFile(pngID)
	waitFor(t, "preview withdrawn", func() bool { return fileEvent(bobRec, "shot.png", "in", "withdrawn") != nil })
	if _, ok := bob.MediaFile(pngID); ok {
		t.Fatal("preview still served after withdrawal")
	}
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Fatal("cached preview not deleted")
	}
	if _, ok := alice.MediaFile(pngID); ok {
		t.Fatal("sender copy not deleted")
	}

	// 3. a file the recipient already downloaded stays theirs
	alice.OfferFile("channel", 0, "notes.txt", "text/plain", bytes.NewReader([]byte("keep me")))
	waitFor(t, "offer 3", func() bool { return offerID(bobRec, "notes.txt") != "" })
	txt := offerID(bobRec, "notes.txt")
	bob.DownloadFile(txt)
	waitFor(t, "downloaded", func() bool { return fileEvent(bobRec, "notes.txt", "in", "done") != nil })
	alice.CancelFile(txt)
	waitFor(t, "withdrawn after download", func() bool {
		m := fileEvent(bobRec, "notes.txt", "in", "withdrawn")
		return m != nil && m["kept"] == true
	})
	p, err := bob.DownloadedPath(txt)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "keep me" {
		t.Fatal("downloaded copy was removed")
	}
}
