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

// findBrowser picks the browser for the app window: Edge or Chrome first
// (the most tested), then the user's default browser if it has an app mode
// (Brave, Vivaldi, Chromium...), then other app-mode browsers in their usual
// install folders. "" means none: the default browser opens a normal tab.
func findBrowser() string {
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
		if base := os.Getenv(env); base != "" {
			roots = append(roots, base)
		}
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	find := func(rel ...string) string {
		for _, r := range roots {
			if p := filepath.Join(append([]string{r}, rel...)...); exists(p) {
				return p
			}
		}
		return ""
	}
	if p := find("Microsoft", "Edge", "Application", "msedge.exe"); p != "" {
		return p
	}
	if p := find("Google", "Chrome", "Application", "chrome.exe"); p != "" {
		return p
	}
	if p := defaultBrowserExe(); p != "" && supportsAppMode(p) && exists(p) {
		return p
	}
	for _, rel := range [][]string{
		{"BraveSoftware", "Brave-Browser", "Application", "brave.exe"},
		{"Vivaldi", "Application", "vivaldi.exe"},
		{"Chromium", "Application", "chrome.exe"},
	} {
		if p := find(rel...); p != "" {
			return p
		}
	}
	return ""
}

// defaultBrowserExe returns the program the user chose for https links.
func defaultBrowserExe() string {
	progID := regString(syscall.HKEY_CURRENT_USER, `Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice`, "ProgId")
	if progID == "" {
		return ""
	}
	for _, k := range []struct {
		root syscall.Handle
		path string
	}{
		{syscall.HKEY_CURRENT_USER, `Software\Classes\` + progID + `\shell\open\command`},
		{syscall.HKEY_LOCAL_MACHINE, `Software\Classes\` + progID + `\shell\open\command`},
		{syscall.HKEY_CLASSES_ROOT, progID + `\shell\open\command`},
	} {
		if cmd := regString(k.root, k.path, ""); cmd != "" {
			return exeFromCommand(cmd)
		}
	}
	return ""
}

// regString reads a string value ("" = the key's default value).
func regString(root syscall.Handle, path, name string) string {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var key syscall.Handle
	if syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ, &key) != nil {
		return ""
	}
	defer syscall.RegCloseKey(key)
	var namePtr *uint16
	if name != "" {
		if namePtr, err = syscall.UTF16PtrFromString(name); err != nil {
			return ""
		}
	}
	var typ, size uint32
	if syscall.RegQueryValueEx(key, namePtr, nil, &typ, nil, &size) != nil || size == 0 || size > 64*1024 {
		return ""
	}
	if typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ {
		return ""
	}
	buf := make([]uint16, size/2+1)
	if syscall.RegQueryValueEx(key, namePtr, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size) != nil {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// OpenWindow shows the UI in a chromeless Edge/Chrome "app" window with its own
// profile. The returned channel is closed when the window process exits.
func OpenWindow(url, profileDir string) (<-chan struct{}, error) {
	done := make(chan struct{})
	exe := findBrowser()
	if exe == "" {
		// fall back to the default browser; we cannot detect closing
		err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		return nil, errors.Join(errors.New("no browser with an app mode found (Edge, Chrome, Brave, Vivaldi, Chromium); opened the default browser"), err)
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
	setAppBrowser(exe, profileDir, false)
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

// RefreshUserIcon is only needed on Linux.
func RefreshUserIcon(png []byte) {}
