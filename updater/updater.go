package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	ErrRateLimited = errors.New("github api rate limited")
	ErrNoAsset     = errors.New("latest release has no " + assetName + " asset")
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
		if p == "" || len(p) > 9 {
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
// reported for: no releases (404), draft/pre-release, non-semver tag, or a
// version that is not newer. Single request, no retries.
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
		return "", "", nil
	case resp.StatusCode != http.StatusOK:
		return "", "", fmt.Errorf("fetch latest: unexpected status %d", resp.StatusCode)
	}

	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBodySize)).Decode(&rel); err != nil {
		return "", "", fmt.Errorf("parse latest: %w", err)
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
	return strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v"), a.BrowserDownloadURL, nil
}

// downloadTo fetches url into dst. A non-200 status or a body shorter/longer
// than Content-Length is an error and the partial file is removed.
func downloadTo(url, dst string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := (&http.Client{Timeout: downloadTO}).Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	n, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && resp.ContentLength >= 0 && n != resp.ContentLength {
		err = fmt.Errorf("size mismatch: got %d, want %d", n, resp.ContentLength)
	}
	if err == nil && n == 0 {
		err = errors.New("empty download")
	}
	if err != nil {
		os.Remove(dst)
		return fmt.Errorf("write temp file: %w", err)
	}
	return nil
}

// DownloadAndReplace downloads the new exe and schedules a replacement via a batch script, then exits.
func DownloadAndReplace(dlURL, exePath string) error {
	dir := filepath.Dir(exePath)
	tmpExe := filepath.Join(dir, "supcua-v2-update.exe")

	// Remove stale temp file from a previous failed update
	os.Remove(tmpExe)

	if err := downloadTo(dlURL, tmpExe); err != nil {
		return err
	}

	// Rename the running exe out of the way now (Windows allows renaming open files).
	// Then the batch just needs to move the update into place after we exit.
	oldExe := exePath + ".old"
	os.Remove(oldExe) // clean up any leftover from previous update
	if err := os.Rename(exePath, oldExe); err != nil {
		return fmt.Errorf("rename current exe: %w", err)
	}

	pid := os.Getpid()
	batPath := filepath.Join(dir, "update.bat")
	batContent := fmt.Sprintf(`@echo off
taskkill /pid %d /f >nul 2>&1
timeout /t 2 /nobreak >nul
move /y "%s" "%s"
start "" "%s"
timeout /t 3 /nobreak >nul
del "%s"
del "%%~f0"
`, pid, tmpExe, exePath, exePath, oldExe)

	if err := os.WriteFile(batPath, []byte(batContent), 0644); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}

	cmd := exec.Command("cmd", "/c", "start", "/b", "cmd", "/c", batPath)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch batch: %w", err)
	}

	os.Exit(0)
	return nil
}
