package server

import (
	"bufio"
	"crypto/cipher"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"securespeak/internal/e2e"
	"securespeak/internal/proto"
)

type Channel struct {
	ID           uint32 `json:"id"`
	Parent       uint32 `json:"parent"`
	Name         string `json:"name"`
	Topic        string `json:"topic"`
	Description  string `json:"description"`
	PasswordHash string `json:"password_hash,omitempty"`
	MaxUsers     int    `json:"max_users"`
	Permanent    bool   `json:"permanent"`
	Default      bool   `json:"default"`
	Order        int    `json:"order"`
	Owner        uint32 `json:"-"`
}

type Ban struct {
	IP     string `json:"ip"`
	Key    string `json:"key,omitempty"` // banned identity (hex public key)
	Nick   string `json:"nick"`
	Reason string `json:"reason"`
	Until  int64  `json:"until"`
	By     string `json:"by"`
}

type outFrame struct {
	kind byte
	data []byte
}

type Client struct {
	ID          uint32
	Nick        string
	IDPub       []byte
	EphPub      []byte
	Sig         []byte
	Channel     uint32
	Key         string   // hex identity key (proven on connect)
	Sharing     bool     // screen share active
	Watching    uint32   // id of the sharer this client watches (0 = none)
	Roles       []uint32 // assigned roles (besides the default role)
	Owner       bool
	Muted       bool
	Deaf        bool
	ServerMuted bool
	Away        bool
	AwayMsg     string

	ip   string
	conn net.Conn
	out  chan outFrame
	done chan struct{}
	once sync.Once

	udpAEAD cipher.AEAD
	udpAddr atomic.Pointer[net.UDPAddr]
	udpLast atomic.Int64
	udpCtr  atomic.Uint64
	udpWin  proto.ReplayWindow // only touched by the UDP goroutine

	limMu      sync.Mutex
	voiceTok   float64
	voiceLast  time.Time
	msgTok     float64
	msgLast    time.Time
	fileTok    float64
	fileLast   time.Time
	videoTok   float64
	videoLast  time.Time
	failedAuth int
}

type Server struct {
	cfgPath string
	dataDir string
	CertFP  string

	mu       sync.RWMutex
	cfg      *Config
	channels map[uint32]*Channel
	clients  map[uint32]*Client
	bans     []Ban
	roles    []*Role
	members  map[string]*Member
	keys     []*PrivKey
	nextRole uint32
	nextChan uint32
	nextCli  uint32

	tlsCfg *tls.Config
	udp    *net.UDPConn
	tcp    net.Listener
	Logf   func(format string, args ...any)
}

func New(dataDir string) (*Server, error) {
	cfgPath := filepath.Join(dataDir, "config.json")
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	cert, fp, err := LoadOrCreateCert(dataDir)
	if err != nil {
		return nil, fmt.Errorf("certificate: %w", err)
	}
	s := &Server{
		cfgPath:  cfgPath,
		dataDir:  dataDir,
		CertFP:   fp,
		cfg:      cfg,
		channels: map[uint32]*Channel{},
		clients:  map[uint32]*Client{},
		nextChan: 1,
		nextCli:  1,
		Logf:     log.Printf,
		tlsCfg: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		},
	}
	s.nextRole = 1
	s.loadChannels()
	s.loadBans()
	s.loadRoles()
	return s, nil
}

// ---- persistence ---------------------------------------------------------------

func (s *Server) loadChannels() {
	var list []*Channel
	if b, err := os.ReadFile(filepath.Join(s.dataDir, "channels.json")); err == nil {
		if err := json.Unmarshal(b, &list); err != nil {
			s.Logf("channels.json: %v", err)
		}
	}
	for _, c := range list {
		c.Permanent = true
		s.channels[c.ID] = c
		if c.ID >= s.nextChan {
			s.nextChan = c.ID + 1
		}
	}
	for _, c := range s.channels { // orphan fix
		if c.Parent != 0 && s.channels[c.Parent] == nil {
			c.Parent = 0
		}
	}
	if len(s.channels) == 0 {
		for i, name := range []string{"Lobby", "Talk 1", "Talk 2", "AFK"} {
			id := s.nextChan
			s.nextChan++
			s.channels[id] = &Channel{ID: id, Name: name, Permanent: true, Default: i == 0, Order: i}
		}
		s.channels[4].Topic = "Away from keyboard"
		s.saveChannelsLocked()
	}
	s.ensureDefaultLocked()
}

func (s *Server) ensureDefaultLocked() {
	var def *Channel
	for _, c := range s.channels {
		if c.Default {
			if def == nil || c.ID < def.ID {
				def = c
			}
		}
	}
	if def == nil {
		ids := s.sortedChannelIDs()
		for _, id := range ids {
			if s.channels[id].Permanent && s.channels[id].Parent == 0 {
				def = s.channels[id]
				break
			}
		}
	}
	if def == nil {
		id := s.nextChan
		s.nextChan++
		def = &Channel{ID: id, Name: "Lobby", Permanent: true}
		s.channels[id] = def
	}
	for _, c := range s.channels {
		c.Default = c == def
	}
}

