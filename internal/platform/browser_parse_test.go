package platform

import (
	"os"
	"testing"
)

func TestExeFromCommand(t *testing.T) {
	os.Setenv("SS_TEST_LOCALAPPDATA", `C:\Users\jordy\AppData\Local`)
	for cmd, want := range map[string]string{
		`"C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe" --single-argument %1`: `C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`,
		`"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe" --single-argument %1`:       `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`%SS_TEST_LOCALAPPDATA%\Vivaldi\Application\vivaldi.exe -- "%1"`:                            `C:\Users\jordy\AppData\Local\Vivaldi\Application\vivaldi.exe`,
		`C:\Firefox\firefox.EXE -osint -url "%1"`:                                                   `C:\Firefox\firefox.EXE`,
		`"unterminated`: ``,
		``:              ``,
	} {
		if got := exeFromCommand(cmd); got != want {
			t.Errorf("exeFromCommand(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestSupportsAppMode(t *testing.T) {
	for exe, want := range map[string]bool{
		`C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`: true,
		`C:\x\msedge.exe`:            true,
		`C:\x\Chrome.EXE`:            true,
		`C:\x\vivaldi.exe`:           true,
		`C:\Firefox\firefox.exe`:     false,
		`C:\Opera\launcher.exe`:      false,
		`C:\x\brave.exe.evil\ie.exe`: false,
	} {
		if got := supportsAppMode(exe); got != want {
			t.Errorf("supportsAppMode(%q) = %v", exe, got)
		}
	}
}
