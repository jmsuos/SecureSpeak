package client

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"securespeak/internal/proto"
)

func chanByName(c *Client, name string) proto.ChannelInfo {
	for _, ch := range c.State().Channels {
		if ch.Name == name {
			return ch
		}
	}
	return proto.ChannelInfo{}
}

func TestChannelIcons(t *testing.T) {
	srv, host, port := startServer(t)
	_ = srv
	alice, aliceRec := newTestClient(t, host, port, "Alice")
	bob, bobRec := newTestClient(t, host, port, "Bob")
	alice.HandleCommand("token_use", mustJSON(map[string]any{"token": ownerToken}))
	waitFor(t, "alice owner", func() bool { return alice.State().Owner })

	// a new server gives Lobby and AFK built-in icons
	lobby := chanByName(bob, "Lobby")
	if lobby.Icon != "std:home" || chanByName(bob, "AFK").Icon != "std:moon" {
		t.Fatalf("default icons: %q %q", lobby.Icon, chanByName(bob, "AFK").Icon)
	}

	// guests may not set icons
	bob.HandleCommand("chan_icon", mustJSON(map[string]any{"id": lobby.ID, "icon": "std:star"}))
	waitFor(t, "guest refused", hasErr(bobRec, "permission", "channel icons"))

	// built-in icon
	alice.HandleCommand("chan_icon", mustJSON(map[string]any{"id": lobby.ID, "icon": "std:game"}))
	waitFor(t, "bob sees std icon", func() bool { return chanByName(bob, "Lobby").Icon == "std:game" })
	alice.HandleCommand("chan_icon", mustJSON(map[string]any{"id": lobby.ID, "icon": "std:../../etc"}))
	waitFor(t, "bad name refused", hasErr(aliceRec, "icon", "unknown icon"))

	// custom PNG with transparency
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	img.SetNRGBA(3, 4, color.NRGBA{10, 200, 30, 128})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	alice.HandleCommand("chan_icon", mustJSON(map[string]any{"id": lobby.ID, "png": buf.Bytes()}))
	waitFor(t, "bob sees custom icon", func() bool { return strings.HasPrefix(chanByName(bob, "Lobby").Icon, "img:") })
	hash := chanByName(bob, "Lobby").Icon[4:]
	got := bob.ChannelIcon(hash)
	if got == nil {
		t.Fatal("bob could not fetch the icon")
	}
	dec, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if c := color.NRGBAModel.Convert(dec.At(3, 4)).(color.NRGBA); c.A != 128 || c.G != 200 {
		t.Fatalf("pixel %+v", c)
	}
	if bob.ChannelIcon(strings.Repeat("0", 64)) != nil || bob.ChannelIcon("../x") != nil {
		t.Fatal("unknown or malformed hash returned data")
	}
	stored := filepath.Join(srv.DataDir(), "channel-icons", hash+".png")
	if _, err := os.Stat(stored); err != nil {
		t.Fatal("icon not stored on the server")
	}

	// oversized images are refused
	big := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	buf.Reset()
	png.Encode(&buf, big)
	alice.HandleCommand("chan_icon", mustJSON(map[string]any{"id": lobby.ID, "png": buf.Bytes()}))
	waitFor(t, "oversized refused", hasErr(aliceRec, "icon", "64×64"))

	// removing the icon deletes the stored file
	alice.HandleCommand("chan_icon", mustJSON(map[string]any{"id": lobby.ID, "icon": ""}))
	waitFor(t, "icon removed", func() bool { return chanByName(bob, "Lobby").Icon == "" })
	if _, err := os.Stat(stored); !os.IsNotExist(err) {
		t.Fatal("unused icon file not cleaned up")
	}

	// temporary channels cannot have icons
	alice.HandleCommand("chan_create", mustJSON(map[string]any{"name": "Temp", "perm": false}))
	waitFor(t, "temp channel", func() bool { return chanByName(alice, "Temp").ID != 0 })
	alice.HandleCommand("chan_icon", mustJSON(map[string]any{"id": chanByName(alice, "Temp").ID, "icon": "std:star"}))
	waitFor(t, "temp refused", hasErr(aliceRec, "icon", "only permanent"))

	// the permission is listed for the Roles & Permissions screen
	found := false
	for _, p := range alice.State().PermCatalog {
		found = found || p.ID == proto.PermChannelIcon
	}
	if !found {
		t.Fatal("channel.icon missing from the permission catalog")
	}
}
