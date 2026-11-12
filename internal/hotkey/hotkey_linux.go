//go:build linux

// Package hotkey reads global key state for push-to-talk and whisper keys.
//
// Linux backends, tried in order:
//
//  1. evdev (/dev/input/event*): truly global, works on X11 and Wayland, needs
//     read access to input devices (user in the "input" group).
//  2. X11 QueryKeymap over the X socket (pure Go, no libX11): global on X11
//     sessions such as Linux Mint Cinnamon; on Wayland it only sees keys while
//     an X11/XWayland window has focus.
//
// Key codes are Linux input codes (KEY_*, BTN_*); X11 keycodes are code+8.
package hotkey

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type backend interface {
	pressed(code int) bool
	snapshot() map[int]bool
}

var (
	once    sync.Once
	active  backend
	info    = "Global hotkeys unavailable."
	initErr error
)

func start() {
	once.Do(func() {
		if b := startEvdev(); b != nil {
			active = b
			info = "Global hotkeys via input devices (evdev): work everywhere, including Wayland."
			return
		}
		if b, err := startX11(); err == nil {
			active = b
			info = "Global hotkeys via X11. On a Wayland session they only work while SecureSpeak is focused; " +
				"for system-wide push-to-talk run: sudo usermod -aG input $USER (then log out and in)."
			return
		} else {
			initErr = err
		}
		info = "No global hotkeys: not in the 'input' group and no X11 display. Run: sudo usermod -aG input $USER " +
			"(then log out and in), or use the push-to-talk button in the window."
	})
}

func Supported() bool { start(); return active != nil }
func Info() string    { start(); return info }

func Pressed(code int) bool {
	if code <= 0 {
		return false
	}
	start()
	return active != nil && active.pressed(code)
}

// Capture waits for a key or extra mouse button to be pressed.
func Capture(ctx context.Context) (int, error) {
	start()
	if active == nil {
		return 0, errors.New("no hotkey backend available")
	}
	skip := func(c int) bool { return c == 0x110 || c == 0x111 } // left/right mouse
	// wait for release of everything currently held
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		held := false
		for c, d := range active.snapshot() {
			if d && !skip(c) {
				held = true
			}
		}
		if !held {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-t.C:
			for c, d := range active.snapshot() {
				if d && !skip(c) {
					return c, nil
				}
			}
		}
	}
}

// ---- evdev ----------------------------------------------------------------------

type evdev struct {
	mu    sync.Mutex
	state map[string]map[int]bool // device -> code -> down
}

func startEvdev() backend {
	paths, _ := filepath.Glob("/dev/input/event*")
	e := &evdev{state: map[string]map[int]bool{}}
	n := 0
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		n++
		e.state[p] = map[int]bool{}
		go e.read(p, f)
	}
	if n == 0 {
		return nil
	}
	return e
}

func (e *evdev) read(path string, f *os.File) {
	defer f.Close()
	// struct input_event on 64-bit: timeval (16) + type u16 + code u16 + value s32
	const size = 24
	buf := make([]byte, size*64)
	for {
		n, err := f.Read(buf)
		if err != nil {
			return
		}
		e.mu.Lock()
		for i := 0; i+size <= n; i += size {
			ev := buf[i : i+size]
			typ := binary.LittleEndian.Uint16(ev[16:])
			code := int(binary.LittleEndian.Uint16(ev[18:]))
			val := int32(binary.LittleEndian.Uint32(ev[20:]))
			if typ == 1 { // EV_KEY
				e.state[path][code] = val != 0
			}
		}
		e.mu.Unlock()
	}
}

func (e *evdev) pressed(code int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, st := range e.state {
		if st[code] {
			return true
		}
	}
	return false
}

func (e *evdev) snapshot() map[int]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[int]bool{}
	for _, st := range e.state {
		for c, d := range st {
			if d {
				out[c] = true
			}
		}
	}
	return out
}

// ---- X11 --------------------------------------------------------------------------

type x11 struct {
	mu   sync.Mutex
	keys [32]byte
}

func xauthCookie(display string) ([]byte, error) {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".Xauthority")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil // no auth file: try without
	}
	rd := func() ([]byte, bool) {
		if len(b) < 2 {
			return nil, false
		}
		n := int(binary.BigEndian.Uint16(b))
		if len(b) < 2+n {
			return nil, false
		}
		v := b[2 : 2+n]
		b = b[2+n:]
		return v, true
	}
	var fallback []byte
	for len(b) >= 2 {
		b = b[2:] // family
		_, ok1 := rd()
		num, ok2 := rd()
		name, ok3 := rd()
		data, ok4 := rd()
		if !(ok1 && ok2 && ok3 && ok4) {
			break
		}
		if string(name) != "MIT-MAGIC-COOKIE-1" {
			continue
		}
		if string(num) == display {
			return data, nil
		}
		if fallback == nil && len(num) == 0 {
			fallback = data
		}
	}
	return fallback, nil
}

