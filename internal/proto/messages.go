package proto

// Message type names.
const (
	// client -> server
	THello       = "hello"
	TJoin        = "join"
	TChanCreate  = "chan_create"
	TChanEdit    = "chan_edit"
	TChanDelete  = "chan_delete"
	TChat        = "chat"
	TKeyDist     = "keydist"
	TStatus      = "status"
	TKick        = "kick"
	TBan         = "ban"
	TMove        = "move"
	TServerMute  = "server_mute"
	TServerEdit  = "server_edit"
	TBanList     = "ban_list"
	TUnban       = "unban"
	TPing        = "ping"
	TNickChange  = "nick"
	TTokenUse    = "token_use"    // redeem a one-time privilege key
	TTokenCreate = "token_create" // create a privilege key for a role
	TTokenList   = "token_list"
	TTokenDelete = "token_delete"
	TRoleSave    = "role_save" // create (id 0) or update a role
	TRoleDelete  = "role_delete"
	TClientRoles = "client_roles" // set the roles of a user (online id or identity key)
	TMemberList  = "member_list"
	TServerPw    = "server_password" // set or remove the server password
	TWatch       = "watch"           // start/stop watching a screen share
	TKeyframeReq = "keyframe_req"    // ask a sharer for a video keyframe
	TFileCtl     = "file_ctl"        // both directions: E2E-encrypted transfer control
	TFileData    = "file_data"       // both directions: E2E-encrypted file chunk
	TChanIcon    = "chan_icon"       // set or remove a permanent channel's icon
	TIconGet     = "icon_get"        // ask for a custom channel icon by hash

	// server -> client
	TWelcome      = "welcome"
	TError        = "error"
	TChanAdd      = "chan_add"
	TChanUpdate   = "chan_update"
	TChanRemove   = "chan_remove"
	TClientJoin   = "client_join"
	TClientUpdate = "client_update"
	TClientLeave  = "client_leave"
	TClientMove   = "client_move"
	TKicked       = "kicked"
	TPong         = "pong"
	TServerUpdate = "server_update"
	TNotice       = "notice"
	TRoles        = "roles"     // full role list (sent on change)
	TToken        = "token"     // a freshly created privilege key (shown once)
	TTokens       = "tokens"    // privilege key list
	TMembers      = "members"   // identities with roles
	TIconData     = "icon_data" // a custom channel icon (answer to icon_get)
)

type ChannelInfo struct {
	ID          uint32 `json:"id"`
	Parent      uint32 `json:"parent"`
	Name        string `json:"name"`
	Topic       string `json:"topic,omitempty"`
	Description string `json:"desc,omitempty"`
	HasPassword bool   `json:"pw,omitempty"`
	MaxUsers    int    `json:"max,omitempty"`
	Permanent   bool   `json:"perm,omitempty"`
	Default     bool   `json:"def,omitempty"`
	Order       int    `json:"order,omitempty"`
	Owner       uint32 `json:"owner,omitempty"`
	Icon        string `json:"icon,omitempty"` // "std:<name>" built-in icon, "img:<sha256 hex>" custom PNG
}

// ChanIcon sets a channel icon: Icon "std:<name>", or PNG for a custom one,
// or neither to remove it.
type ChanIcon struct {
	ID   uint32 `json:"id"`
	Icon string `json:"icon,omitempty"`
	PNG  []byte `json:"png,omitempty"`
}

// IconData carries a custom channel icon (PNG, at most MaxIconSize pixels).
type IconData struct {
	Hash string `json:"hash"`
	PNG  []byte `json:"png,omitempty"` // empty: unknown icon
}

const (
	MaxIconSize  = 64        // pixels per side
	MaxIconBytes = 64 * 1024 // encoded PNG
)

type ClientInfo struct {
	ID          uint32   `json:"id"`
	Nick        string   `json:"nick"`
	Channel     uint32   `json:"ch"`
	IDPub       []byte   `json:"idpub"`
	EphPub      []byte   `json:"eph"`
	Sig         []byte   `json:"sig"`
	Roles       []uint32 `json:"roles,omitempty"`
	Owner       bool     `json:"owner,omitempty"`
	Muted       bool     `json:"muted,omitempty"`
	Deaf        bool     `json:"deaf,omitempty"`
	ServerMuted bool     `json:"smuted,omitempty"`
	Away        bool     `json:"away,omitempty"`
	AwayMsg     string   `json:"awaymsg,omitempty"`
	Sharing     bool     `json:"sharing,omitempty"`
}

