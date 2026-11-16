//go:build darwin

package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The identity key is kept in a file readable only by the user
// (~/Library/Application Support/SecureSpeak, protected by FileVault when
// it is on), like on Linux.
const ProtectsSecrets = false

func Protect(data []byte) ([]byte, error)   { return data, nil }
func Unprotect(data []byte) ([]byte, error) { return data, nil }

// macBrowsers are Chromium-based browsers with an app-window mode, in order
// of preference (relative to /Applications or ~/Applications).
var macBrowsers = []string{
	"Google Chrome.app/Contents/MacOS/Google Chrome",
	"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	"Brave Browser.app/Contents/MacOS/Brave Browser",
	"Vivaldi.app/Contents/MacOS/Vivaldi",
	"Chromium.app/Contents/MacOS/Chromium",
}

func findMacBrowser() string {
	home, _ := os.UserHomeDir()
	for _, rel := range macBrowsers {
		for _, root := range []string{"/Applications", filepath.Join(home, "Applications")} {
			p := filepath.Join(root, rel)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	return ""
}

// OpenWindow shows the UI in an app window of a Chromium-based browser
// (its own profile, no address bar). Without one, Safari (or the default
// browser) opens it as a normal tab.
func OpenWindow(url, profileDir string) (<-chan struct{}, error) {
	exe := findMacBrowser()
	if exe == "" {
		err := exec.Command("open", url).Start()
		return nil, errors.Join(errors.New("no browser with an app mode found (Chrome, Edge, Brave, Vivaldi, Chromium); opened the default browser"), err)
	}
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return nil, err
	}
	cmd := exec.Command(exe,
		"--app="+url,
		"--user-data-dir="+profileDir,
		"--window-size=1180,760",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--autoplay-policy=no-user-gesture-required",
	)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	setWindow(cmd)
	setAppBrowser(exe, profileDir, false)
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	return done, nil
}

// Fatal shows an error dialog (there is no terminal when started from Finder).
func Fatal(title, msg string) {
	q := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	exec.Command("osascript", "-e", "display alert "+q(title)+" message "+q(msg)+" as critical").Run()
}

func DownloadDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func OpenURL(u string) error    { return exec.Command("open", u).Start() }
func OpenPath(dir string) error { return exec.Command("open", dir).Start() }
func Reveal(path string) error  { return exec.Command("open", "-R", path).Start() }

// RefreshUserIcon is only needed on Linux.
func RefreshUserIcon(png []byte) {}
