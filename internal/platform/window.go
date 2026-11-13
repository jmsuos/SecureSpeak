package platform

import (
	"os/exec"
	"sync"
)

var (
	winMu  sync.Mutex
	winCmd *exec.Cmd

	appExe, appProfile string // the browser and profile of the app window
	appFirefox         bool
)

func setAppBrowser(exe, profile string, firefox bool) {
	winMu.Lock()
	appExe, appProfile, appFirefox = exe, profile, firefox
	winMu.Unlock()
}

// OpenInAppBrowser opens a web page in the browser profile of the app window,
// so a sign-in there (e.g. YouTube Premium) also applies inside SecureSpeak.
// Without an app window it falls back to the default browser.
func OpenInAppBrowser(url string) error {
	winMu.Lock()
	exe, profile, ff := appExe, appProfile, appFirefox
	winMu.Unlock()
	if exe == "" {
		return OpenURL(url)
	}
	if ff {
		return exec.Command(exe, "--profile", profile, url).Start()
	}
	return exec.Command(exe, "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check", url).Start()
}

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
