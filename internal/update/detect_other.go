//go:build !windows && !linux

package update

import "os/exec"

func detect(exe string) (Kind, string) {
	return Manual, "Automatic updates are not available on this system; download the new version from GitHub"
}

func detach(cmd *exec.Cmd) {}
