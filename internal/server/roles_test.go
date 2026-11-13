package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"securespeak/internal/proto"
)

// Roles saved by 3.0 get the permissions introduced in 3.1.
func TestRoleMigration(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	SaveJSON(filepath.Join(dir, "config.json"), cfg)
	old := []*Role{
		{ID: 1, Name: "Guest", Default: true, Perms: []string{proto.PermVoiceSpeak}},
		{ID: 2, Name: "Server Admin", Power: 100, Perms: []string{proto.PermServerEdit, proto.PermVoiceSpeak}},
		{ID: 5, Name: "Listener", Power: 10, Perms: []string{proto.PermChatChannel}},
	}
	SaveJSON(filepath.Join(dir, "roles.json"), old)
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	has := func(id uint32, p string) bool {
		for _, x := range s.roleLocked(id).Perms {
			if x == p {
				return true
			}
		}
		return false
	}
	if !has(2, proto.PermServerPassword) || !has(2, proto.PermScreenShare) || !has(1, proto.PermScreenShare) {
		t.Fatal("new permissions not added")
	}
	if has(5, proto.PermScreenShare) || has(1, proto.PermServerPassword) {
		t.Fatal("permissions added where they did not belong")
	}
	// runs only once: an admin removing the permission later keeps it removed
	r := s.roleLocked(2)
	r.Perms = []string{proto.PermServerEdit}
	s.saveRolesLocked()
	s2, _ := New(dir)
	for _, p := range s2.roleLocked(2).Perms {
		if p == proto.PermServerPassword {
			t.Fatal("migration ran twice")
		}
	}
	os.RemoveAll(dir)
}

// Roles from 3.1/3.2 (schema 2) get channel.icon where they had channel.edit,
// without re-adding 3.1 permissions an admin removed on purpose.
func TestRoleMigrationChannelIcons(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.RolesSchema = 2
	SaveJSON(filepath.Join(dir, "config.json"), cfg)
	SaveJSON(filepath.Join(dir, "roles.json"), []*Role{
		{ID: 1, Name: "Guest", Default: true, Perms: []string{proto.PermVoiceSpeak}},
		{ID: 3, Name: "Moderator", Power: 50, Perms: []string{proto.PermChannelEdit, proto.PermServerEdit}},
	})
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	has := func(id uint32, p string) bool {
		for _, x := range s.roleLocked(id).Perms {
			if x == p {
				return true
			}
		}
		return false
	}
	if !has(3, proto.PermChannelIcon) {
		t.Fatal("moderator did not get channel.icon")
	}
	if has(1, proto.PermChannelIcon) {
		t.Fatal("guests must not get channel.icon")
	}
	if has(3, proto.PermServerPassword) || has(1, proto.PermScreenShare) {
		t.Fatal("3.1 permissions were re-added on a schema-2 server")
	}
}

func TestSanitizeIcon(t *testing.T) {
	mk := func(w, h int) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		img.SetNRGBA(1, 1, color.NRGBA{255, 0, 0, 77})
		var b bytes.Buffer
		png.Encode(&b, img)
		return b.Bytes()
	}
	out, err := sanitizeIcon(mk(64, 32))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if c := color.NRGBAModel.Convert(img.At(1, 1)).(color.NRGBA); c.A != 77 || c.R != 255 {
		t.Fatalf("transparency lost: %+v", c)
	}
	if c := color.NRGBAModel.Convert(img.At(5, 5)).(color.NRGBA); c.A != 0 {
		t.Fatal("transparent pixel became opaque")
	}
	for name, b := range map[string][]byte{
		"too wide":  mk(65, 10),
		"not a png": []byte("GIF89a......"),
		"empty":     nil,
		"too big":   bytes.Repeat([]byte{1}, proto.MaxIconBytes+1),
		"truncated": mk(32, 32)[:60],
	} {
		if _, err := sanitizeIcon(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// 3.4: roles that can share screens also get watch-together.
func TestRoleMigrationWatch(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.RolesSchema = 3
	SaveJSON(filepath.Join(dir, "config.json"), cfg)
	SaveJSON(filepath.Join(dir, "roles.json"), []*Role{
		{ID: 1, Name: "Guest", Default: true, Perms: []string{proto.PermVoiceSpeak, proto.PermScreenShare}},
		{ID: 5, Name: "Listener", Power: 10, Perms: []string{proto.PermChatChannel, proto.PermChannelEdit}},
	})
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	has := func(id uint32, p string) bool {
		for _, x := range s.roleLocked(id).Perms {
			if x == p {
				return true
			}
		}
		return false
	}
	if !has(1, proto.PermWatch) || has(5, proto.PermWatch) {
		t.Fatal("watch.together migration wrong")
	}
	if has(5, proto.PermChannelIcon) {
		t.Fatal("3.3 migration re-ran on a schema-3 server")
	}
}
