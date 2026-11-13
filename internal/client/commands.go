package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"securespeak/internal/e2e"
	"securespeak/internal/hotkey"
	"securespeak/internal/platform"
	"securespeak/internal/proto"
)

// AppVersion is set by the main package (from the build).
var AppVersion = "dev"

type uiClient struct {
	ID          uint32   `json:"id"`
	Nick        string   `json:"nick"`
	Channel     uint32   `json:"channel"`
	Roles       []uint32 `json:"roles"`
	Owner       bool     `json:"owner"`
	Power       int      `json:"power"`
	Muted       bool     `json:"muted"`
	Deaf        bool     `json:"deaf"`
	ServerMuted bool     `json:"serverMuted"`
	Away        bool     `json:"away"`
	AwayMsg     string   `json:"awayMsg"`
	FP          string   `json:"fp"`
	Valid       bool     `json:"valid"`
	Verified    bool     `json:"verified"`
	KeyChanged  bool     `json:"keyChanged"`
	HasKey      bool     `json:"hasKey"`
	Volume      float64  `json:"volume"`
	LocalMute   bool     `json:"localMute"`
	Sharing     bool     `json:"sharing"`
	Me          bool     `json:"me"`
	Avatar      string   `json:"avatar"` // hash, "" = none
}

type uiWhisper struct {
	WhisperList
	KeyName string `json:"keyName"`
}

type uiState struct {
	Connected       bool                `json:"connected"`
	Connecting      bool                `json:"connecting"`
	ServerName      string              `json:"serverName"`
	WelcomeMsg      string              `json:"welcomeMsg"`
	Addr            string              `json:"addr"`
	CertFP          string              `json:"certFP"`
	MyID            uint32              `json:"myID"`
	MyChannel       uint32              `json:"myChannel"`
	Owner           bool                `json:"owner"`
	Perms           []string            `json:"perms"`
	Power           int                 `json:"power"`
	Roles           []proto.RoleInfo    `json:"roles"`
	PermCatalog     []proto.PermInfo    `json:"permCatalog"`
	ServerMuted     bool                `json:"serverMuted"`
	Muted           bool                `json:"muted"`
	Deaf            bool                `json:"deaf"`
	Away            bool                `json:"away"`
	AwayMsg         string              `json:"awayMsg"`
	Channels        []proto.ChannelInfo `json:"channels"`
	Clients         []uiClient          `json:"clients"`
	Nickname        string              `json:"nickname"`
	LastHost        string              `json:"lastHost"`
	LastPort        int                 `json:"lastPort"`
	Bookmarks       []Bookmark          `json:"bookmarks"`
	Identity        string              `json:"identity"`
	Audio           AudioSettings       `json:"audio"`
	PTTKeyName      string              `json:"pttKeyName"`
	HotkeySupported bool                `json:"hotkeySupported"`
	HotkeyInfo      string              `json:"hotkeyInfo"`
	Whisper         []uiWhisper         `json:"whisper"`
	WhisperAllowed  bool                `json:"whisperAllowed"`
	DownloadDir     string              `json:"downloadDir"`
	Avatar          string              `json:"avatar"`
	Platform        string              `json:"platform"`
	Version         string              `json:"version"`
	Sharing         bool                `json:"sharing"`
	ServerHasPw     bool                `json:"serverHasPw"`
	AutoMedia       bool                `json:"autoMedia"`
	LeaveMessage    string              `json:"leaveMessage"`
	JoinSound       *SoundRef           `json:"joinSound"`
	LeaveSound      *SoundRef           `json:"leaveSound"`
	AppSounds       map[string]SoundRef `json:"appSounds"`
	PeerSounds      bool                `json:"peerSounds"`
	Reconnecting    *uiReconnect        `json:"reconnecting"`
	AutoReconnect   bool                `json:"autoReconnect"`
	KeyProtected    bool                `json:"keyProtected"`
	MaxClients      int                 `json:"maxClients"`
}

