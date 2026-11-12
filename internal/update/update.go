// Package update checks GitHub Releases for a newer SecureSpeak and installs
// it the same way the running copy was installed:
//
//   - Windows, installed with the setup program: the new setup runs silently
//     after SecureSpeak has closed and starts it again.
//   - Windows portable exe: the exe is swapped in place.
//   - Linux .deb (/usr/bin/securespeak): `pkexec dpkg -i` (asks for your password).
//   - Linux binary in a writable folder (install-client.sh, ~/.local/bin): swapped in place.
//
// Every download is checked against the release's SHA256SUMS file (and the
// SHA-256 digest GitHub reports for the asset) before anything is replaced.
package update

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases are published in.
const Repo = "jmsuos/SecureSpeak"

// DefaultAPI is the "latest release" endpoint.
var DefaultAPI = "https://api.github.com/repos/" + Repo + "/releases/latest"

const maxDownload = 300 << 20

// Kind is how the running copy was installed.
type Kind int

const (
	Manual       Kind = iota // cannot update itself; show the download page
	WinInstaller             // Inno Setup install
	WinPortable              // stand-alone SecureSpeak.exe
	LinuxDeb                 // .deb package in /usr/bin
	LinuxBinary              // binary in a user-writable folder
)

func (k Kind) String() string {
	switch k {
	case WinInstaller:
		return "installer"
	case WinPortable:
		return "portable"
	case LinuxDeb:
		return "deb"
	case LinuxBinary:
		return "binary"
	}
	return "manual"
}

// Asset is a downloadable file of a release.
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // "sha256:…" (newer GitHub API versions)
}

// Release is the latest published release.
type Release struct {
	Tag       string  `json:"tag_name"`
	Name      string  `json:"name"`
	Notes     string  `json:"body"`
	URL       string  `json:"html_url"`
	Published string  `json:"published_at"`
	Draft     bool    `json:"draft"`
	Pre       bool    `json:"prerelease"`
	Assets    []Asset `json:"assets"`
}

// Version returns the release version without a leading "v".
func (r *Release) Version() string { return strings.TrimPrefix(strings.TrimPrefix(r.Tag, "v"), ".") }

// Updater checks for and installs updates for one executable.
type Updater struct {
	Current string // running version
	Exe     string // path of the running executable
	Kind    Kind
	Reason  string // why Kind is Manual
	WorkDir string // where downloads are staged
	API     string
	HTTP    *http.Client
	Arch    string
}

// New prepares an updater for the running executable.
func New(current, workDir string) *Updater {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	u := &Updater{
		Current: current, Exe: exe, WorkDir: workDir, API: DefaultAPI, Arch: runtime.GOARCH,
		HTTP: &http.Client{Timeout: 10 * time.Minute},
	}
	u.Kind, u.Reason = detect(exe)
	return u
}

// Cleanup removes leftovers of a previous update (old exe, downloads).
func (u *Updater) Cleanup() {
	os.Remove(u.Exe + ".old")
	os.RemoveAll(u.WorkDir)
}

func (u *Updater) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SecureSpeak/"+u.Current)
	if strings.Contains(url, "api.github.com") {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("timed out; check your internet connection")
		}
		log.Printf("update: %v", err)
		return nil, fmt.Errorf("cannot reach %s; check your internet connection", req.URL.Hostname())
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		if resp.StatusCode == 403 || resp.StatusCode == 429 {
			return nil, errors.New("GitHub is rate limiting update checks; try again later")
		}
		if resp.StatusCode == 404 {
			return nil, errors.New("no release published yet")
		}
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return resp, nil
}

// Latest fetches the latest release.
func (u *Updater) Latest(ctx context.Context) (*Release, error) {
	resp, err := u.get(ctx, u.API)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("reading release info: %w", err)
	}
	if r.Draft || r.Pre || r.Tag == "" {
		return nil, errors.New("no stable release found")
	}
	return &r, nil
}

// IsDev reports whether the running build has no comparable version.
func (u *Updater) IsDev() bool { _, ok := parse(u.Current); return !ok }

// Newer reports whether r is newer than the running version.
func (u *Updater) Newer(r *Release) bool { return Compare(r.Version(), u.Current) > 0 }

