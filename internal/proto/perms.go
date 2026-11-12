package proto

import "sort"

// Permissions. Roles grant sets of these; a user's effective permissions are
// the union of the default role and every role assigned to their identity.
const (
	PermChannelCreateTemp = "channel.create.temporary"
	PermChannelCreatePerm = "channel.create.permanent"
	PermChannelEdit       = "channel.edit"
	PermChannelDelete     = "channel.delete"
	PermChannelJoinAny    = "channel.join.any"

	PermVoiceSpeak   = "voice.speak"
	PermVoiceWhisper = "voice.whisper"
	PermScreenShare  = "screen.share"

	PermChatChannel = "chat.channel"
	PermChatServer  = "chat.server"
	PermChatPrivate = "chat.private"
	PermPoke        = "poke"
	PermFiles       = "file.transfer"

	PermClientMove        = "client.move"
	PermClientKickChannel = "client.kick.channel"
	PermClientKickServer  = "client.kick.server"
	PermClientBan         = "client.ban"
	PermClientMute        = "client.mute"
	PermBanList           = "ban.list"

	PermRolesAssign    = "roles.assign"
	PermRolesManage    = "roles.manage"
	PermTokensCreate   = "tokens.create"
	PermServerEdit     = "server.edit"
	PermServerPassword = "server.password"
)

// PermInfo describes a permission for the role editor.
type PermInfo struct {
	ID    string `json:"id"`
	Group string `json:"group"`
	Label string `json:"label"`
}

// PermCatalog lists every permission in display order.
var PermCatalog = []PermInfo{
	{PermChannelCreateTemp, "Channels", "Create temporary channels"},
	{PermChannelCreatePerm, "Channels", "Create permanent channels"},
	{PermChannelEdit, "Channels", "Edit any channel"},
	{PermChannelDelete, "Channels", "Delete any channel"},
	{PermChannelJoinAny, "Channels", "Join any channel (ignore password and user limit)"},
	{PermVoiceSpeak, "Voice & chat", "Speak"},
	{PermVoiceWhisper, "Voice & chat", "Whisper"},
	{PermScreenShare, "Voice & chat", "Share screen"},
	{PermChatChannel, "Voice & chat", "Channel chat"},
	{PermChatServer, "Voice & chat", "Server-wide chat"},
	{PermChatPrivate, "Voice & chat", "Private messages"},
	{PermPoke, "Voice & chat", "Poke users"},
	{PermFiles, "Voice & chat", "Send files"},
	{PermClientMove, "Moderation", "Move users"},
	{PermClientKickChannel, "Moderation", "Kick users from channels"},
	{PermClientKickServer, "Moderation", "Kick users from the server"},
	{PermClientMute, "Moderation", "Server-mute users"},
	{PermClientBan, "Moderation", "Ban users"},
	{PermBanList, "Moderation", "View ban list and unban"},
	{PermRolesAssign, "Administration", "Assign roles to users"},
	{PermRolesManage, "Administration", "Create, edit and delete roles"},
	{PermTokensCreate, "Administration", "Create privilege keys"},
	{PermServerEdit, "Administration", "Edit server name and welcome message"},
	{PermServerPassword, "Administration", "Change the server password"},
}

// PowerOwner is the effective power of the server owner: above every role.
const PowerOwner = 1 << 30

// MaxRolePower is the highest power a role can have.
const MaxRolePower = 1000

func AllPerms() []string {
	out := make([]string, len(PermCatalog))
	for i, p := range PermCatalog {
		out[i] = p.ID
	}
	return out
}

func ValidPerm(p string) bool {
	for _, x := range PermCatalog {
		if x.ID == p {
			return true
		}
	}
	return false
}

// Effective computes a user's permissions and power from the role list.
func Effective(roles []RoleInfo, assigned []uint32, owner bool) (map[string]bool, int) {
	perms := map[string]bool{}
	if owner {
		for _, p := range PermCatalog {
			perms[p.ID] = true
		}
		return perms, PowerOwner
	}
	has := map[uint32]bool{}
	for _, id := range assigned {
		has[id] = true
	}
	power := 0
	for _, r := range roles {
		if !r.Default && !has[r.ID] {
			continue
		}
		for _, p := range r.Perms {
			perms[p] = true
		}
		if r.Power > power {
			power = r.Power
		}
	}
	return perms, power
}

func SortedPerms(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for p, ok := range m {
		if ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
