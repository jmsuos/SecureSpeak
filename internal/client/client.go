// Package client is the platform-independent SecureSpeak client core:
// connection, end-to-end key management, chat, whisper, file transfer and
// the encrypted voice path. Audio capture, Opus coding, noise suppression
// and playback live in the UI layer, which exchanges encoded frames with
// this core over a local WebSocket.
package client

import (
	"bufio"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"securespeak/internal/e2e"
	"securespeak/internal/hotkey"
	"securespeak/internal/proto"
)

// Emitter receives events for the UI.
type Emitter interface {
	Emit(t string, v any)
}

// UntrustedError is returned when the server certificate is unknown or changed.
type UntrustedError struct {
	Addr  string
	FP    string
	Known string
}

func (e *UntrustedError) Error() string {
	if e.Known != "" {
		return "server certificate CHANGED for " + e.Addr
	}
	return "unknown server certificate for " + e.Addr
}

type Peer struct {
	Info        proto.ClientInfo
	FP          string
	Valid       bool // ephemeral key signature verified
	pairwise    []byte
	keys        []*e2e.SenderKey // most recent last
	wins        map[byte]*proto.ReplayWindow
	vwins       map[byte]*proto.ReplayWindow // video uses its own replay window
	lastVoice   time.Time
	lastWhisper time.Time
	volume      float64
	localMute   bool
	verified    bool
	keyChanged  bool
	avatar      []byte
	avatarHash  string
}

func (p *Peer) key(id byte) *e2e.SenderKey {
	for i := len(p.keys) - 1; i >= 0; i-- {
		if p.keys[i].ID == id {
			return p.keys[i]
		}
	}
	return nil
}

func (p *Peer) addKey(k *e2e.SenderKey) {
	kept := p.keys[:0]
	for _, o := range p.keys {
		if o.ID != k.ID {
			kept = append(kept, o)
		}
	}
	p.keys = append(kept, k)
	if len(p.keys) > 6 { // channel key + whisper keys during rotations
		p.keys = p.keys[len(p.keys)-6:]
	}
	if p.wins == nil {
		p.wins = map[byte]*proto.ReplayWindow{}
	}
	p.wins[k.ID] = &proto.ReplayWindow{}
	if p.vwins == nil {
		p.vwins = map[byte]*proto.ReplayWindow{}
	}
	p.vwins[k.ID] = &proto.ReplayWindow{}
}

type outFrame struct {
	kind byte
	data []byte
}

type session struct {
	addr         string
	host         string
	certFP       string
	conn         *tls.Conn
	out          chan outFrame
	done         chan struct{}
	closeOnce    sync.Once
	myID         uint32
	keys         *e2e.Session
	serverName   string
	welcomeMsg   string
	maxClients   int
	allowWhisper bool
	hasPw        bool
	channels     map[uint32]proto.ChannelInfo
	roles        []proto.RoleInfo
	peers        map[uint32]*Peer
	me           proto.ClientInfo
	senderKey    *e2e.SenderKey
	whisperKey   *e2e.SenderKey
	whisperSet   string
	nextKeyID    byte
	voiceCtr     uint64
	rotateT      *time.Timer
	pingSent     time.Time
	pingMs       int64

	// file transfer state
	offers       map[string]*outFile
	sends        map[string]*sendXfer
	recvs        map[string]*recvXfer
	remoteOffers map[string]*remoteOffer

	udp       *net.UDPConn
	udpAEAD   cipher.AEAD
	udpOK     atomic.Bool
	udpCtr    atomic.Uint64
	udpWin    proto.ReplayWindow
	udpPongAt atomic.Int64
}

func (s *session) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.conn.Close()
		if s.udp != nil {
			s.udp.Close()
		}
	})
}

func (s *session) sendFrame(kind byte, data []byte) {
	select {
	case s.out <- outFrame{kind, data}:
	case <-s.done:
	default: // backlog full: drop voice; control messages wait briefly
		if kind == proto.FrameJSON {
			go func() {
				select {
				case s.out <- outFrame{kind, data}:
				case <-s.done:
				case <-time.After(5 * time.Second):
				}
			}()
		}
	}
}

func (s *session) send(t string, v any) {
	if b, err := proto.Encode(t, v); err == nil {
		s.sendFrame(proto.FrameJSON, b)
	}
}

// sendBlocking queues a control message in order, waiting for space.
func (s *session) sendBlocking(t string, v any) bool {
	b, err := proto.Encode(t, v)
	if err != nil {
		return false
	}
	select {
	case s.out <- outFrame{proto.FrameJSON, b}:
		return true
	case <-s.done:
		return false
	case <-time.After(30 * time.Second):
		return false
	}
}

func (s *session) writer() {
	w := bufio.NewWriterSize(s.conn, 64*1024)
	for {
		select {
		case f := <-s.out:
			s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if proto.WriteFrame(w, f.kind, f.data) != nil {
				s.close()
				return
			}
			if len(s.out) == 0 && w.Flush() != nil {
				s.close()
				return
			}
		case <-s.done:
			return
		}
	}
}

// Client is the client core.
type Client struct {
	store *Store
	em    Emitter

	voiceOut atomic.Pointer[func([]byte)]

	mu       sync.Mutex
	cfg      *Config
	id       *e2e.Identity
	sess     *session
	connStep string

	muted, deaf, away bool
	awayMsg           string
	sharing           bool

	avatar     []byte
	avatarHash string

	txAt         time.Time
	txEnded      bool
	txWhisper    bool
	quickWhisper []uint32
	lastTalk     string

	closed    chan struct{}
	closeOnce sync.Once
}

