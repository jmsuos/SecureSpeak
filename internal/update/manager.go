package update

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State is what the UI shows in "About SecureSpeak".
type State struct {
	Current     string  `json:"current"`
	Status      string  `json:"status"` // idle checking uptodate available downloading installing restarting error dev
	Latest      string  `json:"latest,omitempty"`
	Notes       string  `json:"notes,omitempty"`
	URL         string  `json:"url"` // release page (or releases list)
	Published   string  `json:"published,omitempty"`
	Progress    float64 `json:"progress"` // 0..1 while downloading
	Error       string  `json:"error,omitempty"`
	Kind        string  `json:"kind"`
	Arch        string  `json:"arch"`
	Signed      bool    `json:"signed"`     // updates must carry a valid release signature
	CanInstall  bool    `json:"canInstall"` // an update for this install type is available
	Reason      string  `json:"reason,omitempty"`
	Auto        bool    `json:"auto"`
	Checked     int64   `json:"checked,omitempty"` // unix ms of the last successful check
	JustUpdated bool    `json:"justUpdated,omitempty"`
}

// Manager runs checks and installs and reports State changes.
type Manager struct {
	U       *Updater
	Emit    func(State)
	Auto    func() bool   // automatic checks enabled?
	Restart chan *Pending // receives a verified update; the app must shut down and call Finish
	Every   time.Duration // automatic check interval

	mu   sync.Mutex
	st   State
	rel  *Release
	busy bool
}

func NewManager(u *Updater, emit func(State), auto func() bool) *Manager {
	m := &Manager{U: u, Emit: emit, Auto: auto, Restart: make(chan *Pending, 1), Every: 6 * time.Hour}
	m.st = State{Current: u.Current, Status: "idle", URL: "https://github.com/" + Repo + "/releases", Kind: u.Kind.String(), Arch: u.Arch, Signed: len(u.Keys) > 0, Reason: u.Reason}
	if u.IsDev() {
		m.st.Status = "dev"
	}
	return m
}

// State returns the current state.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.st
	st.Auto = m.Auto()
	return st
}

func (m *Manager) set(f func(*State)) {
	m.mu.Lock()
	f(&m.st)
	m.mu.Unlock()
	m.Emit(m.State())
}

// MarkUpdated records that this run follows an update (the UI says so once).
func (m *Manager) MarkUpdated() { m.set(func(s *State) { s.JustUpdated = true }) }

// Publish emits the current state (e.g. after the auto setting changed).
func (m *Manager) Publish() { m.Emit(m.State()) }

// Check looks for a new release. It returns at once; results arrive via Emit.
func (m *Manager) Check() {
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return
	}
	m.busy = true
	m.mu.Unlock()
	go func() {
		final := m.check()
		// Not busy any more before the result is announced, so a check
		// requested right after seeing the result is not ignored.
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
		m.set(final)
	}()
}

// check returns the final state change; the caller applies it.
func (m *Manager) check() func(*State) {
	m.set(func(s *State) { s.Status, s.Error, s.Progress = "checking", "", 0 })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	r, err := m.U.Latest(ctx)
	if d := 700*time.Millisecond - time.Since(start); d > 0 {
		time.Sleep(d) // let the spinner be seen; an instant flip looks broken
	}
	if err != nil {
		return func(s *State) { s.Status, s.Error = "error", "Could not check for updates: "+err.Error() }
	}
	m.mu.Lock()
	m.rel = r
	m.mu.Unlock()
	return func(s *State) {
		s.Latest, s.Notes, s.Published, s.URL = r.Version(), r.Notes, r.Published, r.URL
		s.Checked = time.Now().UnixMilli()
		s.CanInstall = false
		switch {
		case m.U.IsDev():
			s.Status = "dev"
		case !m.U.Newer(r):
			s.Status = "uptodate"
		default:
			s.Status = "available"
			s.CanInstall = m.U.Kind != Manual && m.U.Asset(r) != nil
			if m.U.Kind != Manual && m.U.Asset(r) == nil {
				s.Reason = "This release has no download for your system yet."
			}
		}
	}
}

// Install downloads, verifies and installs the latest release, then hands the
// pending update to Restart. It returns at once.
func (m *Manager) Install() error {
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return errors.New("busy")
	}
	r := m.rel
	if r == nil || m.st.Status != "available" && m.st.Status != "error" || !m.U.Newer(r) || m.U.Kind == Manual {
		m.mu.Unlock()
		return errors.New("no update to install")
	}
	m.busy = true
	m.mu.Unlock()
	go func() {
		m.set(func(s *State) { s.Status, s.Error, s.Progress = "downloading", "", 0 })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		p, err := m.U.Prepare(ctx, r, func(done, total int64) {
			if total > 0 {
				f := float64(done) / float64(total)
				m.set(func(s *State) {
					s.Progress = f
					if f >= 1 {
						s.Status = "installing"
					}
				})
			}
		})
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
		if err != nil {
			m.set(func(s *State) { s.Status, s.Error = "error", "Update failed: "+err.Error() })
			return
		}
		m.set(func(s *State) { s.Status, s.Progress = "restarting", 1 })
		m.Restart <- p
	}()
	return nil
}

// Run performs automatic checks until ctx ends.
func (m *Manager) Run(ctx context.Context, firstDelay time.Duration) {
	t := time.NewTimer(firstDelay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if m.Auto() && !m.U.IsDev() {
			m.Check()
		}
		t.Reset(m.Every)
	}
}
