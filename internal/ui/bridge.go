// Package ui serves the client interface on 127.0.0.1 and bridges it to the
// client core: commands via POST /api, events via Server-Sent Events.
package ui

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

type Handler func(cmd string, args json.RawMessage) (any, error)

// VoiceIn receives encoded frames from the UI audio engine.
type VoiceIn func(mode, codec, flags byte, payload []byte)

// Uploader stores an uploaded file and offers it in the chat.
type Uploader func(kind string, target uint32, name, mime string, body io.Reader) error

// VideoIn receives encoded screen share frames from the UI.
type VideoIn func(flags, codec byte, payload []byte)

// MediaSource resolves a file id to a local path for previews.
type MediaSource func(id string) (string, bool)

// AvatarSource returns the picture of a user.
type AvatarSource func(id uint32) []byte

type Bridge struct {
	Token   string
	handler Handler
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	last    map[string][]byte // last event of sticky types (state)
	host    string
	everSub bool
	idleAt  time.Time

	VoiceIn  VoiceIn
	VideoIn  VideoIn
	Media    MediaSource
	Upload   Uploader
	Avatar   AvatarSource
	AvatarCT func([]byte) string
	audioMu  sync.Mutex
	audio    map[*wsConn]chan []byte
}

// SendVoice delivers a frame to the UI audio engine (non-blocking).
func (b *Bridge) SendVoice(frame []byte) {
	b.audioMu.Lock()
	defer b.audioMu.Unlock()
	for _, ch := range b.audio {
		select {
		case ch <- frame:
		default:
		}
	}
}

// Idle reports how long no UI has been attached (0 if one is attached or none
// ever was).
func (b *Bridge) Idle() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.everSub || len(b.subs) > 0 {
		return 0
	}
	return time.Since(b.idleAt)
}

func NewBridge() *Bridge {
	b := make([]byte, 24)
	rand.Read(b)
	return &Bridge{Token: hex.EncodeToString(b), subs: map[chan []byte]struct{}{}, last: map[string][]byte{}, audio: map[*wsConn]chan []byte{}}
}

func (b *Bridge) SetHandler(h Handler) { b.handler = h }

func (b *Bridge) Emit(t string, v any) {
	msg, err := json.Marshal(map[string]any{"t": t, "d": v})
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if t == "state" || t == "stats" || t == "update" {
		b.last[t] = msg
	}
	for ch := range b.subs {
		select {
		case ch <- msg:
		default: // slow subscriber; drop
		}
	}
}

// Listen starts the HTTP server on a random localhost port and returns the URL.
func (b *Bridge) Listen() (string, error) {
	// A fixed port keeps the browser origin stable, so the microphone
	// permission is remembered between runs.
	ln, err := net.Listen("tcp", "127.0.0.1:47713")
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return "", err
		}
	}
	b.host = ln.Addr().String()
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' ws://"+b.host+"; media-src 'self' blob:")
		w.Header().Set("X-Frame-Options", "DENY")
		static.ServeHTTP(w, r)
	})
	mux.HandleFunc("/events", b.events)
	mux.HandleFunc("/api", b.api)
	mux.HandleFunc("/audio", b.audioWS)
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		if !b.authorized(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Write([]byte("securespeak"))
	})
	mux.HandleFunc("/upload", b.upload)
	mux.HandleFunc("/avatar", b.avatar)
	mux.HandleFunc("/media", b.media)
	srv := &http.Server{Handler: b.guard(mux), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	return "http://" + b.host + "/?token=" + b.Token, nil
}

// guard rejects requests with a foreign Host header (DNS rebinding protection).
func (b *Bridge) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != b.host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (b *Bridge) authorized(r *http.Request) bool {
	t := r.Header.Get("X-Token")
	if t == "" {
		t = r.URL.Query().Get("token")
	}
	return len(t) == len(b.Token) && t == b.Token
}

