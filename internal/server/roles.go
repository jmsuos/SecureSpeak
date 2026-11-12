package server

// Roles, permissions, privilege keys and the server owner.
//
//   - Identities (Ed25519 public keys) are the only notion of "user". A client
//     proves possession of its key on every connection, so roles stored for a
//     key apply automatically when that person connects.
//   - Every role has a power. You can only act on (kick, ban, move, mute,
//     change roles of) users whose power is lower than yours, only hand out
//     roles below your own power, and only grant permissions you have.
//   - The owner (whoever redeemed the first privilege key) has all
//     permissions and power above every role. Nobody else can act on them.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"securespeak/internal/proto"
)

type Role struct {
	ID      uint32   `json:"id"`
	Name    string   `json:"name"`
	Color   string   `json:"color,omitempty"`
	Power   int      `json:"power"`
	Perms   []string `json:"perms"`
	Default bool     `json:"default,omitempty"`
}

type Member struct {
	Key      string   `json:"key"` // hex identity public key
	Nick     string   `json:"nick"`
	Roles    []uint32 `json:"roles"`
	Owner    bool     `json:"owner,omitempty"`
	LastSeen int64    `json:"last_seen"`
}

type PrivKey struct {
	ID      string `json:"id"`
	Hash    string `json:"hash"` // sha256 of the normalized key
	Role    uint32 `json:"role"`
	Owner   bool   `json:"owner,omitempty"`
	Note    string `json:"note,omitempty"`
	By      string `json:"by,omitempty"`
	Created int64  `json:"created"`
}

const (
	RoleGuest     uint32 = 1
	RoleAdmin     uint32 = 2
	RoleModerator uint32 = 3
)

func defaultRoles(allowTemp bool) []*Role {
	guest := []string{
		proto.PermVoiceSpeak, proto.PermVoiceWhisper, proto.PermScreenShare, proto.PermChatChannel, proto.PermChatServer,
		proto.PermChatPrivate, proto.PermPoke, proto.PermFiles,
	}
	if allowTemp {
		guest = append(guest, proto.PermChannelCreateTemp)
	}
	mod := append(append([]string{}, guest...),
		proto.PermChannelCreateTemp, proto.PermChannelCreatePerm, proto.PermChannelEdit, proto.PermChannelJoinAny,
		proto.PermClientMove, proto.PermClientKickChannel, proto.PermClientKickServer, proto.PermClientMute,
		proto.PermClientBan, proto.PermBanList)
	return []*Role{
		{ID: RoleGuest, Name: "Guest", Power: 0, Perms: dedupe(guest), Default: true},
		{ID: RoleAdmin, Name: "Server Admin", Color: "#b79bff", Power: 100, Perms: proto.AllPerms()},
		{ID: RoleModerator, Name: "Moderator", Color: "#39d98a", Power: 50, Perms: dedupe(mod)},
	}
}

func dedupe(in []string) []string {
	m := map[string]bool{}
	for _, p := range in {
		m[p] = true
	}
	return proto.SortedPerms(m)
}

// ---- persistence ---------------------------------------------------------------

func (s *Server) loadRoles() {
	load := func(name string, v any) bool {
		b, err := os.ReadFile(filepath.Join(s.dataDir, name))
		if err != nil {
			return false
		}
		if err := json.Unmarshal(b, v); err != nil {
			s.Logf("%s: %v", name, err)
			return false
		}
		return true
	}
	if !load("roles.json", &s.roles) || len(s.roles) == 0 {
		s.roles = defaultRoles(s.cfg.AllowTempChannels)
		s.saveRolesLocked()
	}
	hasDefault := false
	for _, r := range s.roles {
		if r.ID >= s.nextRole {
			s.nextRole = r.ID + 1
		}
		if r.Default {
			hasDefault = true
		}
	}
	if !hasDefault {
		r := defaultRoles(true)[0]
		r.ID = s.nextRole
		s.nextRole++
		s.roles = append(s.roles, r)
		s.saveRolesLocked()
	}
	s.migrateRolesLocked()
	var members []*Member
	load("members.json", &members)
	s.members = map[string]*Member{}
	for _, m := range members {
		s.members[m.Key] = m
	}
	load("privilege_keys.json", &s.keys)
}

