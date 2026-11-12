//go:build windows

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func detect(exe string) (Kind, string) {
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, "unins000.exe")); err == nil {
		return WinInstaller, ""
	}
	if writable(dir) {
		return WinPortable, ""
	}
	return Manual, "SecureSpeak cannot write to " + dir + "; download the new version from GitHub"
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".ss-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

const detachedProcess = 0x00000008

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}