// assetName returns the file this install kind needs from a release.
func (u *Updater) assetName(v string) string {
	switch u.Kind {
	case WinInstaller:
		return "SecureSpeak-Setup-" + v + ".exe"
	case WinPortable:
		return "SecureSpeak-" + v + "-portable.exe"
	case LinuxDeb:
		return "securespeak_" + v + "_" + u.Arch + ".deb"
	case LinuxBinary:
		return "securespeak-client-" + v + "-linux.tar.gz"
	}
	return ""
}

// Asset returns the release file for this install, or nil.
func (u *Updater) Asset(r *Release) *Asset {
	want := u.assetName(r.Version())
	for i := range r.Assets {
		if want != "" && r.Assets[i].Name == want {
			return &r.Assets[i]
		}
	}
	return nil
}

func findAsset(r *Release, name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// Pending is a downloaded and verified update, ready to finish once the app
// has shut down.
type Pending struct {
	Version string
	kind    Kind
	file    string // installer or new executable
}

// Prepare downloads and verifies the update and, where possible, installs it
// already (swapping the binary, or dpkg via pkexec), so failures are reported
// while the app is still open. progress receives (done, total) bytes.
func (u *Updater) Prepare(ctx context.Context, r *Release, progress func(done, total int64)) (*Pending, error) {
	if u.Kind == Manual {
		return nil, errors.New(u.Reason)
	}
	a := u.Asset(r)
	if a == nil {
		return nil, fmt.Errorf("release %s has no %s", r.Tag, u.assetName(r.Version()))
	}
	sums := findAsset(r, "SHA256SUMS")
	if sums == nil {
		return nil, errors.New("release has no SHA256SUMS file; refusing to install an unverified download")
	}
	want, err := u.checksum(ctx, sums, a.Name)
	if err != nil {
		return nil, err
	}
	if d, ok := strings.CutPrefix(a.Digest, "sha256:"); ok && !strings.EqualFold(d, want) {
		return nil, errors.New("SHA256SUMS does not match GitHub's checksum for " + a.Name)
	}
	if err := os.MkdirAll(u.WorkDir, 0700); err != nil {
		return nil, err
	}
	file := filepath.Join(u.WorkDir, a.Name)
	if err := u.download(ctx, a, file, want, progress); err != nil {
		return nil, err
	}
	p := &Pending{Version: r.Version(), kind: u.Kind, file: file}
	switch u.Kind {
	case WinPortable:
		if err := swapExe(u.Exe, file); err != nil {
			return nil, err
		}
		p.file = u.Exe
	case LinuxBinary:
		member := "securespeak"
		if u.Arch == "arm64" {
			member = "securespeak-arm64"
		}
		if err := swapFromTarball(u.Exe, file, member); err != nil {
			return nil, err
		}
		p.file = u.Exe
	case LinuxDeb:
		if err := installDeb(ctx, file); err != nil {
			return nil, err
		}
		p.file = u.Exe
	}
	return p, nil
}

func (u *Updater) checksum(ctx context.Context, sums *Asset, name string) (string, error) {
	resp, err := u.get(ctx, sums.URL)
	if err != nil {
		return "", fmt.Errorf("downloading SHA256SUMS: %w", err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			if _, err := hex.DecodeString(f[0]); err == nil {
				return strings.ToLower(f[0]), nil
			}
		}
	}
	return "", errors.New("SHA256SUMS has no entry for " + name)
}

func (u *Updater) download(ctx context.Context, a *Asset, dst, wantHex string, progress func(done, total int64)) error {
	resp, err := u.get(ctx, a.URL)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", a.Name, err)
	}
	defer resp.Body.Close()
	total := resp.ContentLength
	if total <= 0 {
		total = a.Size
	}
	f, err := os.OpenFile(dst+".part", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(dst + ".part")
	h := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	last := time.Time{}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > maxDownload {
				f.Close()
				return errors.New("download is unexpectedly large")
			}
			h.Write(buf[:n])
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			if progress != nil && time.Since(last) > 150*time.Millisecond {
				last = time.Now()
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return fmt.Errorf("downloading %s: %w", a.Name, rerr)
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantHex {
		return errors.New("checksum mismatch: the download is corrupt or was tampered with")
	}
	if progress != nil {
		progress(done, done)
	}
	return os.Rename(dst+".part", dst)
}

// swapExe replaces exe with newFile. The running exe is renamed to .old
// first (Windows allows renaming, not overwriting, a running program).
func swapExe(exe, newFile string) error {
	dir := filepath.Dir(exe)
	tmp := filepath.Join(dir, "."+filepath.Base(exe)+".new")
	if err := copyFile(newFile, tmp, 0755); err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	return replace(exe, tmp)
}

func replace(exe, tmp string) error {
	old := exe + ".old"
	os.Remove(old)
	if runtime.GOOS == "windows" {
		if err := os.Rename(exe, old); err != nil {
			os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, exe); err != nil {
			os.Rename(old, exe)
			os.Remove(tmp)
			return err
		}
		return nil
	}
	// POSIX: rename over the running binary is atomic and safe.
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func swapFromTarball(exe, tarball, member string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return errors.New("the download does not contain " + member)
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || strings.TrimPrefix(h.Name, "./") != member {
			continue
		}
		dir := filepath.Dir(exe)
		tmp := filepath.Join(dir, "."+filepath.Base(exe)+".new")
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", dir, err)
		}
		_, err = io.Copy(out, io.LimitReader(tr, maxDownload))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(tmp)
			return err
		}
		os.Chmod(tmp, 0755)
		return replace(exe, tmp)
	}
}

