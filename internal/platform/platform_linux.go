//go:build linux

// Package platform contains OS integration: secret storage, the UI window,
// opening links and folders.
package platform

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// On Linux the identity key is stored as a 0600 file in ~/.config/SecureSpeak,
// the same protection SSH and GnuPG keys get.
const ProtectsSecrets = false

func Protect(data []byte) ([]byte, error)   { return data, nil }
func Unprotect(data []byte) ([]byte, error) { return data, nil }

type browser struct {
	exe     string
	firefox bool
	snap    string // snap name if installed as a snap
}

func findBrowser() *browser {
	chromium := []string{
		"google-chrome-stable", "google-chrome", "chromium", "chromium-browser",
		"microsoft-edge-stable", "microsoft-edge", "brave-browser", "brave", "vivaldi-stable", "vivaldi",
	}
	for _, name := range chromium {
		if p, err := exec.LookPath(name); err == nil {
			return &browser{exe: p, snap: snapName(p)}
		}
	}
	if p, err := exec.LookPath("firefox"); err == nil {
		return &browser{exe: p, firefox: true, snap: snapName(p)}
	}
	return nil
}

func snapName(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if strings.HasPrefix(p, "/snap/") {
		parts := strings.Split(p, "/")
		if len(parts) > 2 {
			if parts[2] == "bin" && len(parts) > 3 {
				return parts[3]
			}
			return parts[2]
		}
	}
	if strings.HasSuffix(p, "/usr/bin/snap") {
		return "snap"
	}
	return ""
}

// OpenWindow shows the UI in a Chromium-family "app" window (or a dedicated
// Firefox profile as fallback). The returned channel closes when it exits.
func OpenWindow(url, profileDir string) (<-chan struct{}, error) {
	b := findBrowser()
	if b == nil {
		if err := exec.Command("xdg-open", url).Start(); err != nil {
			return nil, fmt.Errorf("no browser found; open %s manually", url)
		}
		return nil, errors.New("no Chrome/Chromium/Edge/Firefox found; opened the default browser")
	}
	if b.snap != "" {
		// Snap-confined browsers cannot use hidden folders in $HOME.
		home, _ := os.UserHomeDir()
		name := b.snap
		if name == "snap" {
			name = filepath.Base(b.exe)
		}
		profileDir = filepath.Join(home, "snap", name, "common", "securespeak-ui")
	}
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	if b.firefox {
		prefs := strings.Join([]string{
			`user_pref("media.autoplay.default", 0);`,
			`user_pref("browser.shell.checkDefaultBrowser", false);`,
			`user_pref("browser.aboutwelcome.enabled", false);`,
			`user_pref("datareporting.policy.dataSubmissionPolicyBypassNotification", true);`,
			`user_pref("browser.startup.homepage_override.mstone", "ignore");`,
			`user_pref("browser.tabs.warnOnClose", false);`,
		}, "\n") + "\n"
		os.WriteFile(filepath.Join(profileDir, "user.js"), []byte(prefs), 0600)
		cmd = exec.Command(b.exe, "--new-instance", "--profile", profileDir, url)
	} else {
		cmd = exec.Command(b.exe,
			"--app="+url,
			"--user-data-dir="+profileDir,
			"--window-size=1180,760",
			"--no-first-run",
			"--no-default-browser-check",
			"--disable-extensions",
			"--autoplay-policy=no-user-gesture-required",
			"--class=SecureSpeak",
		)
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	setWindow(cmd)
	setAppBrowser(b.exe, profileDir, b.firefox)
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	return done, nil
}

// Fatal shows an error dialog if a dialog tool exists, else prints it.
func Fatal(title, msg string) {
	fmt.Fprintln(os.Stderr, title+": "+msg)
	if p, err := exec.LookPath("zenity"); err == nil {
		exec.Command(p, "--error", "--title="+title, "--text="+msg).Run()
	} else if p, err := exec.LookPath("kdialog"); err == nil {
		exec.Command(p, "--title", title, "--error", msg).Run()
	}
}

// DownloadDir returns the XDG download folder (e.g. ~/Downloads, ~/Downloads localized).
func DownloadDir() string {
	home, _ := os.UserHomeDir()
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	if f, err := os.Open(filepath.Join(cfg, "user-dirs.dirs")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if v, ok := strings.CutPrefix(line, "XDG_DOWNLOAD_DIR="); ok {
				v = strings.Trim(v, `"`)
				v = strings.ReplaceAll(v, "$HOME", home)
				if filepath.IsAbs(v) {
					return v
				}
			}
		}
	}
	return filepath.Join(home, "Downloads")
}

func OpenURL(u string) error {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return errors.New("refusing to open non-web link")
	}
	return exec.Command("xdg-open", u).Start()
}

func OpenPath(dir string) error { return exec.Command("xdg-open", dir).Start() }

// Reveal highlights a file in the file manager (Nemo, Nautilus, Dolphin…)
// via the FileManager1 D-Bus interface, or opens its folder.
func Reveal(path string) error {
	if p, err := exec.LookPath("dbus-send"); err == nil {
		err := exec.Command(p, "--session", "--dest=org.freedesktop.FileManager1", "--type=method_call",
			"/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems",
			"array:string:file://"+path, "string:").Run()
		if err == nil {
			return nil
		}
	}
	return OpenPath(filepath.Dir(path))
}

// RefreshUserIcon keeps the menu icon of a per-user install (install-client.sh)
// current after an in-app update, which only replaces the binary.
func RefreshUserIcon(png []byte) {
	home, err := os.UserHomeDir()
	if err != nil || len(png) == 0 {
		return
	}
	share := filepath.Join(home, ".local", "share")
	if _, err := os.Stat(filepath.Join(share, "applications", "securespeak.desktop")); err != nil {
		return // not a per-user install
	}
	icons := filepath.Join(share, "icons", "hicolor")
	changed := os.Remove(filepath.Join(icons, "scalable", "apps", "securespeak.svg")) == nil
	dst := filepath.Join(icons, "256x256", "apps", "securespeak.png")
	if _, err := os.Stat(filepath.Join(icons, "512x512", "apps", "securespeak.png")); err != nil {
		if cur, err := os.ReadFile(dst); err != nil || string(cur) != string(png) {
			if os.MkdirAll(filepath.Dir(dst), 0755) == nil && os.WriteFile(dst, png, 0644) == nil {
				changed = true
			}
		}
	}
	if changed {
		now := time.Now()
		os.Chtimes(icons, now, now) // tells the desktop the icon theme changed
	}
}