func New(store *Store, em Emitter) (*Client, error) {
	id, err := store.LoadIdentity()
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}
	c := &Client{
		store:  store,
		em:     em,
		cfg:    store.LoadConfig(),
		id:     id,
		closed: make(chan struct{}),
	}
	os.RemoveAll(store.CacheDir()) // previews from earlier sessions
	if av := store.LoadAvatar(); validImage(av) {
		c.avatar, c.avatarHash = av, imageHash(av)
	}
	go c.tickLoop()
	go c.keysLoop()
	return c, nil
}

// SetVoiceSink registers where decrypted voice frames go (the UI audio engine).
// Frame format: [1][sender u32][flags][codec][seq u32][payload].
func (c *Client) SetVoiceSink(f func([]byte)) { c.voiceOut.Store(&f) }

func (c *Client) Close() {
	c.closeOnce.Do(func() { close(c.closed) })
	c.Disconnect()
}

func (c *Client) Fingerprint() string { return e2e.Fingerprint(c.id.Pub) }

func (c *Client) saveLocked() {
	b, err := json.MarshalIndent(c.cfg, "", "  ")
	if err != nil {
		return
	}
	go func() {
		if err := c.store.WriteConfig(b); err != nil {
			log.Printf("save config: %v", err)
		}
	}()
}

func (c *Client) system(text string) {
	c.em.Emit("chat", map[string]any{"kind": "system", "text": text, "ts": time.Now().UnixMilli()})
}

func (c *Client) playSound(name string) {
	c.mu.Lock()
	on := c.cfg.Audio.Sounds
	c.mu.Unlock()
	if on {
		c.em.Emit("sound", name)
	}
}

// ---- connect ------------------------------------------------------------------

func (c *Client) Connect(host string, port int, nick, serverPw, trustFP string) error {
	host = strings.TrimSpace(host)
	nick = strings.TrimSpace(nick)
	if host == "" {
		return errors.New("enter a server address")
	}
	if nick == "" {
		return errors.New("enter a nickname")
	}
	if port <= 0 || port > 65535 {
		port = proto.DefaultPort
	}
	c.mu.Lock()
	if c.sess != nil || c.connStep != "" {
		c.mu.Unlock()
		return errors.New("already connected")
	}
	c.connStep = "connecting"
	c.mu.Unlock()
	c.emitState()
	err := c.connect(host, port, nick, serverPw, trustFP)
	c.mu.Lock()
	c.connStep = ""
	c.mu.Unlock()
	c.emitState()
	return err
}

func (c *Client) connect(host string, port int, nick, serverPw, trustFP string) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	raw, err := (&net.Dialer{Timeout: 10 * time.Second}).Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", addr, err)
	}
	// Self-signed server certificate, pinned on first use (like SSH).
	tc := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
	tc.SetDeadline(time.Now().Add(15 * time.Second))
	if err := tc.Handshake(); err != nil {
		tc.Close()
		return fmt.Errorf("TLS handshake failed: %w", err)
	}
	certs := tc.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		tc.Close()
		return errors.New("server sent no certificate")
	}
	fp := proto.CertFingerprint(certs[0].Raw)
	c.mu.Lock()
	known := c.cfg.KnownServers[addr]
	if known != fp {
		if trustFP != fp {
			c.mu.Unlock()
			tc.Close()
			return &UntrustedError{Addr: addr, FP: fp, Known: known}
		}
		c.cfg.KnownServers[addr] = fp
		c.saveLocked()
	}
	id := c.id
	c.mu.Unlock()

	keys, err := e2e.NewSession(id)
	if err != nil {
		tc.Close()
		return err
	}
	// Prove we own our identity key, bound to this TLS connection.
	cs := tc.ConnectionState()
	ekm, err := cs.ExportKeyingMaterial(proto.AuthEKMLabel, nil, 32)
	if err != nil {
		tc.Close()
		return err
	}
	hello, _ := proto.Encode(proto.THello, proto.Hello{
		Version: proto.Version, Nick: nick, ServerPassword: serverPw,
		IDPub: id.Pub, EphPub: keys.EphPub, Sig: keys.Sig, AuthSig: e2e.AuthSign(id, ekm),
	})
	if err := proto.WriteFrame(tc, proto.FrameJSON, hello); err != nil {
		tc.Close()
		return err
	}
	br := bufio.NewReaderSize(tc, 128*1024)
	kind, payload, err := proto.ReadFrame(br)
	if err != nil {
		tc.Close()
		return fmt.Errorf("server closed the connection: %w", err)
	}
	var m proto.Message
	if kind != proto.FrameJSON || json.Unmarshal(payload, &m) != nil {
		tc.Close()
		return errors.New("protocol error")
	}
	if m.T == proto.TError {
		var e proto.Error
		json.Unmarshal(m.D, &e)
		tc.Close()
		return errors.New(e.Msg)
	}
	var w proto.Welcome
	if m.T != proto.TWelcome || json.Unmarshal(m.D, &w) != nil {
		tc.Close()
		return errors.New("protocol error: expected welcome")
	}
	tc.SetDeadline(time.Time{})

	udpAEAD, err := e2e.NewGCM(w.UDPKey)
	if err != nil {
		tc.Close()
		return err
	}
	var kid [1]byte
	rand.Read(kid[:])
	s := &session{
		addr: addr, host: host, certFP: fp, conn: tc,
		out: make(chan outFrame, 1024), done: make(chan struct{}),
		myID: w.YourID, keys: keys, serverName: w.ServerName, welcomeMsg: w.WelcomeMsg,
		maxClients: w.MaxClients, allowWhisper: w.Whisper, roles: w.Roles, hasPw: w.HasPw,
		channels: map[uint32]proto.ChannelInfo{}, peers: map[uint32]*Peer{},
		offers: map[string]*outFile{}, sends: map[string]*sendXfer{},
		recvs: map[string]*recvXfer{}, remoteOffers: map[string]*remoteOffer{},
		udpAEAD: udpAEAD, nextKeyID: kid[0],
	}
	s.senderKey = e2e.NewSenderKey(s.nextKeyID)
	s.nextKeyID++
	for _, ch := range w.Channels {
		s.channels[ch.ID] = ch
	}

	c.mu.Lock()
	c.sess = s
	for _, ci := range w.Clients {
		if ci.ID == s.myID {
			s.me = ci
			continue
		}
		c.addPeerLocked(s, ci)
	}
	c.cfg.Nickname = nick
	c.cfg.LastHost, c.cfg.LastPort = host, port
	c.saveLocked()
	c.sharing = false // a new connection never starts out sharing
	if c.muted || c.deaf || c.away {
		s.send(proto.TStatus, proto.Status{Muted: c.muted, Deaf: c.deaf, Away: c.away, AwayMsg: c.awayMsg})
	}
	for _, p := range s.peers {
		if p.Info.Channel == s.me.Channel {
			c.sendKeyLocked(s, p, s.senderKey)
		}
	}
	if c.avatar != nil {
		c.sendAvatarLocked(s, nil)
	}
	c.mu.Unlock()

	go s.writer()
	go c.readLoop(s, br)
	go c.udpLoop(s, w.UDPPort)
	go c.pingLoop(s)

	c.playSound("connect")
	c.em.Emit("connected", map[string]any{"server": w.ServerName, "welcome": w.WelcomeMsg})
	if w.WelcomeMsg != "" {
		c.system(w.WelcomeMsg)
	}
	return nil
}