func (s *Server) sortedChannelIDs() []uint32 {
	ids := make([]uint32, 0, len(s.channels))
	for id := range s.channels {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.channels[ids[i]], s.channels[ids[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.ID < b.ID
	})
	return ids
}

func (s *Server) saveChannelsLocked() {
	var list []*Channel
	for _, id := range s.sortedChannelIDs() {
		if c := s.channels[id]; c.Permanent {
			list = append(list, c)
		}
	}
	if err := SaveJSON(filepath.Join(s.dataDir, "channels.json"), list); err != nil {
		s.Logf("save channels: %v", err)
	}
}

func (s *Server) loadBans() {
	if b, err := os.ReadFile(filepath.Join(s.dataDir, "bans.json")); err == nil {
		json.Unmarshal(b, &s.bans)
	}
}

func (s *Server) saveBansLocked() {
	if s.bans == nil {
		s.bans = []Ban{}
	}
	if err := SaveJSON(filepath.Join(s.dataDir, "bans.json"), s.bans); err != nil {
		s.Logf("save bans: %v", err)
	}
}

func (s *Server) isBannedKey(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().Unix()
	for _, b := range s.bans {
		if b.Key == key && (b.Until == 0 || b.Until > now) {
			return true
		}
	}
	return false
}

func (s *Server) isBanned(ip string) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	kept := s.bans[:0]
	changed := false
	banned, reason := false, ""
	for _, b := range s.bans {
		if b.Until != 0 && b.Until < now {
			changed = true
			continue
		}
		kept = append(kept, b)
		if b.IP == ip {
			banned, reason = true, b.Reason
		}
	}
	s.bans = kept
	if changed {
		s.saveBansLocked()
	}
	return banned, reason
}

// ---- run -------------------------------------------------------------------------

func (s *Server) Run() error {
	if err := s.Listen(); err != nil {
		return err
	}
	return s.Serve()
}

// Listen opens the TCP and UDP sockets.
func (s *Server) Listen() error {
	ua, err := net.ResolveUDPAddr("udp", s.cfg.ListenUDP)
	if err != nil {
		return err
	}
	s.udp, err = net.ListenUDP("udp", ua)
	if err != nil {
		return fmt.Errorf("udp listen: %w", err)
	}
	s.tcp, err = net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("tcp listen: %w", err)
	}
	s.Logf("listening on tcp %s, udp %s", s.tcp.Addr(), s.udp.LocalAddr())
	s.Logf("certificate fingerprint (SHA-256): %s", s.CertFP)
	return nil
}

// Serve accepts connections until Close is called.
func (s *Server) Serve() error {
	go s.udpLoop()
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.Logf("accept: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go s.handleConn(conn)
	}
}

// Addrs returns the actual listening addresses (useful when ports are 0).
func (s *Server) Addrs() (tcp, udp string) {
	return s.tcp.Addr().String(), s.udp.LocalAddr().String()
}

func (s *Server) Close() {
	if s.tcp != nil {
		s.tcp.Close()
	}
	if s.udp != nil {
		s.udp.Close()
	}
	s.mu.RLock()
	for _, c := range s.clients {
		c.close()
	}
	s.mu.RUnlock()
}

// Start listens and serves in the background (used by tests).
func (s *Server) Start() error {
	if err := s.Listen(); err != nil {
		return err
	}
	go s.Serve()
	return nil
}

// ---- connection handling -----------------------------------------------------------

