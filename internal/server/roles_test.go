package server

import (
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