func (c *Client) Disconnect() {
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()
	if s != nil {
		c.dropSession(s, "Disconnected")
	}
}

func (c *Client) dropSession(s *session, reason string) {
	c.mu.Lock()
	if c.sess != s {
		c.mu.Unlock()
		return
	}
	c.sess = nil
	if s.rotateT != nil {
		s.rotateT.Stop()
	}
	c.cleanupFilesLocked(s)
	c.mu.Unlock()
	s.close()
	c.playSound("disconnect")
	c.em.Emit("disconnected", map[string]any{"reason": reason})
	c.emitState()
}

// ---- peers & keys --------------------------------------------------------------

func (c *Client) addPeerLocked(s *session, ci proto.ClientInfo) *Peer {
	p := &Peer{Info: ci, FP: e2e.Fingerprint(ci.IDPub), volume: 1}
	if e2e.VerifyPeer(ci.IDPub, ci.EphPub, ci.Sig) {
		if pk, err := s.keys.Pairwise(ci.EphPub); err == nil {
			p.pairwise, p.Valid = pk, true
		}
	}
	if !p.Valid {
		c.system(fmt.Sprintf("⚠ %s presented an invalid key signature. Their messages and voice are ignored.", ci.Nick))
	}
	if v, ok := c.cfg.PeerVolumes[p.FP]; ok {
		p.volume = v
	}
	p.localMute = c.cfg.PeerMuted[p.FP]
	rec := c.cfg.KnownPeers[p.FP]
	p.verified = rec.Verified
	if rec.LastNick != ci.Nick {
		rec.LastNick = ci.Nick
		c.cfg.KnownPeers[p.FP] = rec
		c.saveLocked()
	}
	c.checkBindingLocked(s, p)
	s.peers[ci.ID] = p
	return p
}

// checkBindingLocked warns when a nickname shows up with a different identity
// than last time on this server.
func (c *Client) checkBindingLocked(s *session, p *Peer) {
	key := s.addr + "|" + strings.ToLower(p.Info.Nick)
	old, ok := c.cfg.NickBindings[key]
	switch {
	case !ok:
		c.cfg.NickBindings[key] = p.FP
		c.saveLocked()
	case old != p.FP:
		p.keyChanged = true
		c.system(fmt.Sprintf("⚠ The identity key of \"%s\" is different from last time on this server. This can mean a different person is using the name, or that someone is impersonating them. Verify the fingerprint before trusting them.", p.Info.Nick))
	}
}

func (c *Client) sendKeyLocked(s *session, p *Peer, k *e2e.SenderKey) {
	if !p.Valid || k == nil {
		return
	}
	blob, err := e2e.WrapSenderKey(p.pairwise, k, s.myID, p.Info.ID)
	if err != nil {
		return
	}
	s.send(proto.TKeyDist, proto.KeyDist{To: p.Info.ID, Blob: blob})
}

// scheduleRotateLocked replaces our channel voice key shortly after a member
// left, so they cannot decrypt anything we say from now on.
func (c *Client) scheduleRotateLocked(s *session) {
	if s.rotateT != nil {
		return
	}
	s.rotateT = time.AfterFunc(250*time.Millisecond, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		s.rotateT = nil
		if c.sess == s {
			c.rotateLocked(s)
		}
	})
}

func (c *Client) rotateLocked(s *session) {
	s.senderKey = e2e.NewSenderKey(s.nextKeyID)
	s.nextKeyID++
	for _, p := range s.peers {
		if p.Info.Channel == s.me.Channel {
			c.sendKeyLocked(s, p, s.senderKey)
		}
	}
}

// ---- receive -------------------------------------------------------------------

