package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"3.1.1", "3.1.1", 0},
		{"v3.1.2", "3.1.1", 1},
		{"3.2.0", "3.10.0", -1},
		{"3.2.0", "3.2.0-dev.7", 1},
		{"3.2.0-dev.7", "3.2.0-dev.10", -1},
		{"3.2.0-dev.7", "3.1.9", 1},
		{"3.1", "3.1.0", 0},
		{"4.0.0", "dev", 1},
		{"dev", "dev", 0},
		{"3.2.0-rc.1", "3.2.0-beta.2", 1},
		{"3.2.0+build5", "3.2.0", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

type fakeGH struct {
	srv      *httptest.Server
	files    map[string][]byte
	sums     map[string]string // overrides
	noSums   bool
	digest   string // override digest for all assets
	tag      string
	signKey  ed25519.PrivateKey // sign SHA256SUMS with this key
	badSig   bool               // serve a signature over different content
	requests int
	mu       sync.Mutex
}

func newFakeGH(t *testing.T, tag string, files map[string][]byte) *fakeGH {
	f := &fakeGH{files: files, sums: map[string]string{}, tag: tag}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests++
		f.mu.Unlock()
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "no UA", 403)
			return
		}
		switch {
		case r.URL.Path == "/latest":
			var assets []Asset
			names := []string{}
			for n := range f.files {
				names = append(names, n)
			}
			if !f.noSums {
				names = append(names, "SHA256SUMS")
			}
			if f.signKey != nil {
				names = append(names, "SHA256SUMS.sig")
			}
			for _, n := range names {
				a := Asset{Name: n, URL: f.srv.URL + "/dl/" + n, Size: int64(len(f.files[n]))}
				if n != "SHA256SUMS" && n != "SHA256SUMS.sig" {
					h := sha256.Sum256(f.files[n])
					a.Digest = "sha256:" + hex.EncodeToString(h[:])
					if f.digest != "" {
						a.Digest = f.digest
					}
				}
				assets = append(assets, a)
			}
			json.NewEncoder(w).Encode(Release{Tag: f.tag, Name: "SecureSpeak", Notes: "## What's new\n* things", URL: "https://example/rel", Published: "2026-11-13T10:00:00Z", Assets: assets})
		case r.URL.Path == "/dl/SHA256SUMS":
			w.Write(f.sumsFile())
		case r.URL.Path == "/dl/SHA256SUMS.sig":
			msg := f.sumsFile()
			if f.badSig {
				msg = append(msg, "tampered"...)
			}
			fmt.Fprintln(w, base64.StdEncoding.EncodeToString(ed25519.Sign(f.signKey, msg)))
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			b, ok := f.files[strings.TrimPrefix(r.URL.Path, "/dl/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(b)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGH) sumsFile() []byte {
	var names []string
	for n := range f.files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		h := sha256.Sum256(f.files[n])
		sum := hex.EncodeToString(h[:])
		if o, ok := f.sums[n]; ok {
			sum = o
		}
		fmt.Fprintf(&b, "%s  %s\n", sum, n)
	}
	return []byte(b.String())
}

func tarball(t *testing.T, files map[string][]byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for n, b := range files {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0755, Size: int64(len(b)), Typeflag: tar.TypeReg})
		tw.Write(b)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func testUpdater(t *testing.T, gh *fakeGH, kind Kind, exeName string) *Updater {
	dir := t.TempDir()
	exe := filepath.Join(dir, exeName)
	if err := os.WriteFile(exe, []byte("OLD BINARY"), 0755); err != nil {
		t.Fatal(err)
	}
	return &Updater{Current: "3.1.1", Exe: exe, Kind: kind, WorkDir: filepath.Join(t.TempDir(), "update"), API: gh.srv.URL + "/latest", HTTP: gh.srv.Client(), Arch: "amd64"}
}

func TestLinuxBinaryUpdate(t *testing.T) {
	gh := newFakeGH(t, "v3.2.0", map[string][]byte{
		"securespeak-client-3.2.0-linux.tar.gz": tarball(t, map[string][]byte{
			"securespeak": []byte("NEW AMD64"), "securespeak-arm64": []byte("NEW ARM64"), "install-client.sh": []byte("#!/bin/sh"),
		}),
		"SecureSpeak-Setup-3.2.0.exe": []byte("setup"),
	})
	u := testUpdater(t, gh, LinuxBinary, "securespeak")
	r, err := u.Latest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version() != "3.2.0" || !u.Newer(r) {
		t.Fatalf("version %q newer=%v", r.Version(), u.Newer(r))
	}
	var calls int
	p, err := u.Prepare(t.Context(), r, func(done, total int64) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(u.Exe); string(b) != "NEW AMD64" {
		t.Fatalf("exe now %q", b)
	}
	if fi, _ := os.Stat(u.Exe); runtime.GOOS != "windows" && fi.Mode()&0100 == 0 {
		t.Fatal("new binary is not executable")
	}
	if calls == 0 || p.Version != "3.2.0" || p.file != u.Exe {
		t.Fatalf("progress calls %d, pending %+v", calls, p)
	}

	u.Arch = "arm64"
	os.WriteFile(u.Exe, []byte("OLD"), 0755)
	if _, err := u.Prepare(t.Context(), r, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(u.Exe); string(b) != "NEW ARM64" {
		t.Fatalf("arm64 exe now %q", b)
	}
	u.Cleanup()
	if _, err := os.Stat(u.WorkDir); !os.IsNotExist(err) {
		t.Fatal("cleanup left the download folder")
	}
}

func TestPortableUpdate(t *testing.T) {
	gh := newFakeGH(t, "v3.2.0", map[string][]byte{"SecureSpeak-3.2.0-portable.exe": []byte("NEW EXE")})
	u := testUpdater(t, gh, WinPortable, "SecureSpeak.exe")
	r, err := u.Latest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Prepare(t.Context(), r, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(u.Exe); string(b) != "NEW EXE" {
		t.Fatalf("exe now %q", b)
	}
	if runtime.GOOS == "windows" {
		if b, _ := os.ReadFile(u.Exe + ".old"); string(b) != "OLD BINARY" {
			t.Fatalf("old exe %q", b)
		}
	}
	u.Cleanup()
	if _, err := os.Stat(u.Exe + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old not removed")
	}
}

func TestInstallerDownloadOnly(t *testing.T) {
	gh := newFakeGH(t, "v3.2.0", map[string][]byte{"SecureSpeak-Setup-3.2.0.exe": []byte("SETUP")})
	u := testUpdater(t, gh, WinInstaller, "SecureSpeak.exe")
	r, _ := u.Latest(t.Context())
	p, err := u.Prepare(t.Context(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.file); string(b) != "SETUP" {
		t.Fatal("setup not staged")
	}
	if b, _ := os.ReadFile(u.Exe); string(b) != "OLD BINARY" {
		t.Fatal("installer kind must not touch the exe itself")
	}
}

func TestRejectsBadDownloads(t *testing.T) {
	name := "SecureSpeak-3.2.0-portable.exe"
	check := func(t *testing.T, gh *fakeGH, want string) {
		t.Helper()
		u := testUpdater(t, gh, WinPortable, "SecureSpeak.exe")
		r, err := u.Latest(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		_, err = u.Prepare(t.Context(), r, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want %q", err, want)
		}
		if b, _ := os.ReadFile(u.Exe); string(b) != "OLD BINARY" {
			t.Fatal("exe was replaced by a bad download")
		}
		if left, _ := filepath.Glob(filepath.Join(filepath.Dir(u.Exe), ".*")); len(left) > 0 {
			t.Fatalf("temporary files left: %v", left)
		}
	}
	t.Run("tampered", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("EVIL")})
		h := sha256.Sum256([]byte("GOOD"))
		gh.sums[name] = hex.EncodeToString(h[:])
		gh.digest = "sha256:" + gh.sums[name]
		check(t, gh, "checksum mismatch")
	})
	t.Run("no SHA256SUMS", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("NEW")})
		gh.noSums = true
		check(t, gh, "SHA256SUMS")
	})
	t.Run("digest disagrees", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("NEW")})
		gh.digest = "sha256:" + strings.Repeat("0", 64)
		check(t, gh, "does not match GitHub")
	})
	t.Run("missing asset", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{"other.exe": []byte("x")})
		check(t, gh, "has no "+name)
	})
}