// migrateRolesLocked gives existing roles the permissions added in later
// versions, mirroring what they could already do. Runs once per schema bump.
func (s *Server) migrateRolesLocked() {
	const current = 2
	if s.cfg.RolesSchema >= current {
		return
	}
	for _, r := range s.roles {
		has := map[string]bool{}
		for _, p := range r.Perms {
			has[p] = true
		}
		if has[proto.PermServerEdit] { // 3.1: server password
			has[proto.PermServerPassword] = true
		}
		if has[proto.PermVoiceSpeak] { // 3.1: screen share
			has[proto.PermScreenShare] = true
		}
		r.Perms = proto.SortedPerms(has)
	}
	s.cfg.RolesSchema = current
	s.saveRolesLocked()
	if err := SaveJSON(s.cfgPath, s.cfg); err != nil {
		s.Logf("save config: %v", err)
	}
}

func (s *Server) saveRolesLocked() {
	sort.Slice(s.roles, func(i, j int) bool {
		if s.roles[i].Power != s.roles[j].Power {
			return s.roles[i].Power > s.roles[j].Power
		}
		return s.roles[i].ID < s.roles[j].ID
	})
	if err := SaveJSON(filepath.Join(s.dataDir, "roles.json"), s.roles); err != nil {
		s.Logf("save roles: %v", err)
	}
}

func (s *Server) saveMembersLocked() {
	list := make([]*Member, 0, len(s.members))
	for _, m := range s.members {
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	if err := SaveJSON(filepath.Join(s.dataDir, "members.json"), list); err != nil {
		s.Logf("save members: %v", err)
	}
}

func (s *Server) saveKeysLocked() {
	if s.keys == nil {
		s.keys = []*PrivKey{}
	}
	if err := SaveJSON(filepath.Join(s.dataDir, "privilege_keys.json"), s.keys); err != nil {
		s.Logf("save privilege keys: %v", err)
	}
}

// ---- privilege keys ---------------------------------------------------------------

func normalizeToken(t string) string {
	t = strings.ToUpper(t)
	t = strings.TrimPrefix(strings.TrimSpace(t), "SS-")
	return strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7') {
			return r
		}
		return -1
	}, t)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte("securespeak-privkey-v1|" + normalizeToken(t)))
	return hex.EncodeToString(h[:])
}

func newToken() string {
	b := make([]byte, 20)
	rand.Read(b)
	raw := base32.StdEncoding.EncodeToString(b) // 32 chars, 160 bits
	var parts []string
	for i := 0; i < len(raw); i += 4 {
		parts = append(parts, raw[i:i+4])
	}
	return "SS-" + strings.Join(parts, "-")
}

func (s *Server) createKeyLocked(role uint32, owner bool, note, by string) string {
	t := newToken()
	h := hashToken(t)
	s.keys = append(s.keys, &PrivKey{ID: h[:10], Hash: h, Role: role, Owner: owner, Note: note, By: by, Created: time.Now().Unix()})
	s.saveKeysLocked()
	return t
}

func (s *Server) ownerTokenPath() string { return filepath.Join(s.dataDir, "owner-token.txt") }

// EnsureOwnerToken creates a one-time owner key when the server has no owner
// and no pending owner key. It returns the new key ("" if none was created).
func (s *Server) EnsureOwnerToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.members {
		if m.Owner {
			return ""
		}
	}
	for _, k := range s.keys {
		if k.Owner {
			return ""
		}
	}
	return s.newOwnerTokenLocked()
}

// ResetOwner removes the owner flag from everyone and issues a new owner key
// (for when the owner lost their identity).
func (s *Server) ResetOwner() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.members {
		m.Owner = false
	}
	s.saveMembersLocked()
	kept := s.keys[:0]
	for _, k := range s.keys {
		if !k.Owner {
			kept = append(kept, k)
		}
	}
	s.keys = kept
	return s.newOwnerTokenLocked()
}

func (s *Server) newOwnerTokenLocked() string {
	t := s.createKeyLocked(RoleAdmin, true, "server owner", "server")
	msg := "SecureSpeak owner privilege key (one-time use):\n\n    " + t + "\n\n" +
		"In the client: connect, then Admin -> Use privilege key. The identity that\n" +
		"redeems it becomes the permanent server owner. This file is deleted on use.\n"
	if err := os.WriteFile(s.ownerTokenPath(), []byte(msg), 0600); err != nil {
		s.Logf("write owner token: %v", err)
	}
	return t
}

// PendingOwnerToken returns the text of an unused owner key file, if any.
func (s *Server) PendingOwnerToken() string {
	b, err := os.ReadFile(s.ownerTokenPath())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "SS-") {
			return t
		}
	}
	return ""
}

// ---- effective permissions -----------------------------------------------------------

func (s *Server) roleInfosLocked() []proto.RoleInfo {
	out := make([]proto.RoleInfo, 0, len(s.roles))
	for _, r := range s.roles {
		out = append(out, proto.RoleInfo{ID: r.ID, Name: r.Name, Color: r.Color, Power: r.Power, Perms: r.Perms, Default: r.Default})
	}
	return out
}