func (c *Client) readLoop(s *session, br *bufio.Reader) {
	reason := "Connection lost"
	for {
		s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		kind, payload, err := proto.ReadFrame(br)
		if err != nil {
			select {
			case <-s.done:
				reason = "Disconnected"
			default:
			}
			break
		}
		if kind == proto.FrameVoice {
			c.handleVoice(s, payload)
			continue
		}
		if kind == proto.FrameVideo {
			c.handleVideo(s, payload)
			continue
		}
		var m proto.Message
		if json.Unmarshal(payload, &m) != nil {
			continue
		}
		if m.T == proto.TKicked {
			var k proto.Kicked
			json.Unmarshal(m.D, &k)
			reason = fmt.Sprintf("Kicked from server by %s. %s", k.By, k.Reason)
			break
		}
		c.handleMsg(s, m)
	}
	c.dropSession(s, reason)
}

func unmarshal[T any](m proto.Message) (T, bool) {
	var v T
	return v, json.Unmarshal(m.D, &v) == nil
}

func (c *Client) handleMsg(s *session, m proto.Message) {
	var sound string
	stateChanged := true
	c.mu.Lock()
	if c.sess != s {
		c.mu.Unlock()
		return
	}
	switch m.T {
	case proto.TClientJoin:
		ci, ok := unmarshal[proto.ClientInfo](m)
		if !ok {
			break
		}
		p := c.addPeerLocked(s, ci)
		if ci.Channel == s.me.Channel {
			c.sendKeyLocked(s, p, s.senderKey)
			sound = "join"
		}
		c.sendAvatarLocked(s, []uint32{ci.ID})
		c.system(fmt.Sprintf("%s connected", ci.Nick))
	case proto.TClientUpdate:
		ci, ok := unmarshal[proto.ClientInfo](m)
		if !ok {
			break
		}
		if ci.ID == s.myID {
			if ci.Owner != s.me.Owner || fmt.Sprint(ci.Roles) != fmt.Sprint(s.me.Roles) {
				sound = "msg"
			}
			s.me = ci
			break
		}
		if p := s.peers[ci.ID]; p != nil {
			nickChanged := p.Info.Nick != ci.Nick
			ci.IDPub, ci.EphPub, ci.Sig = p.Info.IDPub, p.Info.EphPub, p.Info.Sig
			p.Info = ci
			if nickChanged {
				c.checkBindingLocked(s, p)
			}
		}
	case proto.TClientLeave:
		cl, ok := unmarshal[proto.ClientLeave](m)
		if !ok {
			break
		}
		if p := s.peers[cl.ID]; p != nil {
			if p.Info.Channel == s.me.Channel {
				c.scheduleRotateLocked(s)
				sound = "leave"
			}
			c.system(fmt.Sprintf("%s disconnected", p.Info.Nick))
			c.peerGoneFilesLocked(s, cl.ID)
			delete(s.peers, cl.ID)
		}
	case proto.TClientMove:
		mv, ok := unmarshal[proto.Move](m)
		if !ok {
			break
		}
		if mv.ID == s.myID {
			s.me.Channel = mv.Channel
			if s.rotateT != nil {
				s.rotateT.Stop()
				s.rotateT = nil
			}
			c.rotateLocked(s) // fresh key for the new channel, sent to its members
			if mv.By != 0 {
				by := "an admin"
				if p := s.peers[mv.By]; p != nil {
					by = p.Info.Nick
				}
				c.system(fmt.Sprintf("You were moved to \"%s\" by %s", s.channels[mv.Channel].Name, by))
			}
			sound = "move"
			break
		}
		p := s.peers[mv.ID]
		if p == nil {
			break
		}
		old := p.Info.Channel
		p.Info.Channel = mv.Channel
		if mv.Channel == s.me.Channel {
			c.sendKeyLocked(s, p, s.senderKey)
			sound = "join"
		} else if old == s.me.Channel {
			c.scheduleRotateLocked(s)
			sound = "leave"
		}
	case proto.TChanAdd, proto.TChanUpdate:
		if ch, ok := unmarshal[proto.ChannelInfo](m); ok {
			s.channels[ch.ID] = ch
		}
	case proto.TChanRemove:
		if id, ok := unmarshal[proto.IDOnly](m); ok {
			delete(s.channels, id.ID)
		}
	case proto.TRoles:
		if r, ok := unmarshal[proto.Roles](m); ok {
			s.roles = r.Roles
		}
	case proto.TToken:
		stateChanged = false
		if t, ok := unmarshal[proto.Token](m); ok {
			c.em.Emit("token", t)
		}
	case proto.TTokens:
		stateChanged = false
		if t, ok := unmarshal[proto.TokenList](m); ok {
			c.em.Emit("tokens", t)
		}
	case proto.TMembers:
		stateChanged = false
		if ml, ok := unmarshal[proto.MemberList](m); ok {
			out := make([]map[string]any, 0, len(ml.Members))
			for _, mi := range ml.Members {
				out = append(out, map[string]any{"key": mi.Key, "fp": e2e.Fingerprint(mi.Key), "nick": mi.Nick,
					"roles": nonNil(mi.Roles), "owner": mi.Owner, "lastSeen": mi.LastSeen, "online": mi.Online})
			}
			c.em.Emit("members", out)
		}
	case proto.TServerUpdate:
		if se, ok := unmarshal[proto.ServerEdit](m); ok {
			s.serverName, s.welcomeMsg, s.hasPw = se.Name, se.WelcomeMsg, se.HasPw
		}
	case proto.TKeyframeReq:
		stateChanged = false
		if kr, ok := unmarshal[proto.KeyframeReq](m); ok && c.sharing {
			c.em.Emit("keyframe", map[string]any{"from": kr.From})
		}
	case proto.TKeyDist:
		stateChanged = false
		kd, ok := unmarshal[proto.KeyDist](m)
		p := s.peers[kd.From]
		if !ok || p == nil || !p.Valid {
			break
		}
		if k, err := e2e.UnwrapSenderKey(p.pairwise, kd.Blob, kd.From, s.myID); err == nil {
			hadKey := len(p.keys) > 0
			p.addKey(k)
			stateChanged = !hadKey
		}
	case proto.TChat:
		stateChanged = false
		if ch, ok := unmarshal[proto.Chat](m); ok {
			sound, stateChanged = c.receiveChatLocked(s, ch)
		}
	case proto.TFileCtl:
		stateChanged = false
		if fc, ok := unmarshal[proto.FileCtl](m); ok {
			c.handleFileCtlLocked(s, fc)
		}
	case proto.TFileData:
		stateChanged = false
		if fd, ok := unmarshal[proto.FileData](m); ok {
			c.handleFileDataLocked(s, fd)
		}
	case proto.TPong:
		stateChanged = false
		if !s.pingSent.IsZero() {
			s.pingMs = time.Since(s.pingSent).Milliseconds()
			c.em.Emit("stats", c.statsLocked(s))
		}
	case proto.TError:
		stateChanged = false
		if e, ok := unmarshal[proto.Error](m); ok {
			c.em.Emit("error", e)
			sound = "error"
		}
	case proto.TNotice:
		stateChanged = false
		if n, ok := unmarshal[proto.Notice](m); ok {
			c.system(n.Msg)
		}
	case proto.TBanList:
		stateChanged = false
		if bl, ok := unmarshal[proto.BanList](m); ok {
			c.em.Emit("bans", bl)
		}
	default:
		stateChanged = false
	}
	c.mu.Unlock()
	if sound != "" {
		c.playSound(sound)
	}
	if stateChanged {
		c.emitState()
	}
}

