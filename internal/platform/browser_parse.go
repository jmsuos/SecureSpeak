package platform

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// appModeBrowsers are Chromium-based browsers whose "--app=" mode gives a
// window without address bar or tabs (Opera and Firefox don't have it).
var appModeBrowsers = map[string]bool{
	"msedge.exe": true, "chrome.exe": true, "brave.exe": true, "vivaldi.exe": true, "chromium.exe": true,
}

var envVarRe = regexp.MustCompile(`%([^%]+)%`)

// exeFromCommand extracts the program path from a Windows "open" command
// such as `"C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe" --single-argument %1`.
func exeFromCommand(cmd string) string {
	cmd = strings.TrimSpace(envVarRe.ReplaceAllStringFunc(cmd, func(m string) string {
		if v := os.Getenv(m[1 : len(m)-1]); v != "" {
			return v
		}
		return m
	}))
	if strings.HasPrefix(cmd, `"`) {
		if end := strings.Index(cmd[1:], `"`); end >= 0 {
			return cmd[1 : end+1]
		}
		return ""
	}
	if i := strings.Index(strings.ToLower(cmd), ".exe"); i >= 0 {
		return cmd[:i+4]
	}
	return ""
}

// supportsAppMode reports whether exe is a browser with an app-window mode.
func supportsAppMode(exe string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(exe, `\`, "/")))
	return appModeBrowsers[base]
}
