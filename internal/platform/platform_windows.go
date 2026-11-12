//go:build windows

// Package platform contains OS integration: secret storage and the UI window.
package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	crypt32             = syscall.NewLazyDLL("crypt32.dll")
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	pLocalFree          = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func blob(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func (b *dataBlob) bytes() []byte {
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

const cryptprotectUIForbidden = 0x1

// Protect encrypts data for the current Windows user (DPAPI).
func Protect(data []byte) ([]byte, error) {
	var out dataBlob
	r, _, err := pCryptProtectData.Call(uintptr(unsafe.Pointer(blob(data))), 0, 0, 0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}

func Unprotect(data []byte) ([]byte, error) {
	var out dataBlob
	r, _, err := pCryptUnprotectData.Call(uintptr(unsafe.Pointer(blob(data))), 0, 0, 0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}

const ProtectsSecrets = true

func findBrowser() string {
	var cands []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		cands = append(cands,
			filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
		)
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// OpenWindow shows the UI in a chromeless Edge/Chrome "app" window with its own
// profile. The returned channel is closed when the window process exits.
func OpenWindow(url, profileDir string) (<-chan struct{}, error) {
	done := make(chan struct{})
	exe := findBrowser()
	if exe == "" {
		// fall back to the default browser; we cannot detect closing
		err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		return nil, errors.Join(errors.New("Edge/Chrome not found; opened default browser"), err)
	}
	cmd := exec.Command(exe,
		"--app="+url,
		"--user-data-dir="+profileDir,
		"--window-size=1180,760",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--autoplay-policy=no-user-gesture-required",
		"--disable-features=Translate,msEdgeSidebarV2,msUndersideButton",
	)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	setWindow(cmd)
	go func() {
		cmd.Wait()
		close(done)
	}()
	return done, nil
}

var pMessageBoxW = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

// Fatal shows an error dialog (the GUI build has no console).
func Fatal(title, msg string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(msg)
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}

// DownloadDir returns the user's Downloads folder.
func DownloadDir() string {
	if p := os.Getenv("USERPROFILE"); p != "" {
		return filepath.Join(p, "Downloads")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

// OpenURL opens a web link in the default browser.
func OpenURL(u string) error {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return errors.New("refusing to open non-web link")
	}
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
}

// OpenPath opens a folder in Explorer.
func OpenPath(dir string) error { return exec.Command("explorer", dir).Start() }

// Reveal shows a file selected in Explorer (never executes it).
func Reveal(path string) error { return exec.Command("explorer", "/select,", path).Start() }
