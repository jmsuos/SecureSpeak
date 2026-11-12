//go:build !windows && !linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const ProtectsSecrets = false

func Protect(data []byte) ([]byte, error)   { return data, nil }
func Unprotect(data []byte) ([]byte, error) { return data, nil }

func OpenWindow(url, profileDir string) (<-chan struct{}, error) {
	fmt.Println("Open the SecureSpeak UI in your browser:", url)
	return nil, errors.New("no app window on this platform")
}

func Fatal(title, msg string) {}

func DownloadDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func OpenURL(u string) error    { return exec.Command("open", u).Start() }
func OpenPath(dir string) error { return exec.Command("open", dir).Start() }
func Reveal(path string) error  { return exec.Command("open", "-R", path).Start() }

// RefreshUserIcon is only needed on Linux.
func RefreshUserIcon(png []byte) {}
