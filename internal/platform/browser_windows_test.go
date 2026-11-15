//go:build windows

package platform

import "testing"

// On the CI runner (Edge and Chrome installed) a browser must be found, and
// reading the default browser from the registry must not fail badly.
func TestFindBrowserWindows(t *testing.T) {
	t.Logf("default browser: %q", defaultBrowserExe())
	b := findBrowser()
	t.Logf("app window browser: %q", b)
	if b == "" {
		t.Fatal("no browser found on a machine with Edge")
	}
	if !supportsAppMode(b) {
		t.Fatalf("picked a browser without app mode: %s", b)
	}
}