func TestManager(t *testing.T) {
	gh := newFakeGH(t, "v3.2.0", map[string][]byte{"SecureSpeak-3.2.0-portable.exe": []byte("NEW EXE")})
	u := testUpdater(t, gh, WinPortable, "SecureSpeak.exe")
	states := make(chan State, 100)
	auto := true
	m := NewManager(u, func(s State) { states <- s }, func() bool { return auto })
	wait := func(status string) State {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case s := <-states:
				if s.Status == status {
					return s
				}
				if s.Status == "error" && status != "error" {
					t.Fatalf("error: %s", s.Error)
				}
			case <-deadline:
				t.Fatalf("timed out waiting for %s", status)
			}
		}
	}
	if err := m.Install(); err == nil {
		t.Fatal("install before a check must fail")
	}
	m.Check()
	s := wait("available")
	if s.Latest != "3.2.0" || !s.CanInstall || !s.Auto || s.Notes == "" || s.Kind != "portable" {
		t.Fatalf("state %+v", s)
	}
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	wait("restarting")
	select {
	case p := <-m.Restart:
		if p.Version != "3.2.0" {
			t.Fatal(p.Version)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no restart")
	}

	// once on the latest version: up to date
	u.Current = "3.2.0"
	m.Check()
	if s := wait("uptodate"); s.CanInstall {
		t.Fatal("up to date but installable")
	}
	if err := m.Install(); err == nil {
		t.Fatal("installing the same version must fail")
	}

	// dev builds never offer an update
	u.Current = "dev"
	m.Check()
	wait("dev")

	// a manual install shows the update but cannot install it
	u.Current, u.Kind, u.Reason = "3.1.1", Manual, "read-only"
	m.Check()
	if s := wait("available"); s.CanInstall {
		t.Fatal("manual install offered Install")
	}

	// unreachable GitHub is an error, not a crash
	u.API = "http://127.0.0.1:1/latest"
	m.Check()
	if s := wait("error"); !strings.Contains(s.Error, "Could not check") {
		t.Fatal(s.Error)
	}
}

