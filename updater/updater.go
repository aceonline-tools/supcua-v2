package updater

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	assetName      = "supcua-v2.exe"
	apiVersion     = "2022-11-28"
	userAgent      = "supcua-v2-updater"
	maxAPIBodySize = 1 << 20
)

// Overridable in tests.
var (
	apiBase    = "https://api.github.com"
	repo       = "aceonline-tools/supcua-v2"
	checkTO    = 10 * time.Second
	downloadTO = 2 * time.Minute
)

var (
	ErrRateLimited  = errors.New("github api rate limited")
	ErrNoRelease    = errors.New("no published release found (repo renamed, private, or no releases)")
	ErrUntrustedURL = errors.New("untrusted download url")
	ErrNoAsset      = errors.New("latest release has no " + assetName + " asset")
)

type release struct {
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

// parseStrict parses a release version "X.Y.Z" (optional leading "v").
// Anything else, including pre-release/build suffixes, is rejected.
func parseStrict(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		if p == "" || len(p) > 9 || (len(p) > 1 && p[0] == '0') {
			return out, false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return out, false
			}
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out, true
}

// parseCurrent parses the running build's version leniently: a pre-release or
// build suffix is dropped ("1.0.29-rc.1" -> 1.0.29) and unparseable values such
// as "dev" are treated as 0.0.0, so dev builds are offered any release.
func parseCurrent(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	out, _ := parseStrict(v)
	return out
}

func gt(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func pickAsset(assets []asset) (asset, bool) {
	for _, a := range assets {
		if a.Name == assetName && a.BrowserDownloadURL != "" {
			return a, true
		}
	}
	return asset{}, false
}

func rateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return resp.StatusCode == http.StatusForbidden &&
		(resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")
}

// CheckUpdate returns the latest version (without "v") and download URL if a
// newer published release exists. No update (nil error, empty values) is
// reported for: draft/pre-release, non-semver tag, or a version that is not
// newer. A 404 returns ErrNoRelease. Single request, no retries.
func CheckUpdate(currentVersion string) (latestVersion, dlURL string, err error) {
	req, err := http.NewRequest(http.MethodGet, apiBase+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)

	resp, err := (&http.Client{Timeout: checkTO}).Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch latest: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case rateLimited(resp):
		return "", "", fmt.Errorf("%w (status %d, retry-after %q, reset %q)", ErrRateLimited,
			resp.StatusCode, resp.Header.Get("Retry-After"), resp.Header.Get("X-RateLimit-Reset"))
	case resp.StatusCode == http.StatusNotFound:
		return "", "", ErrNoRelease
	case resp.StatusCode != http.StatusOK:
		return "", "", fmt.Errorf("fetch latest: unexpected status %d", resp.StatusCode)
	}

	var rel release
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBodySize))
	if err := dec.Decode(&rel); err != nil {
		return "", "", fmt.Errorf("parse latest: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", "", errors.New("parse latest: trailing data after JSON object")
	}
	if rel.Draft || rel.Prerelease {
		return "", "", nil
	}
	latest, ok := parseStrict(rel.TagName)
	if !ok {
		return "", "", nil
	}
	if !gt(latest, parseCurrent(currentVersion)) {
		return "", "", nil
	}
	a, ok := pickAsset(rel.Assets)
	if !ok {
		return "", "", ErrNoAsset
	}
	if !initialAllowed(a.BrowserDownloadURL) {
		return "", "", fmt.Errorf("%w: %s", ErrUntrustedURL, a.BrowserDownloadURL)
	}
	expected.Store(a.BrowserDownloadURL, a)
	return strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v"), a.BrowserDownloadURL, nil
}

const (
	maxDownload  = 200 << 20
	maxRedirects = 5
	downloadPath = "/" + "aceonline-tools/supcua-v2" + "/releases/download/"
)

// expected holds the selected asset metadata keyed by the URL CheckUpdate returned.
var expected sync.Map

// Overridable in tests.
var (
	initialAllowed  = defaultInitialAllowed
	redirectAllowed = defaultRedirectAllowed
	transport       http.RoundTripper
)

func defaultInitialAllowed(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" &&
		path.Clean(u.Path) == u.Path && strings.HasPrefix(u.Path, downloadPath)
}

func defaultRedirectAllowed(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && (h == "github.com" || strings.HasSuffix(h, ".githubusercontent.com"))
}

// downloadTo fetches url into dst. It enforces trusted hosts, the asset size
// (or Content-Length when unknown, else a hard cap), the PE "MZ" header and
// the sha256 digest when known. The partial file is removed on any failure.
func downloadTo(rawURL, dst string) error { return downloadRecorded(rawURL, dst, nil) }

// downloadRecord is what the verified download looked like: the size and sha256 of the bytes
// written, taken from the very pass that enforced size/digest/MZ (no second read of the file).
type downloadRecord struct {
	Size   int64
	SHA256 string
}