type chatBody struct {
	Text   string      `json:"text,omitempty"`
	File   *fileOffer  `json:"file,omitempty"`
	Avatar *avatarBody `json:"avatar,omitempty"`
}

func (c *Client) receiveChatLocked(s *session, ch proto.Chat) (sound string, stateChanged bool) {
	p := s.peers[ch.From]
	if p == nil || !p.Valid {
		return "", false
	}
	wrap := ch.Wraps[strconv.FormatUint(uint64(s.myID), 10)]
	pt, err := e2e.DecryptMessage(ch.Nonce, ch.CT, e2e.ChatAAD(ch.Kind, ch.Target, ch.From), wrap, p.pairwise, s.myID)
	if err != nil {
		c.system(fmt.Sprintf("⚠ Could not decrypt a message from %s", p.Info.Nick))
		return "", false
	}
	var b chatBody
	if json.Unmarshal(pt, &b) != nil {
		return "", false
	}
	if ch.Kind == "avatar" {
		if b.Avatar == nil {
			return "", false
		}
		if len(b.Avatar.Data) == 0 {
			p.avatar, p.avatarHash = nil, ""
		} else if validImage(b.Avatar.Data) {
			p.avatar, p.avatarHash = b.Avatar.Data, imageHash(b.Avatar.Data)
		}
		return "", true
	}
	if p.localMute && ch.Kind != "server" {
		return "", false
	}
	text := b.Text
	if r := []rune(text); len(r) > proto.MaxChatLen {
		text = string(r[:proto.MaxChatLen])
	}
	ev := map[string]any{
		"kind": ch.Kind, "from": ch.From, "fromNick": p.Info.Nick, "target": ch.Target,
		"text": text, "ts": ch.TS, "verified": p.verified, "keyChanged": p.keyChanged,
	}
	if b.File != nil && (ch.Kind == "channel" || ch.Kind == "private") {
		if f := c.registerOfferLocked(s, p, b.File); f != nil {
			ev["file"] = f
		}
	}
	if ch.Kind == "poke" {
		c.em.Emit("poke", ev)
		return "poke", false
	}
	c.em.Emit("chat", ev)
	return "msg", false
}

// ---- send helpers used by commands ---------------------------------------------

func (c *Client) withSession(f func(s *session) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess == nil {
		return errors.New("not connected")
	}
	return f(c.sess)
}

// sendBodyLocked encrypts a chat body for the audience of kind/target.
// It returns the effective target and the number of recipients.
func (c *Client) sendBodyLocked(s *session, kind string, target uint32, body chatBody) (uint32, map[uint32]bool, error) {
	recips := map[uint32][]byte{}
	switch kind {
	case "channel":
		target = s.me.Channel
		for id, p := range s.peers {
			if p.Valid && p.Info.Channel == target {
				recips[id] = p.pairwise
			}
		}
	case "private", "poke":
		p := s.peers[target]
		if p == nil {
			return 0, nil, errors.New("user is not online")
		}
		if !p.Valid {
			return 0, nil, errors.New("this user's key is invalid; cannot encrypt to them")
		}
		recips[target] = p.pairwise
	case "server", "avatar":
		target = 0
		for id, p := range s.peers {
			if p.Valid {
				recips[id] = p.pairwise
			}
		}
	default:
		return 0, nil, errors.New("unknown chat kind")
	}
	return target, keysOf(recips), c.sendEncryptedLocked(s, kind, target, body, recips)
}