type Hello struct {
	Version        int    `json:"v"`
	Nick           string `json:"nick"`
	ServerPassword string `json:"spw,omitempty"`
	IDPub          []byte `json:"idpub"`
	EphPub         []byte `json:"eph"`
	Sig            []byte `json:"sig"`
	// AuthSig proves possession of the identity key: an Ed25519 signature
	// over keying material exported from this very TLS connection, so it
	// cannot be replayed on another connection.
	AuthSig []byte `json:"auth"`
}

type Welcome struct {
	YourID     uint32        `json:"you"`
	ServerName string        `json:"name"`
	WelcomeMsg string        `json:"welcome,omitempty"`
	UDPPort    int           `json:"udp"`
	UDPKey     []byte        `json:"udpkey"`
	Channels   []ChannelInfo `json:"channels"`
	Clients    []ClientInfo  `json:"clients"`
	MaxClients int           `json:"max"`
	Whisper    bool          `json:"whisper"`
	HasPw      bool          `json:"haspw,omitempty"`
	Roles      []RoleInfo    `json:"roles"`
}

type Error struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

type Join struct {
	Channel  uint32 `json:"ch"`
	Password string `json:"pw,omitempty"`
}

type ChanCreate struct {
	Parent      uint32 `json:"parent"`
	Name        string `json:"name"`
	Topic       string `json:"topic,omitempty"`
	Description string `json:"desc,omitempty"`
	Password    string `json:"pw,omitempty"`
	MaxUsers    int    `json:"max,omitempty"`
	Permanent   bool   `json:"perm,omitempty"`
}

type ChanEdit struct {
	ID          uint32  `json:"id"`
	Name        string  `json:"name"`
	Topic       string  `json:"topic"`
	Description string  `json:"desc"`
	Password    *string `json:"pw,omitempty"` // nil = unchanged, "" = remove
	MaxUsers    int     `json:"max"`
	Permanent   bool    `json:"perm"`
	Default     bool    `json:"def"`
	Order       int     `json:"order"`
}

type IDOnly struct {
	ID uint32 `json:"id"`
}

// Chat is an end-to-end encrypted text envelope. CT is encrypted with a random
// message key; Wraps holds that key encrypted for each recipient with the
// pairwise key between sender and recipient. The server only routes it.
type Chat struct {
	Kind   string            `json:"kind"` // channel | private | server | poke
	Target uint32            `json:"target"`
	From   uint32            `json:"from,omitempty"` // set by server
	TS     int64             `json:"ts,omitempty"`   // set by server
	Nonce  []byte            `json:"nonce"`
	CT     []byte            `json:"ct"`
	Wraps  map[string][]byte `json:"wraps"`
}

type KeyDist struct {
	To   uint32 `json:"to,omitempty"`
	From uint32 `json:"from,omitempty"`
	Blob []byte `json:"blob"`
}

type Status struct {
	Muted   bool   `json:"muted"`
	Deaf    bool   `json:"deaf"`
	Away    bool   `json:"away"`
	AwayMsg string `json:"awaymsg,omitempty"`
	Sharing bool   `json:"sharing,omitempty"`
}

type Kick struct {
	ID     uint32 `json:"id"`
	Reason string `json:"reason,omitempty"`
	Server bool   `json:"server"`
}

type Ban struct {
	ID      uint32 `json:"id"`
	Minutes int    `json:"minutes"` // 0 = permanent
	Reason  string `json:"reason,omitempty"`
}

type BanInfo struct {
	IP     string `json:"ip"`
	Nick   string `json:"nick"`
	Reason string `json:"reason,omitempty"`
	Until  int64  `json:"until"` // unix seconds, 0 = permanent
	By     string `json:"by,omitempty"`
}