func downloadRecorded(rawURL, dst string, rec *downloadRecord) error {
	if !initialAllowed(rawURL) {
		return fmt.Errorf("%w: %s", ErrUntrustedURL, rawURL)
	}
	var want int64 = -1
	var wantSum string
	if v, ok := expected.Load(rawURL); ok {
		a := v.(asset)
		if a.Size > 0 {
			want = a.Size
		}
		wantSum = strings.TrimPrefix(a.Digest, "sha256:")
		if wantSum == a.Digest {
			wantSum = ""
		}
	}
	if b, err := hex.DecodeString(wantSum); err != nil || len(b) != sha256.Size {
		wantSum = ""
		log.Printf("updater: no usable sha256 digest for asset, hash check skipped")
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{
		Timeout:   downloadTO,
		Transport: transport,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return errors.New("too many redirects")
			}
			if !redirectAllowed(r.URL) {
				return fmt.Errorf("%w: redirect to %s", ErrUntrustedURL, r.URL.Host)
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}
	if want < 0 && resp.ContentLength >= 0 {
		want = resp.ContentLength
	}
	limit := int64(maxDownload)
	if want >= 0 {
		if want > maxDownload {
			return fmt.Errorf("download too large: %d", want)
		}
		limit = want
	}

	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // exclusive: never follows or reuses an existing file
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
	case n > limit:
		err = fmt.Errorf("download exceeds limit %d", limit)
	case want >= 0 && n != want:
		err = fmt.Errorf("size mismatch: got %d, want %d", n, want)
	case n < 2:
		err = errors.New("empty download")
	case wantSum != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantSum):
		err = errors.New("sha256 digest mismatch")
	}
	if err == nil {
		err = checkMZ(dst)
	}
	if err != nil {
		os.Remove(dst)
		return fmt.Errorf("write temp file: %w", err)
	}
	if rec != nil {
		rec.Size, rec.SHA256 = n, hex.EncodeToString(h.Sum(nil))
	}
	return nil
}

func checkMZ(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var b [2]byte
	if _, err := io.ReadFull(f, b[:]); err != nil || b[0] != 'M' || b[1] != 'Z' {
		return errors.New("not a PE executable (missing MZ header)")
	}
	return nil
}

// UpdateTempPrefix starts the name of the downloaded file (the rest is random).
const UpdateTempPrefix = "supcua-v2-update-"

// ErrUpdateChanged: the downloaded file no longer matches what Download verified.
// ErrUpdatePath: the file is not one Download created (wrong directory or name); it is left alone.
var ErrUpdatePath = errors.New("update file is not a download of this application")

// ErrUpdateBatchPath: a path cannot be quoted safely in update.bat (", CR, LF or NUL).
var ErrUpdateBatchPath = errors.New("install path cannot be used in the update script")

var ErrUpdateChanged = errors.New("downloaded update changed on disk; refusing to install it")

// Downloaded identifies a verified download: Replace re-checks these exact bytes.
type Downloaded struct {
	Path   string
	Size   int64
	SHA256 string // hex of the bytes written, computed after the verified download
}

// Discard deletes the downloaded file (best effort).
func Discard(d Downloaded) { os.Remove(d.Path) }

func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return n, hex.EncodeToString(h.Sum(nil)), err
}

var staleName = regexp.MustCompile(`^` + regexp.QuoteMeta(UpdateTempPrefix) + `[0-9a-f]{16}\.exe$`)

const staleAfter = 24 * time.Hour

