package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const apiURL = "https://api.github.com/repos/aceonline-tools/supcua-v2/releases/latest"

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckUpdate returns the latest version tag and download URL, or empty strings if up to date.
func CheckUpdate(currentVersion string) (latestTag, downloadURL string, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	current := strings.TrimPrefix(currentVersion, "v")

	if latest == "" || latest == current {
		return "", "", nil
	}

	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, "supcua-v2.exe") {
			return rel.TagName, a.BrowserDownloadURL, nil
		}
	}
	return rel.TagName, "", nil
}

// DownloadAndReplace downloads the new exe and schedules a replacement via a batch script, then exits.
func DownloadAndReplace(downloadURL, exePath string) error {
	dir := filepath.Dir(exePath)
	tmpExe := filepath.Join(dir, "supcua-v2-update.exe")

	// Download new exe
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(downloadURL)
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

	// Write update.bat
	batPath := filepath.Join(dir, "update.bat")
	exeName := filepath.Base(exePath)
	batContent := fmt.Sprintf(`@echo off
timeout /t 2 /nobreak >nul
copy /y "%s" "%s"
del "%s"
start "" "%s"
del "%%~f0"
`, tmpExe, exePath, tmpExe, exeName)

	if err := os.WriteFile(batPath, []byte(batContent), 0644); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}

	// Launch batch and exit
	cmd := exec.Command("cmd", "/c", "start", "", batPath)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch batch: %w", err)
	}

	os.Exit(0)
	return nil
}
