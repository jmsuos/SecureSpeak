package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"securespeak/internal/e2e"
	"securespeak/internal/platform"
)

type Bookmark struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Nickname       string `json:"nickname,omitempty"`
	ServerPassword string `json:"serverPassword,omitempty"`
}

type PeerRecord struct {
	LastNick string `json:"lastNick"`
	Verified bool   `json:"verified"`
}

// AudioSettings are applied by the UI's audio engine (capture, Opus,
// noise suppression, playback). They are stored here so they persist.
type AudioSettings struct {
	InputDevice      string  `json:"inputDevice"`  // browser deviceId, "" = default
	OutputDevice     string  `json:"outputDevice"` // browser sinkId, "" = default
	Mode             string  `json:"mode"`         // ptt | va | continuous
	VAThreshold      float64 `json:"vaThreshold"`  // dBFS
	InputGain        float64 `json:"inputGain"`    // linear
	OutputVolume     float64 `json:"outputVolume"`
	PTTKey           int     `json:"pttKey"`
	PTTReleaseMs     int     `json:"pttReleaseMs"`
	Sounds           bool    `json:"sounds"`
	SoundVolume      float64 `json:"soundVolume"`
	NoiseSuppression bool    `json:"noiseSuppression"`
	EchoCancellation bool    `json:"echoCancellation"`
	AutoGain         bool    `json:"autoGain"`
	VoiceIsolation   bool    `json:"voiceIsolation"`
	Bitrate          int     `json:"bitrate"` // Opus kbit/s
}

// WhisperList is a named set of whisper targets activated by a hotkey.
// Users are remembered by identity fingerprint, channels by name.
type WhisperList struct {
	Name     string   `json:"name"`
	Key      int      `json:"key"`
	Users    []string `json:"users"`
	Channels []string `json:"channels"`
}

type Config struct {
	Nickname     string                `json:"nickname"`
	LastHost     string                `json:"lastHost"`
	LastPort     int                   `json:"lastPort"`
	Bookmarks    []Bookmark            `json:"bookmarks"`
	KnownServers map[string]string     `json:"knownServers"` // host:port -> cert fingerprint
	KnownPeers   map[string]PeerRecord `json:"knownPeers"`   // identity fingerprint -> record
	NickBindings map[string]string     `json:"nickBindings"` // host:port|nick -> identity fingerprint
	PeerVolumes  map[string]float64    `json:"peerVolumes"`  // identity fingerprint -> volume
	PeerMuted    map[string]bool       `json:"peerMuted"`
	Audio        AudioSettings         `json:"audio"`
	Whisper      []WhisperList         `json:"whisper"`
	DownloadDir  string                `json:"downloadDir"`
	NoAutoMedia  bool                  `json:"noAutoMedia"`  // do not auto-load image/video previews
	NoAutoUpdate bool                  `json:"noAutoUpdate"` // do not check GitHub for new versions automatically

	LeaveMessage string              `json:"leaveMessage,omitempty"` // shown to others when we disconnect
	JoinSound    *SoundRef           `json:"joinSound,omitempty"`    // others hear this when we connect
	LeaveSound   *SoundRef           `json:"leaveSound,omitempty"`   // ... and this when we leave
	AppSounds    map[string]SoundRef `json:"appSounds,omitempty"`    // replaced notification sounds
	NoPeerSounds bool                `json:"noPeerSounds,omitempty"` // don't play other people's join/leave sounds
}

func defaultConfig() *Config {
	return &Config{
		LastPort:     9987,
		KnownServers: map[string]string{},
		KnownPeers:   map[string]PeerRecord{},
		NickBindings: map[string]string{},
		PeerVolumes:  map[string]float64{},
		PeerMuted:    map[string]bool{},
		Audio: AudioSettings{
			Mode: "va", VAThreshold: -45, InputGain: 1, OutputVolume: 1, PTTReleaseMs: 150,
			Sounds: true, SoundVolume: 0.5, NoiseSuppression: true, EchoCancellation: true,
			AutoGain: true, Bitrate: 48,
		},
	}
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) LoadConfig() *Config {
	c := defaultConfig()
	b, err := os.ReadFile(filepath.Join(s.dir, "config.json"))
	if err == nil {
		json.Unmarshal(b, c)
	}
	if c.KnownServers == nil {
		c.KnownServers = map[string]string{}
	}
	if c.KnownPeers == nil {
		c.KnownPeers = map[string]PeerRecord{}
	}
	if c.NickBindings == nil {
		c.NickBindings = map[string]string{}
	}
	if c.PeerVolumes == nil {
		c.PeerVolumes = map[string]float64{}
	}
	if c.Audio.Bitrate < 12 || c.Audio.Bitrate > 128 {
		c.Audio.Bitrate = 48
	}
	if c.Audio.Mode == "" {
		c.Audio.Mode = "va"
	}
	if c.PeerMuted == nil {
		c.PeerMuted = map[string]bool{}
	}
	return c
}

func (s *Store) SaveConfig(c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return s.WriteConfig(b)
}

func (s *Store) WriteConfig(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := filepath.Join(s.dir, "config.json")
	if err := os.WriteFile(p+".tmp", b, 0600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// LoadIdentity loads the local identity key, creating one on first run. On
// Windows the key is encrypted with DPAPI so other users cannot read it.
func (s *Store) LoadIdentity() (*e2e.Identity, error) {
	p := filepath.Join(s.dir, "identity.key")
	b, err := os.ReadFile(p)
	if err == nil {
		seed, err := platform.Unprotect(b)
		if err != nil {
			return nil, err
		}
		return e2e.IdentityFromSeed(seed)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s.NewIdentity()
}

func (s *Store) NewIdentity() (*e2e.Identity, error) {
	id := e2e.NewIdentity()
	enc, err := platform.Protect(id.Seed())
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(s.dir, "identity.key"), enc, 0600); err != nil {
		return nil, err
	}
	return id, nil
}

// Avatar storage (our own picture).
func (s *Store) LoadAvatar() []byte {
	b, _ := os.ReadFile(filepath.Join(s.dir, "avatar.img"))
	return b
}

func (s *Store) SaveAvatar(b []byte) error {
	p := filepath.Join(s.dir, "avatar.img")
	if len(b) == 0 {
		err := os.Remove(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.WriteFile(p, b, 0600)
}

// OutboxDir holds copies of files we offered, until we disconnect.
func (s *Store) OutboxDir() string { return filepath.Join(s.dir, "outbox") }

// CacheDir holds automatically loaded media previews (cleared on start).
func (s *Store) CacheDir() string { return filepath.Join(s.dir, "cache") }