func (c *Client) close() {
	c.once.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

func (c *Client) sendFrame(kind byte, data []byte) {
	select {
	case c.out <- outFrame{kind, data}:
	case <-c.done:
	default:
		if kind == proto.FrameJSON { // control message backlog: client is hopelessly slow
			c.close()
		}
	}
}

func (c *Client) send(t string, v any) {
	b, err := proto.Encode(t, v)
	if err != nil {
		return
	}
	c.sendFrame(proto.FrameJSON, b)
}

func (c *Client) sendErr(code, msg string) { c.send(proto.TError, proto.Error{Code: code, Msg: msg}) }

func (c *Client) writer() {
	w := bufio.NewWriterSize(c.conn, 32*1024)
	for {
		select {
		case f := <-c.out:
			c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if err := proto.WriteFrame(w, f.kind, f.data); err != nil {
				c.close()
				return
			}
			if len(c.out) == 0 {
				if err := w.Flush(); err != nil {
					c.close()
					return
				}
			}
		case <-c.done:
			return
		}
	}
}

func remoteIP(conn net.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

func cleanText(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}

func cleanName(s string, max int) string {
	s = strings.ReplaceAll(cleanText(s, max), "\n", " ")
	return strings.TrimSpace(strings.ReplaceAll(s, "\t", " "))
}

func (s *Server) handleConn(raw net.Conn) {
	ip := remoteIP(raw)
	if banned, _ := s.isBanned(ip); banned {
		raw.Close()
		return
	}
	conn := tls.Server(raw, s.tlsCfg)
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := conn.Handshake(); err != nil {
		conn.Close()
		return
	}
	br := bufio.NewReaderSize(conn, 64*1024)
	kind, payload, err := proto.ReadFrame(br)
	if err != nil || kind != proto.FrameJSON {
		conn.Close()
		return
	}
	var m proto.Message
	var h proto.Hello
	fail := func(code, msg string) {
		b, _ := proto.Encode(proto.TError, proto.Error{Code: code, Msg: msg})
		proto.WriteFrame(conn, proto.FrameJSON, b)
		conn.Close()
	}
	if json.Unmarshal(payload, &m) != nil || m.T != proto.THello || json.Unmarshal(m.D, &h) != nil {
		fail("protocol", "expected hello")
		return
	}
	if h.Version != proto.Version {
		fail("version", fmt.Sprintf("protocol version mismatch (server %d, client %d)", proto.Version, h.Version))
		return
	}
	if !e2e.VerifyPeer(h.IDPub, h.EphPub, h.Sig) {
		fail("protocol", "invalid identity signature")
		return
	}
	// Proof of identity bound to this TLS connection (prevents replaying
	// someone else's public key to obtain their roles).
	cs := conn.ConnectionState()
	ekm, err := cs.ExportKeyingMaterial(proto.AuthEKMLabel, nil, 32)
	if err != nil || !e2e.AuthVerify(h.IDPub, ekm, h.AuthSig) {
		fail("auth", "identity authentication failed")
		return
	}
	key := hex.EncodeToString(h.IDPub)
	if s.isBannedKey(key) {
		fail("banned", "you are banned from this server")
		return
	}
	s.mu.RLock()
	pwHash := s.cfg.ServerPasswordHash
	s.mu.RUnlock()
	if pwHash != "" && !CheckPassword(pwHash, h.ServerPassword) {
		time.Sleep(time.Second)
		fail("password", "wrong server password")
		return
	}
	nick := cleanName(h.Nick, proto.MaxNickLen)
	if nick == "" {
		fail("nick", "invalid nickname")
		return
	}
	conn.SetDeadline(time.Time{})

	udpKey := e2e.RandomKey()
	aead, _ := e2e.NewGCM(udpKey)
	c := &Client{
		IDPub: h.IDPub, EphPub: h.EphPub, Sig: h.Sig, Key: key,
		ip: ip, conn: conn,
		out:     make(chan outFrame, 2048),
		done:    make(chan struct{}),
		udpAEAD: aead,
	}

	s.mu.Lock()
	if len(s.clients) >= s.cfg.MaxClients {
		s.mu.Unlock()
		fail("full", "server is full")
		return
	}
	c.ID = s.nextCli
	s.nextCli++
	c.Nick = s.uniqueNickLocked(nick, 0)
	c.Channel = s.defaultChannelLocked()
	if mem := s.members[key]; mem != nil { // roles follow the identity
		c.Roles = append([]uint32(nil), mem.Roles...)
		c.Owner = mem.Owner
		mem.Nick, mem.LastSeen = c.Nick, time.Now().Unix()
		s.saveMembersLocked()
	}
	s.clients[c.ID] = c
	welcome := proto.Welcome{
		YourID:     c.ID,
		ServerName: s.cfg.Name,
		WelcomeMsg: s.cfg.WelcomeMessage,
		UDPPort:    s.udpPort(),
		UDPKey:     udpKey,
		MaxClients: s.cfg.MaxClients,
		Whisper:    s.cfg.AllowWhisper,
		Roles:      s.roleInfosLocked(),
		HasPw:      s.cfg.ServerPasswordHash != "",
	}
	for _, id := range s.sortedChannelIDs() {
		welcome.Channels = append(welcome.Channels, s.channelInfo(s.channels[id]))
	}
	for _, o := range s.clients {
		welcome.Clients = append(welcome.Clients, s.clientInfo(o))
	}
	go c.writer()
	c.send(proto.TWelcome, welcome)
	s.broadcastLocked(proto.TClientJoin, s.clientInfo(c), c.ID)
	s.mu.Unlock()

	s.Logf("client %d %q connected from %s", c.ID, c.Nick, ip)
	s.readLoop(c, br)

	// disconnect
	c.close()
	s.mu.Lock()
	if _, ok := s.clients[c.ID]; ok {
		delete(s.clients, c.ID)
		old := c.Channel
		s.broadcastLocked(proto.TClientLeave, proto.ClientLeave{ID: c.ID}, 0)
		s.cleanupTempLocked(old)
	}
	s.mu.Unlock()
	s.Logf("client %d %q disconnected", c.ID, c.Nick)
}

func (s *Server) udpPort() int {
	if s.cfg.AdvertiseUDPPort != 0 {
		return s.cfg.AdvertiseUDPPort
	}
	if s.udp != nil {
		return s.udp.LocalAddr().(*net.UDPAddr).Port
	}
	return proto.DefaultPort
}

func (s *Server) uniqueNickLocked(nick string, self uint32) string {
	taken := func(n string) bool {
		for _, o := range s.clients {
			if o.ID != self && strings.EqualFold(o.Nick, n) {
				return true
			}
		}
		return false
	}
	if !taken(nick) {
		return nick
	}
	base := nick
	if r := []rune(base); len(r) > proto.MaxNickLen-3 {
		base = string(r[:proto.MaxNickLen-3])
	}
	for i := 1; ; i++ {
		n := base + strconv.Itoa(i)
		if !taken(n) {
			return n
		}
	}
}

func (s *Server) defaultChannelLocked() uint32 {
	for _, c := range s.channels {
		if c.Default {
			return c.ID
		}
	}
	s.ensureDefaultLocked()
	return s.defaultChannelLocked()
}

func (s *Server) channelInfo(c *Channel) proto.ChannelInfo {
	return proto.ChannelInfo{
		ID: c.ID, Parent: c.Parent, Name: c.Name, Topic: c.Topic, Description: c.Description,
		HasPassword: c.PasswordHash != "", MaxUsers: c.MaxUsers, Permanent: c.Permanent,
		Default: c.Default, Order: c.Order, Owner: c.Owner,
	}
}

func (s *Server) clientInfo(c *Client) proto.ClientInfo {
	return proto.ClientInfo{
		ID: c.ID, Nick: c.Nick, Channel: c.Channel, IDPub: c.IDPub, EphPub: c.EphPub, Sig: c.Sig,
		Roles: c.Roles, Owner: c.Owner, Sharing: c.Sharing, Muted: c.Muted, Deaf: c.Deaf, ServerMuted: c.ServerMuted, Away: c.Away, AwayMsg: c.AwayMsg,
	}
}

func (s *Server) broadcastLocked(t string, v any, except uint32) {
	b, err := proto.Encode(t, v)
	if err != nil {
		return
	}
	for _, c := range s.clients {
		if c.ID != except {
			c.sendFrame(proto.FrameJSON, b)
		}
	}
}

// ---- rate limiting -------------------------------------------------------------

func bucket(tok *float64, last *time.Time, rate, burst float64) bool {
	now := time.Now()
	if last.IsZero() {
		*tok = burst
	} else {
		*tok += now.Sub(*last).Seconds() * rate
		if *tok > burst {
			*tok = burst
		}
	}
	*last = now
	if *tok < 1 {
		return false
	}
	*tok--
	return true
}

func (c *Client) allowVoice() bool {
	c.limMu.Lock()
	defer c.limMu.Unlock()
	return bucket(&c.voiceTok, &c.voiceLast, 60, 30)
}

// allowVideoBytes limits screen share traffic to ~8 MB/s per sharer.
func (c *Client) allowVideoBytes(n int) bool {
	c.limMu.Lock()
	defer c.limMu.Unlock()
	const rate, burst = 8 << 20, 16 << 20
	now := time.Now()
	if c.videoLast.IsZero() {
		c.videoTok = burst
	} else {
		c.videoTok = min(burst, c.videoTok+now.Sub(c.videoLast).Seconds()*rate)
	}
	c.videoLast = now
	if c.videoTok < float64(n) {
		return false
	}
	c.videoTok -= float64(n)
	return true
}

// allowFileBytes limits relayed file data to ~16 MB/s per client.
func (c *Client) allowFileBytes(n int) bool {
	c.limMu.Lock()
	defer c.limMu.Unlock()
	const rate, burst = 16 << 20, 32 << 20
	now := time.Now()
	if c.fileLast.IsZero() {
		c.fileTok = burst
	} else {
		c.fileTok = min(burst, c.fileTok+now.Sub(c.fileLast).Seconds()*rate)
	}
	c.fileLast = now
	if c.fileTok < float64(n) {
		return false
	}
	c.fileTok -= float64(n)
	return true
}

func (c *Client) allowMsg() bool {
	c.limMu.Lock()
	defer c.limMu.Unlock()
	return bucket(&c.msgTok, &c.msgLast, 20, 60)
}

// ---- message loop ------------------------------------------------------------------

func (s *Server) readLoop(c *Client, br *bufio.Reader) {
	for {
		c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		kind, payload, err := proto.ReadFrame(br)
		if err != nil {
			return
		}
		switch kind {
		case proto.FrameVoice:
			if c.allowVoice() {
				s.routeVoice(c, payload)
			}
		case proto.FrameVideo:
			if c.allowVideoBytes(len(payload)) {
				s.routeVideo(c, payload)
			}
		case proto.FrameJSON:
			var m proto.Message
			if json.Unmarshal(payload, &m) != nil {
				return
			}
			if m.T == proto.TFileData || m.T == proto.TFileCtl {
				if !c.allowFileBytes(len(payload)) {
					continue // sender retransmits after its window times out
				}
			} else if !c.allowMsg() {
				c.sendErr("rate", "slow down")
				continue
			}
			if !s.handle(c, m) {
				return
			}
		}
	}
}

func decode[T any](m proto.Message) (T, bool) {
	var v T
	err := json.Unmarshal(m.D, &v)
	return v, err == nil
}

// handle processes one control message; returning false drops the client.
func (s *Server) handle(c *Client, m proto.Message) bool {
	switch m.T {
	case proto.TPing:
		if p, ok := decode[proto.Ping](m); ok {
			c.send(proto.TPong, p)
		}
	case proto.TJoin:
		if j, ok := decode[proto.Join](m); ok {
			s.join(c, j)
		}
	case proto.TStatus:
		if st, ok := decode[proto.Status](m); ok {
			s.mu.Lock()
			c.Muted, c.Deaf, c.Away = st.Muted, st.Deaf, st.Away
			c.AwayMsg = cleanName(st.AwayMsg, 80)
			if st.Sharing && !s.hasLocked(c, proto.PermScreenShare) {
				st.Sharing = false
				c.sendErr("permission", "you are not allowed to share your screen")
			}
			c.Sharing = st.Sharing
			s.broadcastLocked(proto.TClientUpdate, s.clientInfo(c), 0)
			s.mu.Unlock()
		}
	case proto.TWatch:
		if w, ok := decode[proto.Watch](m); ok {
			s.watch(c, w)
		}
	case proto.TKeyframeReq:
		if kr, ok := decode[proto.KeyframeReq](m); ok {
			s.mu.RLock()
			sharer := s.clients[kr.To]
			if sharer != nil && c.Watching == sharer.ID {
				sharer.send(proto.TKeyframeReq, proto.KeyframeReq{From: c.ID})
			}
			s.mu.RUnlock()
		}
	case proto.TNickChange:
		if n, ok := decode[proto.Nick](m); ok {
			nick := cleanName(n.Nick, proto.MaxNickLen)
			if nick == "" {
				c.sendErr("nick", "invalid nickname")
				break
			}
			s.mu.Lock()
			c.Nick = s.uniqueNickLocked(nick, c.ID)
			s.broadcastLocked(proto.TClientUpdate, s.clientInfo(c), 0)
			s.mu.Unlock()
		}
	case proto.TChat:
		if ch, ok := decode[proto.Chat](m); ok {
			s.chat(c, ch)
		}
	case proto.TKeyDist:
		if kd, ok := decode[proto.KeyDist](m); ok && len(kd.Blob) <= 256 {
			s.mu.RLock()
			to := s.clients[kd.To]
			s.mu.RUnlock()
			if to != nil && to != c {
				to.send(proto.TKeyDist, proto.KeyDist{From: c.ID, Blob: kd.Blob})
			}
		}
	case proto.TFileCtl:
		if fc, ok := decode[proto.FileCtl](m); ok && len(fc.Blob) <= 1024 {
			s.relayFile(c, fc.To, proto.TFileCtl, proto.FileCtl{From: c.ID, Blob: fc.Blob})
		}
	case proto.TFileData:
		if fd, ok := decode[proto.FileData](m); ok && len(fd.Data) <= proto.FileChunkSize+64 && len(fd.ID) <= 64 {
			to := fd.To
			fd.To, fd.From = 0, c.ID
			s.relayFile(c, to, proto.TFileData, fd)
		}
	case proto.TChanCreate:
		if cc, ok := decode[proto.ChanCreate](m); ok {
			s.chanCreate(c, cc)
		}
	case proto.TChanEdit:
		if ce, ok := decode[proto.ChanEdit](m); ok {
			s.chanEdit(c, ce)
		}
	case proto.TChanDelete:
		if cd, ok := decode[proto.IDOnly](m); ok {
			s.chanDelete(c, cd.ID)
		}
	default:
		if handled, keep := s.handleRoles(c, m); handled {
			return keep
		}
		return s.handleAdmin(c, m)
	}
	return true
}

func (s *Server) join(c *Client, j proto.Join) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.channels[j.Channel]
	if ch == nil {
		c.sendErr("channel", "channel does not exist")
		return
	}
	if c.Channel == ch.ID {
		return
	}
	if !s.hasLocked(c, proto.PermChannelJoinAny) {
		if ch.PasswordHash != "" && !CheckPassword(ch.PasswordHash, j.Password) {
			c.sendErr("channel_password", "wrong channel password")
			return
		}
		if ch.MaxUsers > 0 && s.countInLocked(ch.ID) >= ch.MaxUsers {
			c.sendErr("channel_full", "channel is full")
			return
		}
	}
	s.moveLocked(c, ch.ID, 0)
}

func (s *Server) countInLocked(chID uint32) int {
	n := 0
	for _, o := range s.clients {
		if o.Channel == chID {
			n++
		}
	}
	return n
}

func (s *Server) moveLocked(c *Client, chID, by uint32) {
	old := c.Channel
	c.Channel = chID
	s.broadcastLocked(proto.TClientMove, proto.Move{ID: c.ID, Channel: chID, By: by}, 0)
	s.cleanupTempLocked(old)
}

// cleanupTempLocked deletes empty temporary channels, walking up the tree.
func (s *Server) cleanupTempLocked(chID uint32) {
	for chID != 0 {
		ch := s.channels[chID]
		if ch == nil || ch.Permanent || ch.Default {
			return
		}
		if s.countInLocked(chID) > 0 {
			return
		}
		for _, o := range s.channels {
			if o.Parent == chID {
				return
			}
		}
		delete(s.channels, chID)
		s.broadcastLocked(proto.TChanRemove, proto.IDOnly{ID: chID}, 0)
		chID = ch.Parent
	}
}

func (s *Server) siblingNameTakenLocked(parent uint32, name string, self uint32) bool {
	for _, o := range s.channels {
		if o.Parent == parent && o.ID != self && strings.EqualFold(o.Name, name) {
			return true
		}
	}
	return false
}

func (s *Server) depthLocked(id uint32) int {
	d := 0
	for id != 0 && d < 100 {
		ch := s.channels[id]
		if ch == nil {
			break
		}
		id = ch.Parent
		d++
	}
	return d
}

func (s *Server) chanCreate(c *Client, cc proto.ChanCreate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := cleanName(cc.Name, 40)
	if name == "" {
		c.sendErr("channel", "channel name required")
		return
	}
	if cc.Permanent && !s.hasLocked(c, proto.PermChannelCreatePerm) {
		c.sendErr("permission", "you cannot create permanent channels")
		return
	}
	if !cc.Permanent && !s.hasLocked(c, proto.PermChannelCreatePerm) &&
		!(s.cfg.AllowTempChannels && s.hasLocked(c, proto.PermChannelCreateTemp)) {
		c.sendErr("permission", "you cannot create channels")
		return
	}
	if cc.Parent != 0 {
		p := s.channels[cc.Parent]
		if p == nil {
			c.sendErr("channel", "parent channel does not exist")
			return
		}
		if cc.Permanent && !p.Permanent {
			c.sendErr("channel", "a permanent channel cannot be inside a temporary one")
			return
		}
		if s.depthLocked(cc.Parent) >= 8 {
			c.sendErr("channel", "channels are nested too deep")
			return
		}
	}
	if len(s.channels) >= 1000 {
		c.sendErr("channel", "too many channels")
		return
	}
	if s.siblingNameTakenLocked(cc.Parent, name, 0) {
		c.sendErr("channel", "a channel with that name already exists here")
		return
	}
	ch := &Channel{
		ID: s.nextChan, Parent: cc.Parent, Name: name,
		Topic: cleanName(cc.Topic, 120), Description: cleanText(cc.Description, 2000),
		MaxUsers: max(0, cc.MaxUsers), Permanent: cc.Permanent,
	}
	s.nextChan++
	maxOrder := -1
	for _, o := range s.channels {
		if o.Parent == ch.Parent && o.Order > maxOrder {
			maxOrder = o.Order
		}
	}
	ch.Order = maxOrder + 1
	if cc.Password != "" {
		ch.PasswordHash = HashPassword(cc.Password)
	}
	if !ch.Permanent {
		ch.Owner = c.ID
	}
	s.channels[ch.ID] = ch
	s.broadcastLocked(proto.TChanAdd, s.channelInfo(ch), 0)
	if ch.Permanent {
		s.saveChannelsLocked()
	} else {
		s.moveLocked(c, ch.ID, 0) // creator joins their temporary channel
	}
}

func (s *Server) canEditLocked(c *Client, ch *Channel) bool {
	return s.hasLocked(c, proto.PermChannelEdit) || (!ch.Permanent && ch.Owner == c.ID)
}

func (s *Server) chanEdit(c *Client, ce proto.ChanEdit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.channels[ce.ID]
	if ch == nil {
		c.sendErr("channel", "channel does not exist")
		return
	}
	if !s.canEditLocked(c, ch) {
		c.sendErr("permission", "you cannot edit this channel")
		return
	}
	name := cleanName(ce.Name, 40)
	if name == "" {
		c.sendErr("channel", "channel name required")
		return
	}
	if s.siblingNameTakenLocked(ch.Parent, name, ch.ID) {
		c.sendErr("channel", "a channel with that name already exists here")
		return
	}
	wasPerm := ch.Permanent
	if s.hasLocked(c, proto.PermChannelEdit) {
		if !ce.Permanent && ch.Permanent {
			for _, o := range s.channels {
				if o.Parent == ch.ID && o.Permanent {
					c.sendErr("channel", "channel has permanent sub-channels")
					return
				}
			}
			if ch.Default {
				c.sendErr("channel", "the default channel must be permanent")
				return
			}
		}
		if ce.Permanent && !ch.Permanent && ch.Parent != 0 && !s.channels[ch.Parent].Permanent {
			c.sendErr("channel", "parent channel is temporary")
			return
		}
		ch.Permanent = ce.Permanent
		if ch.Permanent {
			ch.Owner = 0
		}
		ch.Order = ce.Order
		if ce.Default && ch.Permanent && !ch.Default {
			for _, o := range s.channels {
				if o.Default {
					o.Default = false
					s.broadcastLocked(proto.TChanUpdate, s.channelInfo(o), 0)
				}
			}
			ch.Default = true
		}
	}
	ch.Name = name
	ch.Topic = cleanName(ce.Topic, 120)
	ch.Description = cleanText(ce.Description, 2000)
	ch.MaxUsers = max(0, ce.MaxUsers)
	if ce.Password != nil {
		if *ce.Password == "" {
			ch.PasswordHash = ""
		} else {
			ch.PasswordHash = HashPassword(*ce.Password)
		}
	}
	s.broadcastLocked(proto.TChanUpdate, s.channelInfo(ch), 0)
	if ch.Permanent || wasPerm {
		s.saveChannelsLocked()
	}
	if !ch.Permanent {
		s.cleanupTempLocked(ch.ID)
	}
}

func (s *Server) chanDelete(c *Client, id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.channels[id]
	if ch == nil {
		return
	}
	if !s.hasLocked(c, proto.PermChannelDelete) && !(!ch.Permanent && ch.Owner == c.ID) {
		c.sendErr("permission", "you cannot delete this channel")
		return
	}
	if ch.Default {
		c.sendErr("channel", "the default channel cannot be deleted")
		return
	}
	// collect subtree
	del := map[uint32]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, o := range s.channels {
			if !del[o.ID] && del[o.Parent] {
				del[o.ID] = true
				changed = true
			}
		}
	}
	for _, o := range s.channels {
		if o.Default && del[o.ID] {
			c.sendErr("channel", "this would delete the default channel")
			return
		}
	}
	def := s.defaultChannelLocked()
	for _, o := range s.clients {
		if del[o.Channel] {
			o.Channel = def
			s.broadcastLocked(proto.TClientMove, proto.Move{ID: o.ID, Channel: def, By: c.ID}, 0)
		}
	}
	// remove children before parents so clients never see orphans
	for len(del) > 0 {
		for cid := range del {
			leaf := true
			for oid := range del {
				if s.channels[oid] != nil && s.channels[oid].Parent == cid {
					leaf = false
					break
				}
			}
			if leaf {
				delete(s.channels, cid)
				delete(del, cid)
				s.broadcastLocked(proto.TChanRemove, proto.IDOnly{ID: cid}, 0)
			}
		}
	}
	s.saveChannelsLocked()
}