func keysOf(m map[uint32][]byte) map[uint32]bool {
	out := make(map[uint32]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func (c *Client) sendEncryptedLocked(s *session, kind string, target uint32, body chatBody, recips map[uint32][]byte) error {
	if len(recips) == 0 {
		return nil
	}
	raw, _ := json.Marshal(body)
	nonce, ct, wraps, err := e2e.EncryptMessage(raw, e2e.ChatAAD(kind, target, s.myID), recips)
	if err != nil {
		return err
	}
	s.send(proto.TChat, proto.Chat{Kind: kind, Target: target, Nonce: nonce, CT: ct, Wraps: wraps})
	return nil
}

func (c *Client) SendChat(kind string, target uint32, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if r := []rune(text); len(r) > proto.MaxChatLen {
		return fmt.Errorf("message too long (max %d characters)", proto.MaxChatLen)
	}
	if kind == "avatar" {
		return errors.New("unknown chat kind")
	}
	return c.withSession(func(s *session) error {
		tgt, recips, err := c.sendBodyLocked(s, kind, target, chatBody{Text: text})
		if err != nil {
			return err
		}
		if kind == "poke" {
			c.system(fmt.Sprintf("You poked %s: %s", s.peers[target].Info.Nick, text))
			return nil
		}
		c.em.Emit("chat", map[string]any{
			"kind": kind, "from": s.myID, "fromNick": s.me.Nick, "target": tgt,
			"text": text, "ts": time.Now().UnixMilli(), "self": true, "recipients": len(recips),
		})
		return nil
	})
}

func (c *Client) SetStatus(muted, deaf, away *bool, awayMsg *string) {
	c.mu.Lock()
	if muted != nil {
		c.muted = *muted
	}
	if deaf != nil {
		c.deaf = *deaf
		if c.deaf {
			c.muted = true // like TeamSpeak: deafening also mutes the mic
		}
	}
	if away != nil {
		c.away = *away
	}
	if awayMsg != nil {
		c.awayMsg = *awayMsg
	}
	c.sendStatusLocked()
	c.mu.Unlock()
	c.emitState()
}

func (c *Client) sendStatusLocked() {
	if c.sess != nil {
		c.sess.send(proto.TStatus, proto.Status{Muted: c.muted, Deaf: c.deaf, Away: c.away, AwayMsg: c.awayMsg, Sharing: c.sharing})
	}
}

// SetSharing announces that our screen share started or stopped.
func (c *Client) SetSharing(on bool) error {
	c.mu.Lock()
	if c.sess == nil {
		c.mu.Unlock()
		return errors.New("not connected")
	}
	c.sharing = on
	c.sendStatusLocked()
	c.mu.Unlock()
	c.emitState()
	return nil
}

// ---- UDP -------------------------------------------------------------------------

// testDisableUDP simulates a network that blocks UDP.
var testDisableUDP atomic.Bool

func (c *Client) udpLoop(s *session, port int) {
	if testDisableUDP.Load() {
		return
	}
	raddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(s.host, strconv.Itoa(port)))
	if err != nil {
		return
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return
	}
	c.mu.Lock()
	if c.sess != s {
		c.mu.Unlock()
		conn.Close()
		return
	}
	s.udp = conn
	c.mu.Unlock()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				select {
				case <-s.done:
					return
				default:
				}
				if errors.Is(err, net.ErrClosed) {
					return
				}
				time.Sleep(100 * time.Millisecond) // e.g. ICMP port unreachable
				continue
			}
			typ, id, ok := proto.UDPHeader(buf[:n])
			if !ok || id != s.myID {
				continue
			}
			ctr, payload, err := proto.OpenUDP(s.udpAEAD, buf[:n], proto.DirServerToClient)
			if err != nil || !s.udpWin.Check(ctr) {
				continue
			}
			switch typ {
			case proto.UDPHelloAck, proto.UDPPong:
				s.udpPongAt.Store(time.Now().UnixNano())
				if !s.udpOK.Swap(true) {
					c.emitStats(s)
				}
			case proto.UDPVoice:
				s.udpPongAt.Store(time.Now().UnixNano())
				c.handleVoice(s, payload)
			}
		}
	}()

	t := time.NewTicker(time.Second)
	defer t.Stop()
	ticks := 0
	for {
		if s.udpOK.Load() {
			if time.Since(time.Unix(0, s.udpPongAt.Load())) > 12*time.Second {
				s.udpOK.Store(false) // fall back to TCP until UDP answers again
				c.emitStats(s)
			} else if ticks%3 == 0 {
				conn.Write(proto.SealUDP(s.udpAEAD, proto.UDPPing, s.myID, proto.DirClientToServer, s.udpCtr.Add(1), nil))
			}
		} else {
			conn.Write(proto.SealUDP(s.udpAEAD, proto.UDPHello, s.myID, proto.DirClientToServer, s.udpCtr.Add(1), nil))
		}
		ticks++
		select {
		case <-s.done:
			return
		case <-t.C:
		}
	}
}

func (c *Client) pingLoop(s *session) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		c.mu.Lock()
		s.pingSent = time.Now()
		c.mu.Unlock()
		s.send(proto.TPing, proto.Ping{TS: time.Now().UnixMilli()})
		select {
		case <-s.done:
			return
		case <-t.C:
		}
	}
}

func (c *Client) statsLocked(s *session) map[string]any {
	transport := "TCP (UDP blocked?)"
	if s.udpOK.Load() {
		transport = "UDP"
	}
	return map[string]any{"ping": s.pingMs, "transport": transport}
}

func (c *Client) emitStats(s *session) {
	c.mu.Lock()
	st := c.statsLocked(s)
	c.mu.Unlock()
	c.em.Emit("stats", st)
}