func (s *Server) roleLocked(id uint32) *Role {
	for _, r := range s.roles {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (s *Server) effectiveLocked(roles []uint32, owner bool) (map[string]bool, int) {
	return proto.Effective(s.roleInfosLocked(), roles, owner)
}

func (s *Server) hasLocked(c *Client, perm string) bool {
	p, _ := s.effectiveLocked(c.Roles, c.Owner)
	return p[perm]
}

func (s *Server) powerLocked(c *Client) int {
	_, pw := s.effectiveLocked(c.Roles, c.Owner)
	return pw
}

// canActLocked reports whether actor may moderate target.
func (s *Server) canActLocked(actor, target *Client) bool {
	if target.Owner {
		return actor.Owner && actor != target
	}
	if actor.Owner {
		return true
	}
	return s.powerLocked(actor) > s.powerLocked(target)
}

func (s *Server) memberOf(c *Client) *Member {
	m := s.members[c.Key]
	if m == nil {
		m = &Member{Key: c.Key}
		s.members[c.Key] = m
	}
	return m
}

// applyMemberLocked copies a member's roles to every online session of that
// identity and broadcasts the change.
func (s *Server) applyMemberLocked(m *Member) {
	for _, c := range s.clients {
		if c.Key == m.Key {
			c.Roles = append([]uint32(nil), m.Roles...)
			c.Owner = m.Owner
			s.broadcastLocked(proto.TClientUpdate, s.clientInfo(c), 0)
		}
	}
}

// ---- message handlers ------------------------------------------------------------------

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (s *Server) handleRoles(c *Client, m proto.Message) (handled, keep bool) {
	switch m.T {
	case proto.TTokenUse:
		t, _ := decode[proto.TokenUse](m)
		return true, s.useToken(c, t.Token)
	case proto.TTokenCreate, proto.TTokenList, proto.TTokenDelete,
		proto.TRoleSave, proto.TRoleDelete, proto.TClientRoles, proto.TMemberList:
	default:
		return false, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	perms, power := s.effectiveLocked(c.Roles, c.Owner)
	deny := func(msg string) { c.sendErr("permission", msg) }
	switch m.T {
	case proto.TTokenCreate:
		tc, _ := decode[proto.TokenCreate](m)
		r := s.roleLocked(tc.Role)
		switch {
		case !perms[proto.PermTokensCreate]:
			deny("you cannot create privilege keys")
		case r == nil || r.Default:
			c.sendErr("role", "choose a role for the key")
		case !c.Owner && r.Power >= power:
			deny("you can only create keys for roles below your own power")
		default:
			t := s.createKeyLocked(r.ID, false, cleanName(tc.Note, 80), c.Nick)
			c.send(proto.TToken, proto.Token{Token: t, Role: r.ID, Note: cleanName(tc.Note, 80)})
			s.Logf("%q created a privilege key for role %q", c.Nick, r.Name)
		}
	case proto.TTokenList:
		if !perms[proto.PermTokensCreate] {
			deny("you cannot view privilege keys")
			break
		}
		c.send(proto.TTokens, s.tokenListLocked(c.Owner))
	case proto.TTokenDelete:
		id, _ := decode[proto.TokenID](m)
		if !perms[proto.PermTokensCreate] {
			deny("you cannot delete privilege keys")
			break
		}
		kept := s.keys[:0]
		for _, k := range s.keys {
			r := s.roleLocked(k.Role)
			allowed := c.Owner || (!k.Owner && r != nil && r.Power < power)
			if k.ID == id.ID && allowed {
				continue
			}
			kept = append(kept, k)
		}
		s.keys = kept
		s.saveKeysLocked()
		c.send(proto.TTokens, s.tokenListLocked(c.Owner))
	case proto.TRoleSave:
		ri, _ := decode[proto.RoleInfo](m)
		s.saveRole(c, perms, power, ri)
	case proto.TRoleDelete:
		id, _ := decode[proto.IDOnly](m)
		r := s.roleLocked(id.ID)
		switch {
		case !perms[proto.PermRolesManage]:
			deny("you cannot manage roles")
		case r == nil:
		case r.Default:
			c.sendErr("role", "the default role cannot be deleted")
		case !c.Owner && r.Power >= power:
			deny("you can only delete roles below your own power")
		default:
			kept := s.roles[:0]
			for _, o := range s.roles {
				if o.ID != r.ID {
					kept = append(kept, o)
				}
			}
			s.roles = kept
			for _, mem := range s.members {
				mem.Roles = without(mem.Roles, r.ID)
			}
			kk := s.keys[:0]
			for _, k := range s.keys {
				if k.Role != r.ID {
					kk = append(kk, k)
				}
			}
			s.keys = kk
			s.saveRolesLocked()
			s.saveMembersLocked()
			s.saveKeysLocked()
			s.broadcastLocked(proto.TRoles, proto.Roles{Roles: s.roleInfosLocked()}, 0)
			for _, o := range s.clients {
				if containsID(o.Roles, r.ID) {
					o.Roles = without(o.Roles, r.ID)
					s.broadcastLocked(proto.TClientUpdate, s.clientInfo(o), 0)
				}
			}
			s.Logf("%q deleted role %q", c.Nick, r.Name)
		}
	case proto.TClientRoles:
		cr, _ := decode[proto.ClientRoles](m)
		s.setRoles(c, perms, power, cr)
	case proto.TMemberList:
		if !perms[proto.PermRolesAssign] {
			deny("you cannot view members")
			break
		}
		c.send(proto.TMembers, s.memberListLocked())
	}
	return true, true
}

func (s *Server) useToken(c *Client, token string) bool {
	h := hashToken(token)
	s.mu.Lock()
	var key *PrivKey
	kept := s.keys[:0]
	for _, k := range s.keys {
		if key == nil && k.Hash == h {
			key = k
			continue
		}
		kept = append(kept, k)
	}
	if key == nil {
		s.mu.Unlock()
		c.failedAuth++
		s.Logf("client %d %q used an invalid privilege key from %s", c.ID, c.Nick, c.ip)
		time.Sleep(time.Second)
		c.sendErr("token", "invalid or already used privilege key")
		return c.failedAuth < 5
	}
	s.keys = kept
	s.saveKeysLocked()
	mem := s.memberOf(c)
	mem.Nick = c.Nick
	role := s.roleLocked(key.Role)
	if role != nil && !role.Default && !containsID(mem.Roles, role.ID) {
		mem.Roles = append(mem.Roles, role.ID)
	}
	msg := ""
	if key.Owner {
		hasOwner := false
		for _, o := range s.members {
			if o.Owner && o != mem {
				hasOwner = true
			}
		}
		if !hasOwner {
			mem.Owner = true
			os.Remove(s.ownerTokenPath())
			msg = "You are now the owner of this server. Your identity is recognized automatically from now on."
		}
	}
	if msg == "" && role != nil {
		msg = fmt.Sprintf("You received the role \"%s\". It is linked to your identity and applies automatically from now on.", role.Name)
	}
	s.saveMembersLocked()
	s.applyMemberLocked(mem)
	s.mu.Unlock()
	if msg != "" {
		c.send(proto.TNotice, proto.Notice{Msg: msg})
	}
	s.Logf("client %d %q redeemed a privilege key (owner=%v)", c.ID, c.Nick, key.Owner)
	return true
}

func (s *Server) saveRole(c *Client, perms map[string]bool, power int, ri proto.RoleInfo) {
	if !perms[proto.PermRolesManage] {
		c.sendErr("permission", "you cannot manage roles")
		return
	}
	name := cleanName(ri.Name, 32)
	if name == "" {
		c.sendErr("role", "role name required")
		return
	}
	if ri.Color != "" && !colorRe.MatchString(ri.Color) {
		ri.Color = ""
	}
	ri.Power = max(0, min(proto.MaxRolePower, ri.Power))
	var newPerms []string
	for _, p := range ri.Perms {
		if !proto.ValidPerm(p) {
			continue
		}
		if !c.Owner && !perms[p] {
			c.sendErr("permission", "you cannot grant a permission you do not have: "+p)
			return
		}
		newPerms = append(newPerms, p)
	}
	for _, o := range s.roles {
		if o.ID != ri.ID && strings.EqualFold(o.Name, name) {
			c.sendErr("role", "a role with that name already exists")
			return
		}
	}
	if ri.ID == 0 {
		if !c.Owner && ri.Power >= power {
			c.sendErr("permission", fmt.Sprintf("new roles must have less power than yours (%d)", power))
			return
		}
		if len(s.roles) >= 100 {
			c.sendErr("role", "too many roles")
			return
		}
		r := &Role{ID: s.nextRole, Name: name, Color: ri.Color, Power: ri.Power, Perms: dedupe(newPerms)}
		s.nextRole++
		s.roles = append(s.roles, r)
		s.Logf("%q created role %q", c.Nick, name)
	} else {
		r := s.roleLocked(ri.ID)
		if r == nil {
			c.sendErr("role", "role does not exist")
			return
		}
		if !c.Owner && (r.Power >= power || ri.Power >= power) {
			c.sendErr("permission", "you can only edit roles below your own power")
			return
		}
		r.Name, r.Color, r.Perms = name, ri.Color, dedupe(newPerms)
		if r.Default {
			r.Power = 0
		} else {
			r.Power = ri.Power
		}
		s.Logf("%q edited role %q", c.Nick, name)
	}
	s.saveRolesLocked()
	s.broadcastLocked(proto.TRoles, proto.Roles{Roles: s.roleInfosLocked()}, 0)
}

func (s *Server) setRoles(c *Client, perms map[string]bool, power int, cr proto.ClientRoles) {
	if !perms[proto.PermRolesAssign] {
		c.sendErr("permission", "you cannot assign roles")
		return
	}
	var mem *Member
	var target *Client
	if cr.ID != 0 {
		target = s.clients[cr.ID]
		if target == nil {
			c.sendErr("role", "user is not online")
			return
		}
		mem = s.memberOf(target)
		mem.Nick = target.Nick
	} else {
		mem = s.members[hex.EncodeToString(cr.Key)]
		if mem == nil {
			c.sendErr("role", "unknown identity")
			return
		}
	}
	if mem.Owner && !c.Owner {
		c.sendErr("permission", "the server owner cannot be changed")
		return
	}
	if !c.Owner {
		if mem.Key == c.Key {
			c.sendErr("permission", "you cannot change your own roles")
			return
		}
		if _, tp := s.effectiveLocked(mem.Roles, mem.Owner); tp >= power {
			c.sendErr("permission", "you can only change roles of users with less power than you")
			return
		}
	}
	want := map[uint32]bool{}
	for _, id := range cr.Roles {
		if r := s.roleLocked(id); r != nil && !r.Default {
			want[id] = true
		}
	}
	have := map[uint32]bool{}
	for _, id := range mem.Roles {
		have[id] = true
	}
	for id := range union(want, have) {
		if want[id] == have[id] {
			continue
		}
		r := s.roleLocked(id)
		if r != nil && !c.Owner && r.Power >= power {
			c.sendErr("permission", fmt.Sprintf("you cannot give or take the role \"%s\"", r.Name))
			return
		}
	}
	mem.Roles = mem.Roles[:0]
	for id := range want {
		mem.Roles = append(mem.Roles, id)
	}
	sort.Slice(mem.Roles, func(i, j int) bool { return mem.Roles[i] < mem.Roles[j] })
	if len(mem.Roles) == 0 && !mem.Owner {
		mem.Roles = nil
	}
	s.saveMembersLocked()
	s.applyMemberLocked(mem)
	if target != nil && target != c {
		target.send(proto.TNotice, proto.Notice{Msg: fmt.Sprintf("%s changed your roles.", c.Nick)})
	}
	s.Logf("%q set roles of %q to %v", c.Nick, mem.Nick, mem.Roles)
	if cr.ID == 0 {
		c.send(proto.TMembers, s.memberListLocked())
	}
}

func (s *Server) tokenListLocked(owner bool) proto.TokenList {
	l := proto.TokenList{Tokens: []proto.TokenInfo{}}
	for _, k := range s.keys {
		if k.Owner && !owner {
			continue
		}
		l.Tokens = append(l.Tokens, proto.TokenInfo{ID: k.ID, Role: k.Role, Owner: k.Owner, Note: k.Note, By: k.By, Created: k.Created})
	}
	return l
}

func (s *Server) memberListLocked() proto.MemberList {
	online := map[string]bool{}
	for _, c := range s.clients {
		online[c.Key] = true
	}
	l := proto.MemberList{Members: []proto.MemberInfo{}}
	for _, m := range s.members {
		if len(m.Roles) == 0 && !m.Owner {
			continue
		}
		key, _ := hex.DecodeString(m.Key)
		l.Members = append(l.Members, proto.MemberInfo{Key: key, Nick: m.Nick, Roles: m.Roles, Owner: m.Owner, LastSeen: m.LastSeen, Online: online[m.Key]})
	}
	sort.Slice(l.Members, func(i, j int) bool {
		if l.Members[i].Owner != l.Members[j].Owner {
			return l.Members[i].Owner
		}
		return strings.ToLower(l.Members[i].Nick) < strings.ToLower(l.Members[j].Nick)
	})
	return l
}

func containsID(list []uint32, id uint32) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

func without(list []uint32, id uint32) []uint32 {
	out := list[:0]
	for _, x := range list {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func union(a, b map[uint32]bool) map[uint32]bool {
	out := map[uint32]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}