// TestSwapRunningExe replaces an executable while it is running, like the
// portable Windows build and the Linux binary update do.
func TestSwapRunningExe(t *testing.T) {
	if os.Getenv("SS_UPDATE_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "SecureSpeak.exe")
	if err := copyFile(self, exe, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestSwapRunningExe$")
	cmd.Env = append(os.Environ(), "SS_UPDATE_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	time.Sleep(300 * time.Millisecond)

	newFile := filepath.Join(t.TempDir(), "new.exe")
	os.WriteFile(newFile, []byte("NEW VERSION"), 0755)
	if err := swapExe(exe, newFile); err != nil {
		t.Fatalf("swapping a running exe: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW VERSION" {
		t.Fatal("exe not replaced")
	}
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(exe + ".old"); err != nil {
			t.Fatal("running exe was not moved aside")
		}
	}
}

func pemPublic(t *testing.T, pub ed25519.PublicKey) []byte {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func TestSignedReleases(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	keys, err := ParseKeys(append([]byte("# comment\n"), append(pemPublic(t, otherPub), pemPublic(t, pub)...)...))
	if err != nil || len(keys) != 2 {
		t.Fatalf("ParseKeys: %v %d", err, len(keys))
	}
	if k, err := ParseKeys(signingKeysPEM); err != nil {
		t.Fatalf("built-in signing-keys.pem: %v", err)
	} else if len(k) != len(TrustedKeys) {
		t.Fatal("TrustedKeys mismatch")
	}
	if _, err := ParseKeys([]byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n")); err == nil {
		t.Fatal("a private key in signing-keys.pem must be rejected")
	}
	name := "SecureSpeak-3.2.0-portable.exe"
	run := func(t *testing.T, gh *fakeGH, keys []ed25519.PublicKey) error {
		t.Helper()
		u := testUpdater(t, gh, WinPortable, "SecureSpeak.exe")
		u.Keys = keys
		r, err := u.Latest(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		_, err = u.Prepare(t.Context(), r, nil)
		b, _ := os.ReadFile(u.Exe)
		if (err == nil) != (string(b) == "NEW") {
			t.Fatalf("err=%v but exe=%q", err, b)
		}
		return err
	}
	t.Run("signed by a trusted key", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("NEW"), "x.deb": []byte("y")})
		gh.signKey = priv
		if err := run(t, gh, keys); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("second key also works (rotation)", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("NEW")})
		gh.signKey = otherPriv
		if err := run(t, gh, keys); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unsigned release refused", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("NEW")})
		if err := run(t, gh, keys); err == nil || !strings.Contains(err.Error(), "not signed") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("signed by an unknown key refused", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("NEW")})
		_, stranger, _ := ed25519.GenerateKey(rand.Reader)
		gh.signKey = stranger
		if err := run(t, gh, keys); err == nil || !strings.Contains(err.Error(), "not valid") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("SHA256SUMS changed after signing refused", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("NEW")})
		gh.signKey = priv
		gh.badSig = true
		if err := run(t, gh, keys); err == nil || !strings.Contains(err.Error(), "not valid") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("attacker replaces file and checksums but cannot sign", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("EVIL")})
		gh.signKey = stranger(t)
		if err := run(t, gh, keys); err == nil {
			t.Fatal("malicious release accepted")
		}
	})
	t.Run("without built-in keys signatures are optional", func(t *testing.T) {
		gh := newFakeGH(t, "v3.2.0", map[string][]byte{name: []byte("NEW")})
		if err := run(t, gh, nil); err != nil {
			t.Fatal(err)
		}
	})
}

