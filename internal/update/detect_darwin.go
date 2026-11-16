//go:build darwin

package update

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func detect(exe string) (Kind, string) {
	b := appBundle(exe)
	switch {
	case b == "":
		return Manual, "Start SecureSpeak from SecureSpeak.app to get automatic updates"
	case strings.Contains(b, "/AppTranslocation/") || strings.HasPrefix(b, "/Volumes/"):
		return Manual, "Move SecureSpeak to your Applications folder (drag it there from the download) to get automatic updates"
	}
	if syscall.Access(filepath.Dir(b), 2 /* W_OK */) != nil {
		return Manual, "SecureSpeak cannot write to " + filepath.Dir(b) + "; download the new version from GitHub"
	}
	return MacApp, ""
}

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