// ---- voice -----------------------------------------------------------------------

const (
	flagEnd     = 1 << 0
	flagWhisper = 1 << 1

	ModeQuickWhisper = 255
)

// HandleVoiceFrame takes one encoded frame from the UI audio engine and sends
// it, encrypted and signed. mode 0 = channel, 1..n = whisper list n-1,
// 255 = quick whisper to the users set with SetQuickWhisper.
func (c *Client) HandleVoiceFrame(mode, codec, flags byte, payload []byte) {
	if len(payload) == 0 || len(payload) > 1500 {
		return
	}
	c.mu.Lock()
	s := c.sess
	if s == nil || c.muted || s.me.ServerMuted {
		c.mu.Unlock()
		return
	}
	key := s.senderKey
	var targets []uint32
	whisper := mode != 0
	if whisper {
		if !s.allowWhisper {
			c.mu.Unlock()
			return
		}
		targets = c.resolveWhisperLocked(s, mode)
		if len(targets) == 0 {
			c.mu.Unlock()
			return
		}
		key = c.whisperKeyLocked(s, targets)
	}
	s.voiceCtr++
	ctr := s.voiceCtr
	end := flags&flagEnd != 0
	c.txAt, c.txEnded, c.txWhisper = time.Now(), end, whisper
	myID, id := s.myID, c.id
	c.mu.Unlock()

	fl := flags & flagEnd
	if whisper {
		fl |= flagWhisper // bound inside the encryption, so the server cannot flip it
	}
	plain := make([]byte, 0, 2+len(payload))
	plain = append(plain, codec, fl)
	plain = append(plain, payload...)
	signed := e2e.SignVoice(id, myID, key.SealVoice(myID, ctr, plain))

	var pkt []byte
	if whisper {
		pkt = make([]byte, 2+4*len(targets), 2+4*len(targets)+len(signed))
		pkt[0], pkt[1] = proto.VoiceWhisper, byte(len(targets))
		for i, t := range targets {
			binary.BigEndian.PutUint32(pkt[2+4*i:], t)
		}
	} else {
		pkt = []byte{proto.VoiceChannel}
	}
	pkt = append(pkt, signed...)
	if s.udpOK.Load() && s.udp != nil {
		s.udp.Write(proto.SealUDP(s.udpAEAD, proto.UDPVoice, myID, proto.DirClientToServer, s.udpCtr.Add(1), pkt))
	} else {
		s.sendFrame(proto.FrameVoice, pkt)
	}
}

// resolveWhisperLocked returns the sorted ids of online, valid users matching
// a whisper list (or the quick-whisper targets).
func (c *Client) resolveWhisperLocked(s *session, mode byte) []uint32 {
	var ids []uint32
	if mode == ModeQuickWhisper {
		for _, id := range c.quickWhisper {
			if p := s.peers[id]; p != nil && p.Valid {
				ids = append(ids, id)
			}
		}
	} else {
		i := int(mode) - 1
		if i < 0 || i >= len(c.cfg.Whisper) {
			return nil
		}
		wl := c.cfg.Whisper[i]
		users := map[string]bool{}
		for _, fp := range wl.Users {
			users[fp] = true
		}
		chans := map[string]bool{}
		for _, n := range wl.Channels {
			chans[strings.ToLower(n)] = true
		}
		for id, p := range s.peers {
			if !p.Valid {
				continue
			}
			if users[p.FP] || chans[strings.ToLower(s.channels[p.Info.Channel].Name)] {
				ids = append(ids, id)
			}
		}
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	if len(ids) > proto.MaxWhisperTargets {
		ids = ids[:proto.MaxWhisperTargets]
	}
	return ids
}

// whisperKeyLocked returns a sender key known only to exactly these targets;
// a new key is generated whenever the audience changes.
func (c *Client) whisperKeyLocked(s *session, targets []uint32) *e2e.SenderKey {
	set := fmt.Sprint(targets)
	if s.whisperKey == nil || s.whisperSet != set {
		s.whisperKey = e2e.NewSenderKey(s.nextKeyID)
		s.nextKeyID++
		s.whisperSet = set
		for _, id := range targets {
			if p := s.peers[id]; p != nil {
				c.sendKeyLocked(s, p, s.whisperKey)
			}
		}
	}
	return s.whisperKey
}

func (c *Client) handleVoice(s *session, payload []byte) {
	if len(payload) < 5 {
		return
	}
	sender := binary.BigEndian.Uint32(payload)
	mode := payload[4]
	signed := payload[5:]
	whisper := mode == proto.VoiceWhisper
	c.mu.Lock()
	if c.sess != s || c.deaf {
		c.mu.Unlock()
		return
	}
	p := s.peers[sender]
	if p == nil || !p.Valid || p.localMute || (!whisper && p.Info.Channel != s.me.Channel) {
		c.mu.Unlock()
		return
	}
	idPub := p.Info.IDPub
	c.mu.Unlock()

	// Signature check outside the lock (it is the expensive part).
	inner, ok := e2e.VerifyVoice(idPub, sender, signed)
	if !ok {
		return
	}
	keyID, ctr, ok := e2e.ParseVoice(inner)
	if !ok {
		return
	}
	c.mu.Lock()
	if c.sess != s || s.peers[sender] != p {
		c.mu.Unlock()
		return
	}
	k := p.key(keyID)
	if k == nil {
		c.mu.Unlock()
		return // key not received yet
	}
	pt, err := k.OpenVoice(sender, inner)
	if err != nil || len(pt) < 3 || !p.wins[keyID].Check(ctr) {
		c.mu.Unlock()
		return
	}
	codec, fl := pt[0], pt[1]
	if (fl&flagWhisper != 0) != whisper {
		c.mu.Unlock()
		return // routing mode was tampered with
	}
	if fl&flagEnd != 0 {
		p.lastVoice, p.lastWhisper = time.Time{}, time.Time{}
	} else if whisper {
		p.lastWhisper = time.Now()
	} else {
		p.lastVoice = time.Now()
	}
	c.mu.Unlock()

	if f := c.voiceOut.Load(); f != nil {
		frame := make([]byte, 11+len(pt)-2)
		frame[0] = 1
		binary.BigEndian.PutUint32(frame[1:], sender)
		frame[5] = fl
		frame[6] = codec
		binary.BigEndian.PutUint32(frame[7:], uint32(ctr))
		copy(frame[11:], pt[2:])
		(*f)(frame)
	}
}

// ---- periodic UI updates ------------------------------------------------------------

func (c *Client) tickLoop() {
	t := time.NewTicker(80 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
		}
		talking, whisper := []uint32{}, []uint32{}
		selfWhisper := false
		c.mu.Lock()
		if s := c.sess; s != nil {
			now := time.Now()
			for id, p := range s.peers {
				if !p.lastVoice.IsZero() && now.Sub(p.lastVoice) < 300*time.Millisecond {
					talking = append(talking, id)
				}
				if !p.lastWhisper.IsZero() && now.Sub(p.lastWhisper) < 300*time.Millisecond {
					whisper = append(whisper, id)
				}
			}
			if !c.txEnded && now.Sub(c.txAt) < 300*time.Millisecond {
				talking = append(talking, s.myID)
				selfWhisper = c.txWhisper
			}
		}
		c.mu.Unlock()
		sort.Slice(talking, func(i, j int) bool { return talking[i] < talking[j] })
		sort.Slice(whisper, func(i, j int) bool { return whisper[i] < whisper[j] })
		key := fmt.Sprint(talking, whisper, selfWhisper)
		if key != c.lastTalk {
			c.lastTalk = key
			c.em.Emit("talking", map[string]any{"ids": talking, "whisper": whisper, "selfWhisper": selfWhisper})
		}
	}
}

