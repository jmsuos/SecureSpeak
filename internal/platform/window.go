package platform

import (
	"os/exec"
	"sync"
)

var (
	winMu  sync.Mutex
	winCmd *exec.Cmd
)

func setWindow(cmd *exec.Cmd) {
	winMu.Lock()
	winCmd = cmd
	winMu.Unlock()
}

// CloseWindow ends the app window's browser process, if SecureSpeak started
// one (used when quitting while the window did not close itself).
func CloseWindow() {
	winMu.Lock()
	cmd := winCmd
	winCmd = nil
	winMu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}
