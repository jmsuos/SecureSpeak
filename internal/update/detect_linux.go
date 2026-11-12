//go:build linux

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func detect(exe string) (Kind, string) {
	if exe == "/usr/bin/securespeak" {
		if _, err := os.Stat("/var/lib/dpkg/info/securespeak.list"); err == nil {
			if _, err := exec.LookPath("pkexec"); err != nil {
				return Manual, "Installing the update needs pkexec (policykit-1); download the new .deb from GitHub"
			}
			return LinuxDeb, ""
		}
	}
	dir := filepath.Dir(exe)
	if syscall.Access(dir, 2 /* W_OK */) == nil {
		return LinuxBinary, ""
	}
	return Manual, "SecureSpeak cannot write to " + dir + "; update it with your package manager or download it from GitHub"
}

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