// keysLoop polls the global push-to-talk and whisper hotkeys and tells the UI
// audio engine when they change.
func (c *Client) keysLoop() {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	lastPTT, lastWhisper := false, -1
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
		}
		c.mu.Lock()
		ptt := c.cfg.Audio.PTTKey
		var wkeys []int
		for _, w := range c.cfg.Whisper {
			wkeys = append(wkeys, w.Key)
		}
		c.mu.Unlock()
		down := ptt > 0 && hotkey.Pressed(ptt)
		wh := -1
		for i, k := range wkeys {
			if k > 0 && hotkey.Pressed(k) {
				wh = i
				break
			}
		}
		if down != lastPTT || wh != lastWhisper {
			lastPTT, lastWhisper = down, wh
			c.em.Emit("keys", map[string]any{"ptt": down, "whisper": wh})
		}
	}
}

func nonNil(v []uint32) []uint32 {
	if v == nil {
		return []uint32{}
	}
	return v
}

// ---- screen share ------------------------------------------------------------------

// HandleVideoFrame encrypts and signs one encoded screen share frame from the
// UI and sends it to the server, which forwards it to our viewers.
func (c *Client) HandleVideoFrame(flags, codec byte, payload []byte) {
	if len(payload) == 0 || len(payload) > proto.MaxFrameSize-256 {
		return
	}
	c.mu.Lock()
	s := c.sess
	if s == nil || !c.sharing {
		c.mu.Unlock()
		return
	}
	key := s.senderKey
	s.voiceCtr++ // shared counter: nonces stay unique per key across voice and video
	ctr := s.voiceCtr
	myID, id := s.myID, c.id
	c.mu.Unlock()
	plain := make([]byte, 0, 2+len(payload))
	plain = append(plain, codec, flags)
	plain = append(plain, payload...)
	s.sendFrame(proto.FrameVideo, e2e.SignVoice(id, myID, key.SealVoice(myID, ctr, plain)))
}

func (c *Client) handleVideo(s *session, payload []byte) {
	if len(payload) < 4 {
		return
	}
	sender := binary.BigEndian.Uint32(payload)
	c.mu.Lock()
	p := s.peers[sender]
	if c.sess != s || p == nil || !p.Valid || p.Info.Channel != s.me.Channel {
		c.mu.Unlock()
		return
	}
	idPub := p.Info.IDPub
	c.mu.Unlock()
	inner, ok := e2e.VerifyVoice(idPub, sender, payload[4:])
	if !ok {
		return
	}
	keyID, ctr, ok := e2e.ParseVoice(inner)
	if !ok {
		return
	}
	c.mu.Lock()
	k := p.key(keyID)
	if k == nil || s.peers[sender] != p {
		c.mu.Unlock()
		return
	}
	pt, err := k.OpenVoice(sender, inner)
	if err != nil || len(pt) < 3 || (pt[0] != proto.CodecVP8 && pt[0] != proto.CodecVP9) || !p.vwins[keyID].Check(ctr) {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	if f := c.voiceOut.Load(); f != nil {
		frame := make([]byte, 7+len(pt)-2)
		frame[0] = 2
		binary.BigEndian.PutUint32(frame[1:], sender)
		frame[5] = pt[1] // flags (bit 0 = keyframe)
		frame[6] = pt[0] // codec
		copy(frame[7:], pt[2:])
		(*f)(frame)
	}
}
