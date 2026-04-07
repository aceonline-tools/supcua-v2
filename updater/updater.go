package updater

import (
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
	minioBase   = "https://dev.hrm.vangtatmining.com.vn/minio/bucket-supcua-v2"
	latestURL   = minioBase + "/latest.txt"
	downloadURL = minioBase + "/supcua-v2.exe"
)

// semverGT returns true if a > b using major.minor.patch comparison.
func semverGT(a, b string) bool {
	ap := parseSemver(a)
	bp := parseSemver(b)
	for i := 0; i < 3; i++ {
		if ap[i] > bp[i] {
			return true
		}
		if ap[i] < bp[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		out[i], _ = strconv.Atoi(parts[i])
	}
	return out
}

// CheckUpdate returns the latest version and download URL if a newer version exists.
func CheckUpdate(currentVersion string) (latestVersion, dlURL string, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(latestURL)
	if err != nil {
		return "", "", fmt.Errorf("fetch latest: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read latest: %w", err)
	}

	latest := strings.TrimSpace(string(body))
	if latest == "" {
		return "", "", nil
	}

	if !semverGT(latest, currentVersion) {
		return "", "", nil
	}

	return latest, downloadURL, nil
}

// DownloadAndReplace downloads the new exe and schedules a replacement via a batch script, then exits.
func DownloadAndReplace(dlURL, exePath string) error {
	dir := filepath.Dir(exePath)
	tmpExe := filepath.Join(dir, "supcua-v2-update.exe")

	// Remove stale temp file from a previous failed update
	os.Remove(tmpExe)

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(dlURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	f, err := os.Create(tmpExe)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	f.Close()

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
