package client

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"securespeak/internal/e2e"
	"securespeak/internal/proto"
)

func hasErr(r *recorder, code, contains string) func() bool {
	return func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, e := range r.events {
			if pe, ok := e.v.(proto.Error); ok && e.t == "error" && pe.Code == code && strings.Contains(pe.Msg, contains) {
				return true
			}
		}
		return false
	}
}

func clientByNick(c *Client, nick string) uiClient {
	for _, u := range c.State().Clients {
		if u.Nick == nick {
			return u
		}
	}
	return uiClient{}
}

func has(list []uint32, id uint32) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

func TestRolesAndOwner(t *testing.T) {
	_, host, port := startServer(t)
	aliceDir, bobDir := t.TempDir(), t.TempDir()
	alice, aliceRec := newTestClientDir(t, aliceDir, host, port, "Alice")
	bob, bobRec := newTestClientDir(t, bobDir, host, port, "Bob")
	carol, _ := newTestClient(t, host, port, "Carol")

	// --- one-time owner key
	alice.HandleCommand("token_use", mustJSON(map[string]any{"token": strings.ToLower(ownerToken)})) // case/format-insensitive
	waitFor(t, "alice owner", func() bool { return alice.State().Owner })
	bob.HandleCommand("token_use", mustJSON(map[string]any{"token": ownerToken}))
	waitFor(t, "token reuse rejected", hasErr(bobRec, "token", "already used"))
	if bob.State().Owner {
		t.Fatal("token worked twice")
	}

	// --- owner is recognized automatically after reconnecting
	alice.Close()
	alice, aliceRec = newTestClientDir(t, aliceDir, host, port, "Alice")
	waitFor(t, "owner after reconnect", func() bool { return alice.State().Owner })

	// --- owner hands out a Server Admin key; Bob redeems it
	var token string
	alice.HandleCommand("token_create", mustJSON(map[string]any{"role": 2, "note": "for Bob"}))
	waitFor(t, "token created", func() bool {
		return func() bool {
			aliceRec.mu.Lock()
			defer aliceRec.mu.Unlock()
			for _, e := range aliceRec.events {
				if tk, ok := e.v.(proto.Token); ok && e.t == "token" {
					token = tk.Token
					return true
				}
			}
			return false
		}()
	})
	bob.HandleCommand("token_use", mustJSON(map[string]any{"token": token}))
	waitFor(t, "bob admin", func() bool { return has(bob.State().Clients[indexOfMe(bob)].Roles, 2) })
	aliceID := alice.State().MyID
	carolID := carol.State().MyID

	// --- Bob (Server Admin, power 100) cannot touch the owner
	bob.HandleCommand("kick", mustJSON(map[string]any{"id": aliceID, "server": true}))
	waitFor(t, "kick owner denied", hasErr(bobRec, "permission", "owner"))
	bob.HandleCommand("ban", mustJSON(map[string]any{"id": aliceID, "minutes": 0}))
	bob.HandleCommand("server_mute", mustJSON(map[string]any{"id": aliceID, "on": true}))
	bob.HandleCommand("client_roles", mustJSON(map[string]any{"id": aliceID, "roles": []uint32{}}))
	waitFor(t, "role change of owner denied", hasErr(bobRec, "permission", "owner cannot be changed"))
	time.Sleep(200 * time.Millisecond)
	if !alice.State().Connected || clientByNick(bob, "Alice").ServerMuted || !alice.State().Owner {
		t.Fatal("owner was affected by a non-owner admin")
	}

	// --- power limits for role management
	bob.HandleCommand("role_save", mustJSON(map[string]any{"name": "Co-Admin", "power": 100, "perms": []string{proto.PermClientKickServer}}))
	waitFor(t, "equal-power role denied", hasErr(bobRec, "permission", "less power"))
	bob.HandleCommand("role_save", mustJSON(map[string]any{"name": "Helper", "power": 40, "color": "#ff8800",
		"perms": []string{proto.PermClientMove, proto.PermClientKickChannel}}))
	var helper uint32
	waitFor(t, "helper role", func() bool {
		for _, r := range bob.State().Roles {
			if r.Name == "Helper" {
				helper = r.ID
				return r.Power == 40 && r.Color == "#ff8800"
			}
		}
		return false
	})
	bob.HandleCommand("client_roles", mustJSON(map[string]any{"id": carolID, "roles": []uint32{2}}))
	waitFor(t, "cannot give equal role", hasErr(bobRec, "permission", "Server Admin"))
	bob.HandleCommand("client_roles", mustJSON(map[string]any{"id": carolID, "roles": []uint32{helper}}))
	waitFor(t, "carol helper", func() bool { return has(clientByNick(alice, "Carol").Roles, helper) })

	// --- Carol (Helper, power 40) can move but cannot kick Bob from the server
	_, carolRecOK := carol, true
	_ = carolRecOK
	var talk1 uint32
	for _, ch := range carol.State().Channels {
		if ch.Name == "Talk 1" {
			talk1 = ch.ID
		}
	}
	bobID := bob.State().MyID
	carol.HandleCommand("move", mustJSON(map[string]any{"id": bobID, "channel": talk1}))
	time.Sleep(200 * time.Millisecond)
	if bob.State().MyChannel == talk1 {
		t.Fatal("Helper moved an admin with more power")
	}

	// --- default role permissions apply to everyone
	var guest proto.RoleInfo
	for _, r := range alice.State().Roles {
		if r.Default {
			guest = r
		}
	}
	var perms []string
	for _, p := range guest.Perms {
		if p != proto.PermChannelCreateTemp {
			perms = append(perms, p)
		}
	}
	guest.Perms = perms
	alice.HandleCommand("role_save", mustJSON(guest))
	_, rec2 := carol, carolRecorder(carol)
	waitFor(t, "guest role updated", func() bool {
		for _, r := range carol.State().Roles {
			if r.Default {
				for _, p := range r.Perms {
					if p == proto.PermChannelCreateTemp {
						return false
					}
				}
				return true
			}
		}
		return false
	})
	carol.HandleCommand("chan_create", mustJSON(map[string]any{"name": "Not allowed"}))
	waitFor(t, "guest cannot create channel anymore", hasErr(rec2, "permission", "cannot create channels"))

	// --- the owner can do everything, including kicking an admin
	alice.HandleCommand("kick", mustJSON(map[string]any{"id": bobID, "server": true}))
	waitFor(t, "owner kicks admin", func() bool { return !bob.State().Connected })

	// --- Bob's role persists by identity
	bob.Close()
	bob, _ = newTestClientDir(t, bobDir, host, port, "Bob")
	waitFor(t, "bob still admin", func() bool { return has(bob.State().Clients[indexOfMe(bob)].Roles, 2) })

	// --- member list
	alice.HandleCommand("member_list", nil)
	waitFor(t, "members", func() bool {
		return func() bool {
			aliceRec.mu.Lock()
			defer aliceRec.mu.Unlock()
			for _, e := range aliceRec.events {
				if l, ok := e.v.([]map[string]any); ok && e.t == "members" && len(l) == 3 {
					return true
				}
			}
			return false
		}()
	})
}

