//go:build windows

// Package hotkey reads global key/mouse-button state for push-to-talk. It works
// while the app is in the background, without installing hooks.
package hotkey

import (
	"context"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	pGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
	pMapVirtualKeyW   = user32.NewProc("MapVirtualKeyW")
	pGetKeyNameTextW  = user32.NewProc("GetKeyNameTextW")
)

func Supported() bool { return true }

func Info() string {
	return "Global hotkeys: keyboard keys and mouse buttons 3-5, also while the window is in the background."
}

func Pressed(vk int) bool {
	if vk <= 0 || vk > 254 {
		return false
	}
	r, _, _ := pGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

// skip: left/right mouse buttons and the generic (non-sided) modifier codes,
// which duplicate VK_LSHIFT etc.
func skip(vk int) bool {
	switch vk {
	case 0x01, 0x02, 0x10, 0x11, 0x12:
		return true
	}
	return false
}

// Capture waits for the user to press a key or extra mouse button.
func Capture(ctx context.Context) (int, error) {
	// wait until everything is released
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		any := false
		for vk := 3; vk < 255; vk++ {
			if !skip(vk) && Pressed(vk) {
				any = true
				break
			}
		}
		if !any {
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
			for vk := 3; vk < 255; vk++ {
				if !skip(vk) && Pressed(vk) {
					return vk, nil
				}
			}
		}
	}
}

var special = map[int]string{
	0x04: "Middle Mouse", 0x05: "Mouse 4", 0x06: "Mouse 5",
	0xA0: "Left Shift", 0xA1: "Right Shift", 0xA2: "Left Ctrl", 0xA3: "Right Ctrl",
	0xA4: "Left Alt", 0xA5: "Right Alt", 0x5B: "Left Win", 0x5C: "Right Win",
	0x14: "Caps Lock", 0x20: "Space", 0x2D: "Insert", 0x2E: "Delete", 0x24: "Home",
	0x23: "End", 0x21: "Page Up", 0x22: "Page Down", 0x25: "Left", 0x26: "Up",
	0x27: "Right", 0x28: "Down", 0x90: "Num Lock", 0x91: "Scroll Lock", 0x13: "Pause",
}

func Name(vk int) string {
	if vk <= 0 {
		return "(none)"
	}
	if n, ok := special[vk]; ok {
		return n
	}
	if vk >= 0x70 && vk <= 0x87 {
		return fmt.Sprintf("F%d", vk-0x6F)
	}
	sc, _, _ := pMapVirtualKeyW.Call(uintptr(vk), 0)
	if sc != 0 {
		buf := make([]uint16, 64)
		n, _, _ := pGetKeyNameTextW.Call(sc<<16, uintptr(unsafe.Pointer(&buf[0])), 64)
		if n > 0 {
			return syscall.UTF16ToString(buf[:n])
		}
	}
	return fmt.Sprintf("Key 0x%02X", vk)
}
