//go:build !windows && !linux

package hotkey

import (
	"context"
	"errors"
	"fmt"
)

func Supported() bool     { return false }
func Info() string        { return "Global hotkeys are not supported on this platform." }
func Pressed(vk int) bool { return false }

func Capture(ctx context.Context) (int, error) {
	return 0, errors.New("global hotkeys are not supported on this platform")
}

func Name(vk int) string {
	if vk <= 0 {
		return "(none)"
	}
	return fmt.Sprintf("Key %d", vk)
}