func (c *Client) stateLocked() uiState {
	st := uiState{
		Connecting: c.connStep != "",
		Muted:      c.muted, Deaf: c.deaf, Away: c.away, AwayMsg: c.awayMsg,
		Nickname: c.cfg.Nickname, LastHost: c.cfg.LastHost, LastPort: c.cfg.LastPort,
		Bookmarks: c.cfg.Bookmarks, Identity: e2e.Fingerprint(c.id.Pub),
		Audio: c.cfg.Audio, PTTKeyName: hotkey.Name(c.cfg.Audio.PTTKey),
		HotkeySupported: hotkey.Supported(), HotkeyInfo: hotkey.Info(),
		Whisper: []uiWhisper{}, DownloadDir: c.downloadDirLocked(), Avatar: c.avatarHash,
		Platform: runtime.GOOS, Version: AppVersion, Sharing: c.sharing, AutoMedia: !c.cfg.NoAutoMedia,
		LeaveMessage: c.cfg.LeaveMessage, JoinSound: c.cfg.JoinSound, LeaveSound: c.cfg.LeaveSound,
		AppSounds: c.cfg.AppSounds, PeerSounds: !c.cfg.NoPeerSounds,
		Reconnecting: c.reconnectStateLocked(), AutoReconnect: !c.cfg.NoAutoReconnect,
		KeyProtected: platform.ProtectsSecrets,
		Channels:     []proto.ChannelInfo{}, Clients: []uiClient{},
		Perms: []string{}, Roles: []proto.RoleInfo{}, PermCatalog: proto.PermCatalog,
	}
	if st.Bookmarks == nil {
		st.Bookmarks = []Bookmark{}
	}
	for _, w := range c.cfg.Whisper {
		if w.Users == nil {
			w.Users = []string{}
		}
		if w.Channels == nil {
			w.Channels = []string{}
		}
		st.Whisper = append(st.Whisper, uiWhisper{WhisperList: w, KeyName: hotkey.Name(w.Key)})
	}
	s := c.sess
	if s == nil {
		return st
	}
	st.Connected = true
	st.ServerName, st.WelcomeMsg, st.Addr, st.CertFP = s.serverName, s.welcomeMsg, s.addr, s.certFP
	st.MyID, st.MyChannel, st.Owner, st.ServerMuted = s.myID, s.me.Channel, s.me.Owner, s.me.ServerMuted
	if s.roles != nil {
		st.Roles = s.roles
	}
	myPerms, myPower := proto.Effective(s.roles, s.me.Roles, s.me.Owner)
	st.Perms, st.Power = proto.SortedPerms(myPerms), myPower
	st.MaxClients = s.maxClients
	st.WhisperAllowed = s.allowWhisper
	st.ServerHasPw = s.hasPw
	for _, ch := range s.channels {
		st.Channels = append(st.Channels, ch)
	}
	sort.Slice(st.Channels, func(i, j int) bool {
		a, b := st.Channels[i], st.Channels[j]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.ID < b.ID
	})
	st.Clients = append(st.Clients, uiClient{
		ID: s.myID, Nick: s.me.Nick, Channel: s.me.Channel, Roles: nonNil(s.me.Roles), Owner: s.me.Owner, Power: myPower,
		Muted: c.muted, Deaf: c.deaf, ServerMuted: s.me.ServerMuted, Away: c.away, AwayMsg: c.awayMsg,
		FP: e2e.Fingerprint(c.id.Pub), Valid: true, Verified: true, HasKey: true, Volume: 1, Me: true,
		Avatar: c.avatarHash, Sharing: c.sharing,
	})
	for _, p := range s.peers {
		i := p.Info
		st.Clients = append(st.Clients, uiClient{
			ID: i.ID, Nick: i.Nick, Channel: i.Channel, Roles: nonNil(i.Roles), Owner: i.Owner, Power: power(s, i), Muted: i.Muted, Deaf: i.Deaf,
			ServerMuted: i.ServerMuted, Away: i.Away, AwayMsg: i.AwayMsg, FP: p.FP, Valid: p.Valid,
			Verified: p.verified, KeyChanged: p.keyChanged, HasKey: len(p.keys) > 0,
			Volume: p.volume, LocalMute: p.localMute, Avatar: p.avatarHash, Sharing: i.Sharing,
		})
	}
	sort.Slice(st.Clients, func(i, j int) bool {
		return strings.ToLower(st.Clients[i].Nick) < strings.ToLower(st.Clients[j].Nick)
	})
	return st
}