type BanList struct {
	Bans []BanInfo `json:"bans"`
}

type Unban struct {
	IP string `json:"ip"`
}

type Move struct {
	ID      uint32 `json:"id"`
	Channel uint32 `json:"ch"`
	By      uint32 `json:"by,omitempty"`
}

type ServerMute struct {
	ID uint32 `json:"id"`
	On bool   `json:"on"`
}

type ServerEdit struct {
	Name       string `json:"name"`
	WelcomeMsg string `json:"welcome"`
	HasPw      bool   `json:"haspw,omitempty"`
}

// ServerPassword sets (Password) or removes (Remove) the connect password.
type ServerPassword struct {
	Password string `json:"pw,omitempty"`
	Remove   bool   `json:"remove,omitempty"`
}

// Watch subscribes to (On) or leaves a user's screen share.
type Watch struct {
	ID     uint32 `json:"id"`
	On     bool   `json:"on"`
	Codecs []int  `json:"codecs,omitempty"` // video codecs the viewer can decode (empty: VP8/VP9 only)
}

// KeyframeReq: client -> server names the sharer (To); server -> sharer
// names the viewer (From).
type KeyframeReq struct {
	To     uint32 `json:"to,omitempty"`
	From   uint32 `json:"from,omitempty"`
	Codecs []int  `json:"codecs,omitempty"` // server -> sharer: what the viewer can decode
}

type ClientLeave struct {
	ID     uint32 `json:"id"`
	Reason string `json:"reason,omitempty"`
}

type Kicked struct {
	Reason string `json:"reason,omitempty"`
	By     string `json:"by,omitempty"`
}

type Ping struct {
	TS int64 `json:"ts"`
}

type Notice struct {
	Msg string `json:"msg"`
}

type Nick struct {
	Nick string `json:"nick"`
}

// FileCtl carries a pairwise-encrypted control message for a file transfer.
type FileCtl struct {
	To   uint32 `json:"to,omitempty"`
	From uint32 `json:"from,omitempty"`
	Blob []byte `json:"blob"`
}

// FileData is one chunk, encrypted with the per-transfer key.
type FileData struct {
	To   uint32 `json:"to,omitempty"`
	From uint32 `json:"from,omitempty"`
	ID   string `json:"id"`
	Idx  int    `json:"idx"`
	Data []byte `json:"data"`
}

type RoleInfo struct {
	ID      uint32   `json:"id"`
	Name    string   `json:"name"`
	Color   string   `json:"color,omitempty"`
	Power   int      `json:"power"`
	Perms   []string `json:"perms"`
	Default bool     `json:"default,omitempty"` // implicitly held by everyone
}

type Roles struct {
	Roles []RoleInfo `json:"roles"`
}

type TokenUse struct {
	Token string `json:"token"`
}

type TokenCreate struct {
	Role uint32 `json:"role"`
	Note string `json:"note,omitempty"`
}

type TokenInfo struct {
	ID      string `json:"id"`
	Role    uint32 `json:"role"`
	Owner   bool   `json:"owner,omitempty"`
	Note    string `json:"note,omitempty"`
	By      string `json:"by,omitempty"`
	Created int64  `json:"created"`
}

type TokenList struct {
	Tokens []TokenInfo `json:"tokens"`
}

// Token carries a newly created privilege key; it is never shown again.
type Token struct {
	Token string `json:"token"`
	Role  uint32 `json:"role"`
	Note  string `json:"note,omitempty"`
}

type TokenID struct {
	ID string `json:"id"`
}

// ClientRoles sets the roles of an online client (ID) or a known identity (Key).
type ClientRoles struct {
	ID    uint32   `json:"id,omitempty"`
	Key   []byte   `json:"key,omitempty"`
	Roles []uint32 `json:"roles"`
}

type MemberInfo struct {
	Key      []byte   `json:"key"`
	Nick     string   `json:"nick"`
	Roles    []uint32 `json:"roles"`
	Owner    bool     `json:"owner,omitempty"`
	LastSeen int64    `json:"lastSeen"`
	Online   bool     `json:"online,omitempty"`
}

type MemberList struct {
	Members []MemberInfo `json:"members"`
}
