//go:build windows

package ui

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/ui/webview"
	"github.com/yanmad27/supcua-v2/updater"
)

// Host implements bridge.Host. Hide/Show/Exit run on the UI thread (the bridge posts them);
// WriteClipboard/RebindHotkey run on the RPC worker; ApplyUpdate on the bridge's update goroutine.
type Host struct {
	UI             *webview.UI
	Mode           string // "main" or "notLicensed"
	Macs           []string
	Version        string
	RuntimeVersion string
	ExePath        string
	// OnExit runs the ordered application shutdown on the UI thread.
	OnExit func()
	// Rebind re-registers a fixed hotkey (global/overlay) and restores the previous one on failure.
	Rebind func(kind string, hk model.Hotkey) error

	mu        sync.Mutex
	updateURL string
}

func (h *Host) Hide() { h.UI.Hide() }
func (h *Host) Show() { h.UI.Show() }
func (h *Host) Exit() { h.OnExit() }

func (h *Host) WriteClipboard(text string) error { return WriteClipboard(text) }

func (h *Host) BootInfo() bridge.BootState {
	macs := h.Macs
	if macs == nil {
		macs = []string{}
	}
	return bridge.BootState{Mode: h.Mode, Version: h.Version, Macs: macs, RuntimeVersion: h.RuntimeVersion}
}

func (h *Host) RebindHotkey(kind string, hk model.Hotkey) error {
	if h.Rebind == nil {
		return errors.New("hotkey rebinding unavailable")
	}
	return h.Rebind(kind, hk)
}

// SetUpdate records the download URL of an available update (empty clears it).
func (h *Host) SetUpdate(dlURL string) {
	h.mu.Lock()
	h.updateURL = dlURL
	h.mu.Unlock()
}

// ApplyUpdate downloads and replaces the executable; it never assumes a URL exists and
// returns the updater's error unchanged for the bridge to report as UPDATE_FAILED.
func (h *Host) ApplyUpdate(progress func(pct int)) error {
	h.mu.Lock()
	url := h.updateURL
	h.mu.Unlock()
	if url == "" {
		return errors.New("no update available")
	}
	progress(0)
	if err := updater.DownloadAndReplace(url, h.ExePath); err != nil {
		return err
	}
	progress(100)
	return nil
}

func (h *Host) SkipUpdate() { h.SetUpdate("") }

// NotLicensedMethods is the whole API of the not-licensed screen: boot info (MAC list),
// copy to clipboard and exit. Nothing else of the application is reachable in this mode.
func NotLicensedMethods(h *Host) map[string]func(json.RawMessage) (any, error) {
	return map[string]func(json.RawMessage) (any, error){
		"app.getBootState": func(json.RawMessage) (any, error) { return h.BootInfo(), nil },
		"app.ready":        func(json.RawMessage) (any, error) { return struct{}{}, nil },
		"app.exit": func(json.RawMessage) (any, error) {
			h.UI.Post(h.Exit)
			return struct{}{}, nil
		},
		"clipboard.write": func(p json.RawMessage) (any, error) {
			var in struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(p, &in); err != nil || len(in.Text) > 64*1024 {
				return nil, &bridge.Error{Code: "BAD_REQUEST", Message: "invalid request"}
			}
			if err := WriteClipboard(in.Text); err != nil {
				return nil, &bridge.Error{Code: "INTERNAL", Message: "internal error"}
			}
			return struct{}{}, nil
		},
	}
}