func power(s *session, i proto.ClientInfo) int {
	_, p := proto.Effective(s.roles, i.Roles, i.Owner)
	return p
}

func (c *Client) State() uiState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stateLocked()
}

func (c *Client) emitState() {
	c.em.Emit("state", c.State())
}

// ---- command dispatch ----------------------------------------------------------------

type args map[string]json.RawMessage

func (a args) str(k string) string {
	var s string
	json.Unmarshal(a[k], &s)
	return s
}

func (a args) num(k string) float64 {
	var f float64
	json.Unmarshal(a[k], &f)
	return f
}

func (a args) id(k string) uint32 { return uint32(a.num(k)) }

func (a args) boolean(k string) bool {
	var b bool
	json.Unmarshal(a[k], &b)
	return b
}

func (a args) optBool(k string) *bool {
	if _, ok := a[k]; !ok {
		return nil
	}
	b := a.boolean(k)
	return &b
}

// AutoUpdate reports whether SecureSpeak checks for updates automatically.
func (c *Client) AutoUpdate() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.cfg.NoAutoUpdate
}

// SetAutoUpdate turns automatic update checks on or off (persisted).
func (c *Client) SetAutoUpdate(on bool) {
	c.mu.Lock()
	c.cfg.NoAutoUpdate = !on
	c.saveLocked()
	c.mu.Unlock()
}

// ErrQuit is returned by HandleCommand("quit").
var ErrQuit = errors.New("quit")