// relayFile forwards opaque, end-to-end encrypted file traffic to one client.
func (s *Server) relayFile(c *Client, to uint32, t string, v any) {
	s.mu.RLock()
	allowed := s.cfg.AllowFiles && (t == proto.TFileCtl || s.hasLocked(c, proto.PermFiles))
	dst := s.clients[to]
	s.mu.RUnlock()
	if !allowed {
		c.sendErr("files", "you are not allowed to send files on this server")
		return
	}
	if dst != nil && dst != c {
		dst.send(t, v)
	}
}

func (s *Server) chat(c *Client, m proto.Chat) {
	if len(m.CT) > 64*1024 || len(m.Nonce) != 12 || len(m.Wraps) == 0 {
		c.sendErr("chat", "invalid message")
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	need := map[string]string{"channel": proto.PermChatChannel, "server": proto.PermChatServer,
		"private": proto.PermChatPrivate, "poke": proto.PermPoke}[m.Kind]
	if need != "" && !s.hasLocked(c, need) {
		c.sendErr("permission", "you are not allowed to send this kind of message")
		return
	}
	switch m.Kind {
	case "channel":
		if m.Target != c.Channel {
			return
		}
	case "private", "poke":
		if len(m.Wraps) != 1 {
			return
		}
		if _, ok := m.Wraps[strconv.FormatUint(uint64(m.Target), 10)]; !ok {
			return
		}
	case "server", "avatar": // avatar: profile picture, sent to any set of users
		m.Target = 0
	default:
		return
	}
	m.From = c.ID
	m.TS = time.Now().UnixMilli()
	all := m.Wraps
	for idStr, wrap := range all {
		id, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil || len(wrap) > 128 {
			continue
		}
		to := s.clients[uint32(id)]
		if to == nil || to == c {
			continue
		}
		if m.Kind == "channel" && to.Channel != c.Channel {
			continue
		}
		cp := m
		cp.Wraps = map[string][]byte{idStr: wrap} // each recipient only gets its own wrap
		to.send(proto.TChat, cp)
	}
}

// ---- admin --------------------------------------------------------------------

func (s *Server) handleAdmin(c *Client, m proto.Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	need := map[string]string{
		proto.TBan: proto.PermClientBan, proto.TBanList: proto.PermBanList, proto.TUnban: proto.PermBanList,
		proto.TMove: proto.PermClientMove, proto.TServerMute: proto.PermClientMute, proto.TServerEdit: proto.PermServerEdit,
		proto.TServerPw: proto.PermServerPassword,
	}[m.T]
	if m.T == proto.TKick {
		need = proto.PermClientKickChannel
		if k, _ := decode[proto.Kick](m); k.Server {
			need = proto.PermClientKickServer
		}
	}
	if need == "" {
		c.sendErr("protocol", "unknown message "+m.T)
		return true
	}
	if !s.hasLocked(c, need) {
		c.sendErr("permission", "you do not have permission for this")
		return true
	}
	// actions on another user require more power than theirs; the owner is untouchable
	targetOK := func(id uint32) *Client {
		t := s.clients[id]
		if t == nil {
			return nil
		}
		if t == c {
			c.sendErr("permission", "you cannot do that to yourself")
			return nil
		}
		if !s.canActLocked(c, t) {
			if t.Owner {
				c.sendErr("permission", "the server owner cannot be moderated")
			} else {
				c.sendErr("permission", fmt.Sprintf("%s has the same or more power than you", t.Nick))
			}
			return nil
		}
		return t
	}
	switch m.T {
	case proto.TKick:
		k, ok := decode[proto.Kick](m)
		if !ok {
			return true
		}
		t := targetOK(k.ID)
		if t == nil {
			return true
		}
		if k.Server {
			t.send(proto.TKicked, proto.Kicked{Reason: cleanName(k.Reason, 120), By: c.Nick})
			s.Logf("%q kicked %q from server: %s", c.Nick, t.Nick, k.Reason)
			go func() { time.Sleep(200 * time.Millisecond); t.close() }()
		} else {
			def := s.defaultChannelLocked()
			if t.Channel != def {
				s.moveLocked(t, def, c.ID)
				t.send(proto.TNotice, proto.Notice{Msg: fmt.Sprintf("%s kicked you from the channel. %s", c.Nick, cleanName(k.Reason, 120))})
			}
		}
	case proto.TBan:
		b, ok := decode[proto.Ban](m)
		if !ok {
			return true
		}
		t := targetOK(b.ID)
		if t == nil {
			return true
		}
		ban := Ban{IP: t.ip, Key: t.Key, Nick: t.Nick, Reason: cleanName(b.Reason, 120), By: c.Nick}
		if b.Minutes > 0 {
			ban.Until = time.Now().Add(time.Duration(b.Minutes) * time.Minute).Unix()
		}
		s.bans = append(s.bans, ban)
		s.saveBansLocked()
		for _, o := range s.clients { // that identity and everyone from that IP (except protected users)
			if (o.ip == t.ip || o.Key == t.Key) && (o == t || s.canActLocked(c, o)) {
				o.send(proto.TKicked, proto.Kicked{Reason: "Banned: " + ban.Reason, By: c.Nick})
				o := o
				go func() { time.Sleep(200 * time.Millisecond); o.close() }()
			}
		}
		s.Logf("%q banned %q (%s) for %d min: %s", c.Nick, t.Nick, t.ip, b.Minutes, ban.Reason)
	case proto.TBanList:
		var l proto.BanList
		for _, b := range s.bans {
			l.Bans = append(l.Bans, proto.BanInfo{IP: b.IP, Nick: b.Nick, Reason: b.Reason, Until: b.Until, By: b.By})
		}
		c.send(proto.TBanList, l)
	case proto.TUnban:
		u, _ := decode[proto.Unban](m)
		kept := s.bans[:0]
		for _, b := range s.bans {
			if b.IP != u.IP {
				kept = append(kept, b)
			}
		}
		s.bans = kept
		s.saveBansLocked()
		var l proto.BanList
		for _, b := range s.bans {
			l.Bans = append(l.Bans, proto.BanInfo{IP: b.IP, Nick: b.Nick, Reason: b.Reason, Until: b.Until, By: b.By})
		}
		c.send(proto.TBanList, l)
	case proto.TMove:
		mv, ok := decode[proto.Move](m)
		if !ok || s.channels[mv.Channel] == nil {
			return true
		}
		t := targetOK(mv.ID)
		if t == nil || t.Channel == mv.Channel {
			return true
		}
		s.moveLocked(t, mv.Channel, c.ID)
	case proto.TServerMute:
		sm, ok := decode[proto.ServerMute](m)
		if !ok {
			return true
		}
		t := targetOK(sm.ID)
		if t == nil {
			return true
		}
		t.ServerMuted = sm.On
		s.broadcastLocked(proto.TClientUpdate, s.clientInfo(t), 0)
	case proto.TServerEdit:
		se, ok := decode[proto.ServerEdit](m)
		if !ok {
			return true
		}
		if n := cleanName(se.Name, 60); n != "" {
			s.cfg.Name = n
		}
		s.cfg.WelcomeMessage = cleanText(se.WelcomeMsg, 2000)
		if err := SaveJSON(s.cfgPath, s.cfg); err != nil {
			s.Logf("save config: %v", err)
		}
		s.broadcastLocked(proto.TServerUpdate, s.serverEditLocked(), 0)
	case proto.TServerPw:
		sp, ok := decode[proto.ServerPassword](m)
		if !ok {
			return true
		}
		switch {
		case sp.Remove:
			s.cfg.ServerPasswordHash = ""
		case sp.Password == "" || len(sp.Password) > 128:
			c.sendErr("password", "the password must be 1-128 characters")
			return true
		default:
			s.cfg.ServerPasswordHash = HashPassword(sp.Password)
		}
		if err := SaveJSON(s.cfgPath, s.cfg); err != nil {
			s.Logf("save config: %v", err)
			c.sendErr("password", "could not save the server configuration")
			return true
		}
		msg := "The server password was changed. People who are connected stay connected; new connections need the new password."
		if sp.Remove {
			msg = "The server password was removed. Anyone with the address can connect now."
		}
		c.send(proto.TNotice, proto.Notice{Msg: msg})
		s.broadcastLocked(proto.TServerUpdate, s.serverEditLocked(), 0)
		s.Logf("%q %s the server password", c.Nick, map[bool]string{true: "removed", false: "changed"}[sp.Remove])
	default:
		c.sendErr("protocol", "unknown message "+m.T)
	}
	return true
}

// ---- voice -------------------------------------------------------------------------

// routeVoice relays a voice payload from a client:
//
//	[mode=0][signed inner]                      -> everyone in the sender's channel
//	[mode=1][n][n x u32 ids][signed inner]      -> whisper to the listed clients
//
// "signed inner" is E2E encrypted and Ed25519-signed by the speaker; the server
// cannot read or forge it. Recipients get [sender u32][mode][signed inner].
func (s *Server) routeVoice(c *Client, payload []byte) {
	if len(payload) < 1 || len(payload) > proto.MaxVoicePacket {
		return
	}
	mode := payload[0]
	var ids []uint32
	var inner []byte
	switch mode {
	case proto.VoiceChannel:
		inner = payload[1:]
	case proto.VoiceWhisper:
		if len(payload) < 2 {
			return
		}
		n := int(payload[1])
		if n == 0 || n > proto.MaxWhisperTargets || len(payload) < 2+4*n {
			return
		}
		for i := 0; i < n; i++ {
			ids = append(ids, binary.BigEndian.Uint32(payload[2+4*i:]))
		}
		inner = payload[2+4*n:]
	default:
		return
	}
	if len(inner) < e2e.VoiceHeaderLen+16+e2e.VoiceSigLen {
		return
	}
	s.mu.RLock()
	if c.ServerMuted || c.Muted || !s.hasLocked(c, proto.PermVoiceSpeak) ||
		(mode == proto.VoiceWhisper && (!s.cfg.AllowWhisper || !s.hasLocked(c, proto.PermVoiceWhisper))) {
		s.mu.RUnlock()
		return
	}
	var targets []*Client
	if mode == proto.VoiceChannel {
		for _, o := range s.clients {
			if o != c && o.Channel == c.Channel && !o.Deaf {
				targets = append(targets, o)
			}
		}
	} else {
		seen := map[uint32]bool{}
		for _, id := range ids {
			if o := s.clients[id]; o != nil && o != c && !o.Deaf && !seen[id] {
				seen[id] = true
				targets = append(targets, o)
			}
		}
	}
	s.mu.RUnlock()
	if len(targets) == 0 {
		return
	}
	out := make([]byte, 5+len(inner))
	binary.BigEndian.PutUint32(out, c.ID)
	out[4] = mode
	copy(out[5:], inner)
	now := time.Now().UnixNano()
	for _, t := range targets {
		if addr := t.udpAddr.Load(); addr != nil && now-t.udpLast.Load() < int64(20*time.Second) {
			pkt := proto.SealUDP(t.udpAEAD, proto.UDPVoice, t.ID, proto.DirServerToClient, t.udpCtr.Add(1), out)
			s.udp.WriteToUDP(pkt, addr)
		} else {
			t.sendFrame(proto.FrameVoice, out)
		}
	}
}

func (s *Server) serverEditLocked() proto.ServerEdit {
	return proto.ServerEdit{Name: s.cfg.Name, WelcomeMsg: s.cfg.WelcomeMessage, HasPw: s.cfg.ServerPasswordHash != ""}
}

// watch subscribes a client to a screen share in its own channel.
func (s *Server) watch(c *Client, w proto.Watch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !w.On {
		c.Watching = 0
		return
	}
	t := s.clients[w.ID]
	if t == nil || t == c || !t.Sharing || t.Channel != c.Channel {
		c.sendErr("screen", "this screen share is not available")
		return
	}
	c.Watching = t.ID
	t.send(proto.TKeyframeReq, proto.KeyframeReq{From: c.ID}) // new viewer needs a keyframe
}

// routeVideo relays an end-to-end encrypted, signed screen share frame to
// the clients watching the sender in the same channel. Slow viewers skip
// frames (and ask for a keyframe) instead of building up a backlog.
func (s *Server) routeVideo(c *Client, signed []byte) {
	if len(signed) < e2e.VoiceHeaderLen+16+e2e.VoiceSigLen || len(signed) > proto.MaxFrameSize-8 {
		return
	}
	s.mu.RLock()
	if !c.Sharing || !s.hasLocked(c, proto.PermScreenShare) {
		s.mu.RUnlock()
		return
	}
	var targets []*Client
	for _, o := range s.clients {
		if o.Watching == c.ID && o.Channel == c.Channel {
			targets = append(targets, o)
		}
	}
	s.mu.RUnlock()
	if len(targets) == 0 {
		return
	}
	out := make([]byte, 4+len(signed))
	binary.BigEndian.PutUint32(out, c.ID)
	copy(out[4:], signed)
	for _, t := range targets {
		if len(t.out) < 64 {
			t.sendFrame(proto.FrameVideo, out)
		}
	}
}

func (s *Server) udpLoop() {
	buf := make([]byte, 8192)
	for {
		n, addr, err := s.udp.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		pkt := buf[:n]
		typ, id, ok := proto.UDPHeader(pkt)
		if !ok {
			continue
		}
		s.mu.RLock()
		c := s.clients[id]
		s.mu.RUnlock()
		if c == nil {
			continue
		}
		ctr, payload, err := proto.OpenUDP(c.udpAEAD, pkt, proto.DirClientToServer)
		if err != nil || !c.udpWin.Check(ctr) {
			continue
		}
		c.udpAddr.Store(addr)
		c.udpLast.Store(time.Now().UnixNano())
		switch typ {
		case proto.UDPHello:
			s.udp.WriteToUDP(proto.SealUDP(c.udpAEAD, proto.UDPHelloAck, c.ID, proto.DirServerToClient, c.udpCtr.Add(1), nil), addr)
		case proto.UDPPing:
			s.udp.WriteToUDP(proto.SealUDP(c.udpAEAD, proto.UDPPong, c.ID, proto.DirServerToClient, c.udpCtr.Add(1), payload), addr)
		case proto.UDPVoice:
			if c.allowVoice() {
				s.routeVoice(c, payload)
			}
		}
	}
}
