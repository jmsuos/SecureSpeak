//go:build darwin && cgo

package hotkey

// Global push-to-talk on macOS: polls the key and mouse button state with
// CoreGraphics (CGEventSourceKeyState / CGEventSourceButtonState). Reading
// keys while another app is in front needs the "Input Monitoring"
// permission (System Settings > Privacy & Security > Input Monitoring).
//
// Codes: 1-128 are macOS virtual key codes (kVK_*) plus one (0 means "no
// key" everywhere in SecureSpeak, and kVK 0 is the A key); 1000+n are mouse
// buttons (1002 = middle, 1003 and 1004 = the side buttons).

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include <CoreGraphics/CoreGraphics.h>

static int ssKeyDown(int code) {
	return CGEventSourceKeyState(kCGEventSourceStateCombinedSessionState, (CGKeyCode)code) ? 1 : 0;
}
static int ssButtonDown(int button) {
	return CGEventSourceButtonState(kCGEventSourceStateCombinedSessionState, (CGMouseButton)button) ? 1 : 0;
}
static int ssCanListen(void) {
	if (__builtin_available(macOS 10.15, *)) { return CGPreflightListenEventAccess() ? 1 : 0; }
	return 1;
}
static void ssRequestListen(void) {
	if (__builtin_available(macOS 10.15, *)) { CGRequestListenEventAccess(); }
}
*/
import "C"

import (
	"context"
	"fmt"
	"time"
)

const mouseBase = 1000

func Supported() bool { return true }

func Info() string {
	if C.ssCanListen() == 0 {
		return "To use push-to-talk while another app is in front, allow SecureSpeak under System Settings → Privacy & Security → Input Monitoring (you are asked when you set a key)."
	}
	return "Hotkeys work in every app (Input Monitoring is allowed)."
}

func Pressed(code int) bool {
	switch {
	case code >= 1 && code <= 128:
		return C.ssKeyDown(C.int(code-1)) != 0
	case code >= mouseBase+2 && code < mouseBase+32:
		return C.ssButtonDown(C.int(code-mouseBase)) != 0
	}
	return false
}

// Capture waits for the next key or mouse button (left/right clicks are
// ignored) and returns its code.
func Capture(ctx context.Context) (int, error) {
	if C.ssCanListen() == 0 {
		C.ssRequestListen() // shows the system permission prompt once
	}
	held := map[int]bool{}
	all := func() []int {
		codes := make([]int, 0, 160)
		for k := 1; k <= 128; k++ {
			codes = append(codes, k)
		}
		for b := 2; b < 8; b++ {
			codes = append(codes, mouseBase+b)
		}
		return codes
	}()
	for _, c := range all { // ignore keys that are already down (e.g. the click on "Set key")
		held[c] = Pressed(c)
	}
	t := time.NewTicker(15 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-t.C:
		}
		for _, c := range all {
			down := Pressed(c)
			if down && !held[c] {
				return c, nil
			}
			held[c] = down
		}
	}
}

var names = map[int]string{
	0: "A", 1: "S", 2: "D", 3: "F", 4: "H", 5: "G", 6: "Z", 7: "X", 8: "C", 9: "V", 11: "B", 12: "Q", 13: "W",
	14: "E", 15: "R", 16: "Y", 17: "T", 18: "1", 19: "2", 20: "3", 21: "4", 22: "6", 23: "5", 24: "=", 25: "9",
	26: "7", 27: "-", 28: "8", 29: "0", 30: "]", 31: "O", 32: "U", 33: "[", 34: "I", 35: "P", 36: "Return",
	37: "L", 38: "J", 39: "'", 40: "K", 41: ";", 42: "\\", 43: ",", 44: "/", 45: "N", 46: "M", 47: ".",
	48: "Tab", 49: "Space", 50: "`", 51: "Delete", 53: "Esc", 54: "Right Command", 55: "Command", 56: "Shift",
	57: "Caps Lock", 58: "Option", 59: "Control", 60: "Right Shift", 61: "Right Option", 62: "Right Control",
	63: "Fn", 64: "F17", 65: "Keypad .", 67: "Keypad *", 69: "Keypad +", 71: "Keypad Clear", 72: "Volume Up",
	73: "Volume Down", 74: "Mute", 75: "Keypad /", 76: "Keypad Enter", 78: "Keypad -", 79: "F18", 80: "F19",
	81: "Keypad =", 82: "Keypad 0", 83: "Keypad 1", 84: "Keypad 2", 85: "Keypad 3", 86: "Keypad 4",
	87: "Keypad 5", 88: "Keypad 6", 89: "Keypad 7", 90: "F20", 91: "Keypad 8", 92: "Keypad 9", 96: "F5",
	97: "F6", 98: "F7", 99: "F3", 100: "F8", 101: "F9", 103: "F11", 105: "F13", 106: "F16", 107: "F14",
	109: "F10", 111: "F12", 113: "F15", 114: "Help", 115: "Home", 116: "Page Up", 117: "Forward Delete",
	118: "F4", 119: "End", 120: "F2", 121: "Page Down", 122: "F1", 123: "Left", 124: "Right", 125: "Down", 126: "Up",
	mouseBase + 2: "Middle mouse button", mouseBase + 3: "Mouse button 4", mouseBase + 4: "Mouse button 5",
}

func Name(code int) string {
	switch {
	case code <= 0:
		return "(none)"
	case code <= 128:
		if n, ok := names[code-1]; ok {
			return n
		}
		return fmt.Sprintf("Key %d", code-1)
	}
	if n, ok := names[code]; ok {
		return n
	}
	return fmt.Sprintf("Mouse button %d", code-mouseBase+1)
}