// HandleCommand executes a UI command. It is safe to call concurrently.
func (c *Client) HandleCommand(cmd string, raw json.RawMessage) (any, error) {
	var a args
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
	}
	switch cmd {
	case "init":
		return map[string]any{"state": c.State()}, nil

	case "connect":
		c.mu.Lock()
		c.cancelReconnectLocked()
		c.mu.Unlock()
		err := c.Connect(a.str("host"), int(a.num("port")), a.str("nick"), a.str("password"), a.str("trust"))
		var ue *UntrustedError
		if errors.As(err, &ue) {
			return map[string]any{"untrusted": true, "addr": ue.Addr, "fp": ue.FP, "known": ue.Known}, nil
		}
		return nil, err

	case "disconnect":
		c.Disconnect()
		return nil, nil

	case "reconnect_cancel":
		c.mu.Lock()
		c.cancelReconnectLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "reconnect_now":
		c.mu.Lock()
		if c.rc != nil {
			select {
			case c.rc.now <- struct{}{}:
			default:
			}
		}
		c.mu.Unlock()
		return nil, nil

	case "auto_reconnect":
		c.mu.Lock()
		c.cfg.NoAutoReconnect = !a.boolean("on")
		if c.cfg.NoAutoReconnect {
			c.cancelReconnectLocked()
		}
		c.saveLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "join":
		if pw := a.str("password"); pw != "" {
			c.mu.Lock()
			if c.chanPw == nil {
				c.chanPw = map[uint32]string{}
			}
			c.chanPw[a.id("id")] = pw
			c.mu.Unlock()
		}
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TJoin, proto.Join{Channel: a.id("id"), Password: a.str("password")})
			return nil
		})

	case "chan_create":
		var cc proto.ChanCreate
		json.Unmarshal(raw, &cc)
		return nil, c.withSession(func(s *session) error { s.send(proto.TChanCreate, cc); return nil })

	case "chan_edit":
		var ce proto.ChanEdit
		json.Unmarshal(raw, &ce)
		return nil, c.withSession(func(s *session) error { s.send(proto.TChanEdit, ce); return nil })

	case "sound_set":
		var ss struct {
			Slot string `json:"slot"`
			Name string `json:"name"`
			Data []byte `json:"data"` // base64 in JSON; empty = remove
		}
		if err := json.Unmarshal(raw, &ss); err != nil {
			return nil, errors.New("bad sound")
		}
		return nil, c.setSound(ss.Slot, ss.Name, ss.Data)

	case "leave_msg":
		return nil, c.setLeaveMessage(a.str("text"))

	case "peer_sounds":
		c.mu.Lock()
		c.cfg.NoPeerSounds = !a.boolean("on")
		c.saveLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "yt_signin":
		return nil, platform.OpenInAppBrowser("https://accounts.google.com/ServiceLogin?service=youtube&continue=https%3A%2F%2Fwww.youtube.com%2F")

	case "watch_send":
		var w struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &w); err != nil || len(w.Data) == 0 || len(w.Data) > 4096 {
			return nil, errors.New("bad watch message")
		}
		return nil, c.withSession(func(s *session) error {
			_, _, err := c.sendBodyLocked(s, "wt", 0, chatBody{Watch: w.Data})
			return err
		})

	case "chan_icon":
		var ci struct {
			ID   uint32 `json:"id"`
			Icon string `json:"icon"`
			PNG  []byte `json:"png"` // base64 in JSON
		}
		if err := json.Unmarshal(raw, &ci); err != nil {
			return nil, errors.New("bad icon")
		}
		return nil, c.setChannelIcon(ci.ID, ci.Icon, ci.PNG)

	case "chan_delete":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TChanDelete, proto.IDOnly{ID: a.id("id")})
			return nil
		})

	case "chat":
		return nil, c.SendChat(a.str("kind"), a.id("target"), a.str("text"))

	case "poke":
		return nil, c.SendChat("poke", a.id("target"), a.str("text"))

	case "status":
		var msg *string
		if _, ok := a["awayMsg"]; ok {
			m := a.str("awayMsg")
			msg = &m
		}
		before := c.State()
		c.SetStatus(a.optBool("muted"), a.optBool("deaf"), a.optBool("away"), msg)
		after := c.State()
		if before.Muted != after.Muted {
			if after.Muted {
				c.playSound("mute")
			} else {
				c.playSound("unmute")
			}
		}
		return nil, nil

	case "nick":
		nick := strings.TrimSpace(a.str("nick"))
		if nick == "" {
			return nil, errors.New("nickname cannot be empty")
		}
		c.mu.Lock()
		c.cfg.Nickname = nick
		c.saveLocked()
		if c.sess != nil {
			c.sess.send(proto.TNickChange, proto.Nick{Nick: nick})
		}
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "server_password":
		return nil, c.withSession(func(s *session) error {
			pw, remove := a.str("password"), a.boolean("remove")
			if !remove && (pw == "" || len(pw) > 128) {
				return errors.New("the password must be 1-128 characters")
			}
			s.send(proto.TServerPw, proto.ServerPassword{Password: pw, Remove: remove})
			return nil
		})

	case "share":
		return nil, c.SetSharing(a.boolean("on"))

	case "watch":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TWatch, proto.Watch{ID: a.id("id"), On: a.boolean("on")})
			return nil
		})

	case "request_keyframe":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TKeyframeReq, proto.KeyframeReq{To: a.id("id")})
			return nil
		})

	case "auto_media":
		c.mu.Lock()
		c.cfg.NoAutoMedia = !a.boolean("on")
		c.saveLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "file_save":
		return c.SaveCachedFile(a.str("id"))

	case "token_use":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TTokenUse, proto.TokenUse{Token: a.str("token")})
			return nil
		})

	case "token_create":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TTokenCreate, proto.TokenCreate{Role: a.id("role"), Note: a.str("note")})
			return nil
		})

	case "token_list":
		return nil, c.withSession(func(s *session) error { s.send(proto.TTokenList, struct{}{}); return nil })

	case "token_delete":
		return nil, c.withSession(func(s *session) error { s.send(proto.TTokenDelete, proto.TokenID{ID: a.str("id")}); return nil })

	case "role_save":
		var r proto.RoleInfo
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		return nil, c.withSession(func(s *session) error { s.send(proto.TRoleSave, r); return nil })

	case "role_delete":
		return nil, c.withSession(func(s *session) error { s.send(proto.TRoleDelete, proto.IDOnly{ID: a.id("id")}); return nil })

	case "client_roles":
		var cr proto.ClientRoles
		if err := json.Unmarshal(raw, &cr); err != nil {
			return nil, err
		}
		return nil, c.withSession(func(s *session) error { s.send(proto.TClientRoles, cr); return nil })

	case "member_list":
		return nil, c.withSession(func(s *session) error { s.send(proto.TMemberList, struct{}{}); return nil })

	case "kick":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TKick, proto.Kick{ID: a.id("id"), Reason: a.str("reason"), Server: a.boolean("server")})
			return nil
		})

	case "ban":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TBan, proto.Ban{ID: a.id("id"), Minutes: int(a.num("minutes")), Reason: a.str("reason")})
			return nil
		})

	case "ban_list":
		return nil, c.withSession(func(s *session) error { s.send(proto.TBanList, struct{}{}); return nil })

	case "unban":
		return nil, c.withSession(func(s *session) error { s.send(proto.TUnban, proto.Unban{IP: a.str("ip")}); return nil })

	case "move":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TMove, proto.Move{ID: a.id("id"), Channel: a.id("channel")})
			return nil
		})

	case "server_mute":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TServerMute, proto.ServerMute{ID: a.id("id"), On: a.boolean("on")})
			return nil
		})

	case "server_edit":
		return nil, c.withSession(func(s *session) error {
			s.send(proto.TServerEdit, proto.ServerEdit{Name: a.str("name"), WelcomeMsg: a.str("welcome")})
			return nil
		})

	case "peer_volume", "peer_mute", "verify":
		err := c.withSession(func(s *session) error {
			p := s.peers[a.id("id")]
			if p == nil {
				return errors.New("user is not online")
			}
			switch cmd {
			case "peer_volume":
				v := a.num("volume")
				if v < 0 {
					v = 0
				} else if v > 4 {
					v = 4
				}
				p.volume = v
				c.cfg.PeerVolumes[p.FP] = v
			case "peer_mute":
				p.localMute = a.boolean("on")
				if p.localMute {
					c.cfg.PeerMuted[p.FP] = true
				} else {
					delete(c.cfg.PeerMuted, p.FP)
				}
			case "verify":
				p.verified = a.boolean("on")
				rec := c.cfg.KnownPeers[p.FP]
				rec.Verified, rec.LastNick = p.verified, p.Info.Nick
				c.cfg.KnownPeers[p.FP] = rec
				if p.verified { // accept this key for the nickname from now on
					c.cfg.NickBindings[s.addr+"|"+strings.ToLower(p.Info.Nick)] = p.FP
					p.keyChanged = false
				}
			}
			c.saveLocked()
			return nil
		})
		c.emitState()
		return nil, err

	case "settings":
		c.mu.Lock()
		old := c.cfg.Audio
		na := old
		b, _ := json.Marshal(old)
		var merged map[string]json.RawMessage
		json.Unmarshal(b, &merged)
		for k, v := range a {
			if _, ok := merged[k]; ok {
				merged[k] = v
			}
		}
		b, _ = json.Marshal(merged)
		json.Unmarshal(b, &na)
		na.InputGain = clampF(na.InputGain, 0.1, 8)
		na.OutputVolume = clampF(na.OutputVolume, 0, 4)
		na.SoundVolume = clampF(na.SoundVolume, 0, 2)
		na.VAThreshold = clampF(na.VAThreshold, -90, 0)
		if na.Bitrate < 12 || na.Bitrate > 128 {
			na.Bitrate = 48
		}
		if na.PTTReleaseMs < 0 || na.PTTReleaseMs > 2000 {
			na.PTTReleaseMs = 150
		}
		switch na.Mode {
		case "ptt", "va", "continuous":
		default:
			na.Mode = "va"
		}
		c.cfg.Audio = na
		c.saveLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "capture_key": // wait for a key press, for binding PTT / whisper keys
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		vk, err := hotkey.Capture(ctx)
		if err != nil {
			return nil, errors.New("no key pressed")
		}
		if a.str("for") == "ptt" {
			c.mu.Lock()
			c.cfg.Audio.PTTKey = vk
			c.saveLocked()
			c.mu.Unlock()
			c.emitState()
		}
		return map[string]any{"key": vk, "name": hotkey.Name(vk)}, nil

	case "clear_ptt_key":
		c.mu.Lock()
		c.cfg.Audio.PTTKey = 0
		c.saveLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "whisper_lists":
		var lists []WhisperList
		if err := json.Unmarshal(a["lists"], &lists); err != nil {
			return nil, err
		}
		if len(lists) > 20 {
			return nil, errors.New("at most 20 whisper lists")
		}
		for i := range lists {
			lists[i].Name = strings.TrimSpace(lists[i].Name)
			if lists[i].Name == "" {
				lists[i].Name = fmt.Sprintf("Whisper %d", i+1)
			}
		}
		c.mu.Lock()
		c.cfg.Whisper = lists
		c.saveLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "quick_whisper":
		var ids []uint32
		json.Unmarshal(a["ids"], &ids)
		c.mu.Lock()
		c.quickWhisper = ids
		c.mu.Unlock()
		return nil, nil

	case "file_download":
		return nil, c.DownloadFile(a.str("id"))

	case "file_cancel":
		return nil, c.CancelFile(a.str("id"))

	case "file_reveal":
		p, err := c.DownloadedPath(a.str("id"))
		if err != nil {
			return nil, err
		}
		return nil, platform.Reveal(p)

	case "open_downloads":
		c.mu.Lock()
		dir := c.downloadDirLocked()
		c.mu.Unlock()
		os.MkdirAll(dir, 0700)
		return nil, platform.OpenPath(dir)

	case "download_dir":
		dir := strings.TrimSpace(a.str("dir"))
		if dir != "" && !filepath.IsAbs(dir) {
			return nil, errors.New("use an absolute folder path")
		}
		c.mu.Lock()
		c.cfg.DownloadDir = dir
		c.saveLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "open_url":
		u := a.str("url")
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return nil, errors.New("only web links can be opened")
		}
		return nil, platform.OpenURL(u)

	case "avatar_set":
		var data []byte
		if err := json.Unmarshal(a["data"], &data); err != nil {
			return nil, err
		}
		return nil, c.SetAvatar(data)

	case "avatar_clear":
		return nil, c.SetAvatar(nil)

	case "bookmark_save":
		var b Bookmark
		json.Unmarshal(raw, &b)
		b.Host = strings.TrimSpace(b.Host)
		if b.Host == "" {
			return nil, errors.New("host required")
		}
		if b.Name == "" {
			b.Name = b.Host
		}
		if b.Port == 0 {
			b.Port = proto.DefaultPort
		}
		c.mu.Lock()
		replaced := false
		for i, o := range c.cfg.Bookmarks {
			if o.Name == b.Name {
				c.cfg.Bookmarks[i] = b
				replaced = true
			}
		}
		if !replaced {
			c.cfg.Bookmarks = append(c.cfg.Bookmarks, b)
		}
		c.saveLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "bookmark_delete":
		name := a.str("name")
		c.mu.Lock()
		kept := c.cfg.Bookmarks[:0]
		for _, o := range c.cfg.Bookmarks {
			if o.Name != name {
				kept = append(kept, o)
			}
		}
		c.cfg.Bookmarks = kept
		c.saveLocked()
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "new_identity":
		c.mu.Lock()
		if c.sess != nil {
			c.mu.Unlock()
			return nil, errors.New("disconnect first")
		}
		c.mu.Unlock()
		id, err := c.store.NewIdentity()
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.id = id
		c.mu.Unlock()
		c.emitState()
		return nil, nil

	case "quit":
		return nil, ErrQuit
	}
	return nil, errors.New("unknown command: " + cmd)
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