func (b *Bridge) events(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch := make(chan []byte, 512)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.everSub = true
	initial := [][]byte{}
	for _, m := range b.last {
		initial = append(initial, m)
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.idleAt = time.Now()
		b.mu.Unlock()
	}()
	for _, m := range initial {
		w.Write([]byte("data: "))
		w.Write(m)
		w.Write([]byte("\n\n"))
	}
	fl.Flush()
	keep := time.NewTicker(20 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-ch:
			w.Write([]byte("data: "))
			w.Write(m)
			w.Write([]byte("\n\n"))
			for n := len(ch); n > 0; n-- { // batch
				m = <-ch
				w.Write([]byte("data: "))
				w.Write(m)
				w.Write([]byte("\n\n"))
			}
			fl.Flush()
		case <-keep.C:
			w.Write([]byte(": keepalive\n\n"))
			fl.Flush()
		}
	}
}

func (b *Bridge) api(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !b.authorized(r) || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return
	}
	var req struct {
		Cmd  string          `json:"cmd"`
		Args json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	res, err := b.handler(req.Cmd, req.Args)
	out := map[string]any{"ok": err == nil, "result": res}
	if err != nil {
		out["error"] = err.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// audioWS carries binary audio frames in both directions:
//
//	UI -> core: [1][mode][codec][flags][payload]
//	core -> UI: [1][sender u32][flags][codec][seq u32][payload]
func (b *Bridge) audioWS(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) || r.Header.Get("Origin") != "http://"+b.host {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ws, err := wsAccept(w, r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer ws.c.Close()
	out := make(chan []byte, 256)
	b.audioMu.Lock()
	b.audio[ws] = out
	b.audioMu.Unlock()
	done := make(chan struct{})
	defer func() {
		b.audioMu.Lock()
		delete(b.audio, ws)
		b.audioMu.Unlock()
		close(done)
	}()
	go func() {
		for {
			select {
			case f := <-out:
				if ws.write(0x2, f) != nil {
					ws.c.Close()
					return
				}
			case <-done:
				return
			}
		}
	}()
	for {
		msg, err := ws.read()
		if err != nil {
			return
		}
		if len(msg) >= 5 && msg[0] == 1 && b.VoiceIn != nil {
			b.VoiceIn(msg[1], msg[2], msg[3], msg[4:])
		}
		if len(msg) >= 4 && msg[0] == 2 && b.VideoIn != nil {
			b.VideoIn(msg[1], msg[2], msg[3:])
		}
	}
}

// upload receives a file to offer: POST /upload?kind=channel|private&target=ID&name=...
func (b *Bridge) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !b.authorized(r) || b.Upload == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	target, _ := strconv.ParseUint(q.Get("target"), 10, 32)
	err := b.Upload(q.Get("kind"), uint32(target), q.Get("name"), q.Get("mime"), r.Body)
	w.Header().Set("Content-Type", "application/json")
	out := map[string]any{"ok": err == nil}
	if err != nil {
		out["error"] = err.Error()
	}
	json.NewEncoder(w).Encode(out)
}

// avatar serves a user's picture: GET /avatar?id=ID&token=...
func (b *Bridge) avatar(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) || b.Avatar == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id, _ := strconv.ParseUint(r.URL.Query().Get("id"), 10, 32)
	img := b.Avatar(uint32(id))
	if img == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", b.AvatarCT(img))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(img)
}

// previewTypes are the only content types served for previews. Anything
// else (HTML, SVG, scripts...) is refused, so a malicious file can never run
// in the app's origin.
var previewTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/bmp": true,
	"video/mp4": true, "video/webm": true,
}

// media serves a received or sent file for inline preview: GET /media?id=...
func (b *Bridge) media(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) || b.Media == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	path, ok := b.Media(r.URL.Query().Get("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	ct := http.DetectContentType(head[:n])
	if !previewTypes[ct] {
		http.Error(w, "no preview for this file type", http.StatusUnsupportedMediaType)
		return
	}
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "", st.ModTime(), f) // range requests: video seeking
}