func stranger(t *testing.T) ed25519.PrivateKey {
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestOpenSSLSignatureFormat checks that a signature made the way CI makes it
// (openssl pkeyutl -sign -rawin, base64) verifies with VerifySums.
func TestOpenSSLSignatureFormat(t *testing.T) {
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl not installed")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "k.pem")
	if out, err := exec.Command(openssl, "genpkey", "-algorithm", "ed25519", "-out", key).CombinedOutput(); err != nil {
		t.Skipf("openssl without ed25519: %v %s", err, out)
	}
	pubPEM, err := exec.Command(openssl, "pkey", "-in", key, "-pubout").Output()
	if err != nil {
		t.Fatal(err)
	}
	sums := filepath.Join(dir, "SHA256SUMS")
	os.WriteFile(sums, []byte("abc  file.exe\n"), 0644)
	sig, err := exec.Command(openssl, "pkeyutl", "-sign", "-inkey", key, "-rawin", "-in", sums).Output()
	if err != nil {
		t.Skipf("openssl pkeyutl -rawin unsupported: %v", err)
	}
	keys, err := ParseKeys(pubPEM)
	if err != nil || len(keys) != 1 {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sums)
	if err := VerifySums(b, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), keys); err != nil {
		t.Fatal(err)
	}
	if err := VerifySums(append(b, 'x'), []byte(base64.StdEncoding.EncodeToString(sig)), keys); err == nil {
		t.Fatal("modified SHA256SUMS accepted")
	}
}