// sweepStale deletes leftovers of failed updates: only files whose name matches the random
// pattern AND that are older than 24 h, so a download of this or another running instance
// (always fresh) is never touched.
func sweepStale(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !staleName.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > staleAfter {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// Download fetches and verifies the new executable (trusted-host allowlist, size, sha256 when
// known, MZ header) into an unpredictably named file, created exclusively, next to exePath. The
// recorded size/sha256 come from the same streaming pass. It never touches the running
// executable, so the caller can shut down cleanly before Replace.
func Download(dlURL, exePath string) (Downloaded, error) {
	dir := filepath.Dir(exePath)
	sweepStale(dir, time.Now())
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return Downloaded{}, fmt.Errorf("random name: %w", err)
	}
	tmpExe := filepath.Join(dir, UpdateTempPrefix+hex.EncodeToString(rnd[:])+".exe")
	var rec downloadRecord
	if err := downloadRecorded(dlURL, tmpExe, &rec); err != nil {
		return Downloaded{}, err
	}
	return Downloaded{Path: tmpExe, Size: rec.Size, SHA256: rec.SHA256}, nil
}

// verify re-reads the file and refuses anything that differs from the verified download.
func (d Downloaded) verify(exePath string) error {
	n, sum, err := hashFile(d.Path)
	if err != nil || n != d.Size || !strings.EqualFold(sum, d.SHA256) {
		return ErrUpdateChanged
	}
	return checkMZ(d.Path)
}

// Replace schedules the swap of exePath with the verified tmpExe through a batch script and
// returns; the caller must exit promptly (the script force-kills this process after ~0 s and
// restarts the new exe). If the batch cannot be written or launched the original executable is
// put back and an error returned.
func Replace(d Downloaded, exePath string) error {
	dir := filepath.Dir(exePath)
	tmpExe := d.Path
	// A path we did not create is rejected WITHOUT deleting it (it may not be ours).
	if filepath.Dir(d.Path) != dir || !staleName.MatchString(filepath.Base(d.Path)) {
		return ErrUpdatePath
	}
	// Re-verify the exact bytes immediately before anything is touched (the file sat on disk
	// while the application shut down). Accepted residual window: between this check and the
	// batch's move, someone who can write into the install directory could swap the file — but
	// that someone can already replace supcua-v2.exe itself, so it is no escalation.
	if err := d.verify(exePath); err != nil {
		os.Remove(tmpExe)
		return err
	}

	pid := os.Getpid()
	oldExe := exePath + ".old"
	batContent, err := buildBatch(pid, tmpExe, exePath, oldExe)
	if err != nil { // before anything is touched; the download is ours and useless here
		os.Remove(tmpExe)
		return err
	}

	// Rename the running exe out of the way now (Windows allows renaming open files).
	// Then the batch just needs to move the update into place after we exit.
	os.Remove(oldExe) // clean up any leftover from previous update
	if err := os.Rename(exePath, oldExe); err != nil {
		return fmt.Errorf("rename current exe: %w", err)
	}
	restore := func() { os.Rename(oldExe, exePath) }

	batPath := filepath.Join(dir, "update.bat")

	if err := os.WriteFile(batPath, []byte(batContent), 0644); err != nil {
		restore()
		return fmt.Errorf("write batch: %w", err)
	}

	cmd := batchCommand(dir)
	if err := cmd.Start(); err != nil {
		restore()
		os.Remove(batPath)
		return fmt.Errorf("launch batch: %w", err)
	}
	return nil
}

// DownloadAndReplace downloads the new exe and schedules a replacement via a batch script, then exits.
// Callers that must run an orderly shutdown first use Download, shut down, then Replace.
func DownloadAndReplace(dlURL, exePath string) error {
	d, err := Download(dlURL, exePath)
	if err != nil {
		return err
	}
	if err := Replace(d, exePath); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// buildBatch is the replacement script. If moving the new executable into place fails, the old
// one is moved back and started, and nothing is deleted; .old is only deleted after success.
//
// Every path is embedded literally: delayed expansion is off (so "!" is plain text), "%" is
// doubled (the only character cmd expands inside a quoted batch argument), and every path is
// double-quoted (so &, |, <, >, ^, ( and spaces are plain text). A path cmd cannot quote at all
// (", CR, LF, NUL) is refused with ErrUpdateBatchPath.
func buildBatch(pid int, tmpExe, exePath, oldExe string) (string, error) {
	q := func(p string) (string, error) {
		if p == "" || strings.ContainsAny(p, "\"\r\n\x00") {
			return "", ErrUpdateBatchPath
		}
		return `"` + strings.ReplaceAll(p, "%", "%%") + `"`, nil
	}
	newQ, err := q(tmpExe)
	if err != nil {
		return "", err
	}
	exeQ, err := q(exePath)
	if err != nil {
		return "", err
	}
	oldQ, err := q(oldExe)
	if err != nil {
		return "", err
	}
	lines := []string{
		"@echo off",
		"setlocal DisableDelayedExpansion",
		"taskkill /pid " + strconv.Itoa(pid) + " /f >nul 2>&1",
		"timeout /t 2 /nobreak >nul",
		"move /y " + newQ + " " + exeQ,
		"if errorlevel 1 (",
		"  move /y " + oldQ + " " + exeQ,
		`  start "" ` + exeQ,
		`  del "%~f0"`,
		"  exit /b 1",
		")",
		`start "" ` + exeQ,
		"timeout /t 3 /nobreak >nul",
		"del " + oldQ,
		`del "%~f0"`,
	}
	return strings.Join(lines, "\r\n") + "\r\n", nil
}

// batchName is the launched script, relative to the install directory: no absolute path ever
// reaches a cmd command line. It needs no quoting (no spaces or cmd metacharacters), and the
// ".\" prefix makes cmd run the file in cmd.Dir instead of searching PATH.
const batchName = `.\update.bat`

// batchCommand starts update.bat detached, in dir.
func batchCommand(dir string) *exec.Cmd {
	cmd := exec.Command("cmd", "/c", "start", "/b", "cmd", "/c", batchName)
	cmd.Dir = dir
	return cmd
}