func startX11() (backend, error) {
	d := os.Getenv("DISPLAY")
	if d == "" {
		return nil, errors.New("DISPLAY not set")
	}
	i := strings.LastIndex(d, ":")
	if i < 0 || (i > 0 && d[:i] != "unix" && d[:i] != "localhost") {
		return nil, errors.New("only local X displays are supported")
	}
	num := d[i+1:]
	if j := strings.Index(num, "."); j >= 0 {
		num = num[:j]
	}
	if _, err := strconv.Atoi(num); err != nil {
		return nil, errors.New("bad DISPLAY")
	}
	conn, err := net.Dial("unix", "/tmp/.X11-unix/X"+num)
	if err != nil {
		if conn, err = net.Dial("unix", "@/tmp/.X11-unix/X"+num); err != nil {
			return nil, err
		}
	}
	cookie, _ := xauthCookie(num)
	pad := func(n int) int { return (4 - n%4) % 4 }
	name := "MIT-MAGIC-COOKIE-1"
	if cookie == nil {
		name = ""
	}
	req := []byte{'l', 0, 11, 0, 0, 0}
	req = binary.LittleEndian.AppendUint16(req, uint16(len(name)))
	req = binary.LittleEndian.AppendUint16(req, uint16(len(cookie)))
	req = append(req, 0, 0)
	req = append(req, name...)
	req = append(req, make([]byte, pad(len(name)))...)
	req = append(req, cookie...)
	req = append(req, make([]byte, pad(len(cookie)))...)
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		conn.Close()
		return nil, err
	}
	extra := make([]byte, int(binary.LittleEndian.Uint16(hdr[6:]))*4)
	if _, err := io.ReadFull(conn, extra); err != nil {
		conn.Close()
		return nil, err
	}
	if hdr[0] != 1 {
		conn.Close()
		return nil, errors.New("X server refused the connection")
	}
	conn.SetDeadline(time.Time{})
	x := &x11{}
	go x.poll(conn)
	return x, nil
}

func (x *x11) poll(conn net.Conn) {
	defer conn.Close()
	req := []byte{44, 0, 1, 0} // QueryKeymap
	reply := make([]byte, 40)
	for {
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write(req); err != nil {
			return
		}
		for {
			if _, err := io.ReadFull(conn, reply[:32]); err != nil {
				return
			}
			if reply[0] == 1 { // reply: 32 header bytes + 8 more
				if _, err := io.ReadFull(conn, reply[32:40]); err != nil {
					return
				}
				break
			}
			// error or event (32 bytes): ignore
		}
		x.mu.Lock()
		copy(x.keys[:], reply[8:40])
		x.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
}

func (x *x11) pressed(code int) bool {
	kc := code + 8
	if kc < 8 || kc > 255 {
		return false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.keys[kc/8]&(1<<(kc%8)) != 0
}

func (x *x11) snapshot() map[int]bool {
	out := map[int]bool{}
	x.mu.Lock()
	defer x.mu.Unlock()
	for kc := 8; kc < 256; kc++ {
		if x.keys[kc/8]&(1<<(kc%8)) != 0 {
			out[kc-8] = true
		}
	}
	return out
}

// ---- names ------------------------------------------------------------------------

var names = map[int]string{
	1: "Esc", 12: "-", 13: "=", 14: "Backspace", 15: "Tab", 26: "[", 27: "]", 28: "Enter",
	29: "Left Ctrl", 39: ";", 40: "'", 41: "`", 42: "Left Shift", 43: "\\", 51: ",", 52: ".",
	53: "/", 54: "Right Shift", 55: "Num *", 56: "Left Alt", 57: "Space", 58: "Caps Lock",
	69: "Num Lock", 70: "Scroll Lock", 71: "Num 7", 72: "Num 8", 73: "Num 9", 74: "Num -",
	75: "Num 4", 76: "Num 5", 77: "Num 6", 78: "Num +", 79: "Num 1", 80: "Num 2", 81: "Num 3",
	82: "Num 0", 83: "Num .", 87: "F11", 88: "F12", 96: "Num Enter", 97: "Right Ctrl",
	98: "Num /", 99: "Print", 100: "Right Alt", 102: "Home", 103: "Up", 104: "Page Up",
	105: "Left", 106: "Right", 107: "End", 108: "Down", 109: "Page Down", 110: "Insert",
	111: "Delete", 119: "Pause", 125: "Left Super", 126: "Right Super", 127: "Menu",
	0x112: "Middle Mouse", 0x113: "Mouse 4", 0x114: "Mouse 5", 0x115: "Mouse Forward", 0x116: "Mouse Back",
}

func Name(code int) string {
	if code <= 0 {
		return "(none)"
	}
	if n, ok := names[code]; ok {
		return n
	}
	rows := []struct {
		start int
		keys  string
	}{{2, "1234567890"}, {16, "QWERTYUIOP"}, {30, "ASDFGHJKL"}, {44, "ZXCVBNM"}}
	for _, r := range rows {
		if code >= r.start && code < r.start+len(r.keys) {
			return string(r.keys[code-r.start])
		}
	}
	if code >= 59 && code <= 68 {
		return fmt.Sprintf("F%d", code-58)
	}
	if code >= 183 && code <= 194 {
		return fmt.Sprintf("F%d", code-170)
	}
	return fmt.Sprintf("Key %d", code)
}