func carolRecorder(c *Client) *recorder { r, _ := c.em.(*recorder); return r }

func indexOfMe(c *Client) int {
	for i, u := range c.State().Clients {
		if u.Me {
			return i
		}
	}
	return 0
}

// An attacker who copies someone's public key, ephemeral key and signature
// (all visible to every user) must not be able to log in as them.
func TestIdentityReplayRejected(t *testing.T) {
	_, host, port := startServer(t)
	alice, _ := newTestClient(t, host, port, "Alice")
	alice.HandleCommand("token_use", mustJSON(map[string]any{"token": ownerToken}))
	waitFor(t, "owner", func() bool { return alice.State().Owner })
	me := clientByNick(alice, "Alice")
	_ = me
	alice.mu.Lock()
	idPub := alice.id.Pub
	eph, sig := alice.sess.keys.EphPub, alice.sess.keys.Sig
	alice.mu.Unlock()

	conn, err := tls.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	attacker := e2e.NewIdentity()
	cs := conn.ConnectionState()
	ekm, _ := cs.ExportKeyingMaterial(proto.AuthEKMLabel, nil, 32)
	hello, _ := proto.Encode(proto.THello, proto.Hello{Version: proto.Version, Nick: "Alice2",
		IDPub: idPub, EphPub: eph, Sig: sig, AuthSig: e2e.AuthSign(attacker, ekm)})
	proto.WriteFrame(conn, proto.FrameJSON, hello)
	_, payload, err := proto.ReadFrame(bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	var m proto.Message
	json.Unmarshal(payload, &m)
	var e proto.Error
	json.Unmarshal(m.D, &e)
	if m.T != proto.TError || e.Code != "auth" {
		t.Fatalf("replayed identity was accepted: %s %s", m.T, payload)
	}
}

func TestServerLogs(t *testing.T) {
	_, host, port := startServer(t)
	alice, aliceRec := newTestClient(t, host, port, "Alice")
	bob, bobRec := newTestClient(t, host, port, "Bob")
	bob.HandleCommand("server_logs", mustJSON(map[string]any{}))
	waitFor(t, "guest refused", hasErr(bobRec, "permission", "server logs"))

	alice.HandleCommand("token_use", mustJSON(map[string]any{"token": ownerToken}))
	waitFor(t, "alice owner", func() bool { return alice.State().Owner })
	alice.HandleCommand("server_logs", mustJSON(map[string]any{"since": 0}))
	waitFor(t, "logs arrive", func() bool { return len(aliceRec.all("logs")) > 0 })
	aliceRec.mu.Lock()
	defer aliceRec.mu.Unlock()
	for _, e := range aliceRec.events {
		if l, ok := e.v.(proto.Logs); ok && e.t == "logs" {
			joined := ""
			for _, x := range l.Lines {
				joined += x.Text + "\n"
			}
			if !strings.Contains(joined, "server started") || !strings.Contains(joined, "Bob") {
				t.Fatalf("unexpected log content:\n%s", joined)
			}
			return
		}
	}
	t.Fatal("no proto.Logs event")
}