func installDeb(ctx context.Context, deb string) error {
	pk, err := exec.LookPath("pkexec")
	if err != nil {
		return errors.New("pkexec is not installed; install the .deb manually")
	}
	// dpkg runs as root and must be able to read the file.
	os.Chmod(filepath.Dir(deb), 0755)
	os.Chmod(deb, 0644)
	out, err := exec.CommandContext(ctx, pk, "/usr/bin/dpkg", "-i", deb).CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && (ee.ExitCode() == 126 || ee.ExitCode() == 127) {
			return errors.New("installation cancelled (no administrator permission)")
		}
		msg := strings.TrimSpace(string(out))
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		return fmt.Errorf("dpkg failed: %v %s", err, msg)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// Finish starts the updated SecureSpeak (or the setup program, which starts
// it when done). Call it after the app has shut down, right before exiting.
// args are passed to the restarted app.
func (p *Pending) Finish(args []string) error {
	if p.kind == WinInstaller {
		// /UPDATE=1 makes the setup start SecureSpeak again when it is done.
		cmd := exec.Command(p.file, "/SILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-", "/UPDATE=1")
		detach(cmd)
		return cmd.Start()
	}
	cmd := exec.Command(p.file, append([]string{"-after-update"}, args...)...)
	detach(cmd)
	return cmd.Start()
}

// ---- versions

type semver struct {
	core [3]int
	pre  []string
}

func parse(v string) (semver, bool) {
	v = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	var s semver
	core := v
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core, s.pre = v[:i], strings.Split(v[i+1:], ".")
	}
	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return s, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return s, false
		}
		s.core[i] = n
	}
	return s, true
}

// Compare compares two versions like semver: -1, 0 or 1. Versions that cannot
// be parsed (e.g. "dev") sort below every real version.
func Compare(a, b string) int {
	va, oka := parse(a)
	vb, okb := parse(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := 0; i < 3; i++ {
		if va.core[i] != vb.core[i] {
			if va.core[i] < vb.core[i] {
				return -1
			}
			return 1
		}
	}
	// a release is newer than its pre-releases (3.2.0 > 3.2.0-dev.7)
	if len(va.pre) == 0 || len(vb.pre) == 0 {
		switch {
		case len(va.pre) == len(vb.pre):
			return 0
		case len(va.pre) == 0:
			return 1
		default:
			return -1
		}
	}
	for i := 0; i < len(va.pre) && i < len(vb.pre); i++ {
		x, y := va.pre[i], vb.pre[i]
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil:
			if nx != ny {
				if nx < ny {
					return -1
				}
				return 1
			}
		case ex == nil:
			return -1
		case ey == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(va.pre) < len(vb.pre):
		return -1
	case len(va.pre) > len(vb.pre):
		return 1
	}
	return 0
}
