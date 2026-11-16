package bridge

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/yanmad27/supcua-v2/model"
)

func (b *Bridge) settingsGet() (any, error) {
	return settingsOf(b.d.Config.Get()), nil
}

func settingsOf(c model.AppConfig) SettingsDTO {
	groups := append([]string{}, c.CollapsedGroups...)
	return SettingsDTO{
		Theme: themeOf(c), GlobalHotkey: DisplayHotkey(c.GlobalHotkey), CaptureHotkey: DisplayHotkey(c.CaptureHotkey),
		OverlayHotkey: DisplayHotkey(c.OverlayHotkey), OverlayEnabled: c.OverlayEnabled,
		KeyPressDurationMs: c.KeyPressDurationMs, ScriptsDir: c.ScriptsDir,
		AutoActiveEnabled: c.AutoActiveEnabled, AutoActiveProcessName: c.AutoActiveProcessName,
		CollapsedGroups: groups,
	}
}

// validateScriptsDir accepts only a directory that exists or can be created and is writable.
// It touches nothing outside that directory (a probe file is created inside it and removed).
func validateScriptsDir(dir string) (string, bool) {
	if dir == "" || len(dir) > 1024 || strings.ContainsRune(dir, 0) {
		return "", false
	}
	clean := filepath.Clean(dir)
	if fi, err := os.Stat(clean); err == nil {
		if !fi.IsDir() {
			return "", false
		}
	} else if !os.IsNotExist(err) || os.MkdirAll(clean, 0o755) != nil {
		return "", false
	}
	probe, err := os.CreateTemp(clean, ".write-probe-*")
	if err != nil {
		return "", false
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return clean, true
}

func validProcessName(name string) bool {
	if len(name) > 260 || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (b *Bridge) settingsUpdate(p struct {
	Patch SettingsPatch `json:"patch"`
}) (any, error) {
	patch := p.Patch
	cfg := b.d.Config.Get()
	next := cfg

	var immediate, restart []string
	hotkeys := []struct {
		field, kind string
		val         *string
		dst         *model.Hotkey
		old         model.Hotkey
	}{
		{"global_hotkey", HotkeyGlobal, patch.GlobalHotkey, &next.GlobalHotkey, cfg.GlobalHotkey},
		{"overlay_hotkey", HotkeyOverlay, patch.OverlayHotkey, &next.OverlayHotkey, cfg.OverlayHotkey},
		{"capture_hotkey", HotkeyCapture, patch.CaptureHotkey, &next.CaptureHotkey, cfg.CaptureHotkey},
	}

	if patch.Theme != nil {
		switch *patch.Theme {
		case "light", "dark", "system":
		default:
			return nil, invalidSetting("theme")
		}
		if *patch.Theme != themeOf(cfg) {
			next.Theme = *patch.Theme
			immediate = append(immediate, "theme")
		}
	}
	var rebinds []int
	for i, h := range hotkeys {
		if h.val == nil {
			continue
		}
		hk, ok := ParseHotkey(*h.val)
		if !ok {
			return nil, invalidSetting(h.field)
		}
		*h.dst = hk
		if !b.checker.HotkeysEqual(hk, h.old) {
			rebinds = append(rebinds, i)
		}
	}
	for i := range hotkeys {
		for j := i + 1; j < len(hotkeys); j++ {
			if b.checker.HotkeysEqual(*hotkeys[i].dst, *hotkeys[j].dst) {
				return nil, invalidSetting(hotkeys[j].field)
			}
		}
	}
	if patch.KeyPressDurationMs != nil {
		if *patch.KeyPressDurationMs < 1 || *patch.KeyPressDurationMs > 1000 {
			return nil, invalidSetting("key_press_duration_ms")
		}
		if *patch.KeyPressDurationMs != cfg.KeyPressDurationMs {
			next.KeyPressDurationMs = *patch.KeyPressDurationMs
			restart = append(restart, "key_press_duration_ms")
		}
	}
	if patch.AutoActiveProcessName != nil {
		name := strings.TrimSpace(*patch.AutoActiveProcessName)
		if !validProcessName(name) {
			return nil, invalidSetting("auto_active_process_name")
		}
		next.AutoActiveProcessName = name
	}
	if patch.ScriptsDir != nil {
		clean, ok := validateScriptsDir(*patch.ScriptsDir)
		if !ok {
			return nil, invalidSetting("scripts_dir")
		}
		if clean != filepath.Clean(cfg.ScriptsDir) {
			next.ScriptsDir = clean
			restart = append(restart, "scripts_dir")
		}
	}
	if patch.OverlayEnabled != nil {
		next.OverlayEnabled = *patch.OverlayEnabled
	}
	if patch.AutoActiveEnabled != nil {
		next.AutoActiveEnabled = *patch.AutoActiveEnabled
	}

	var undo []func()
	rollback := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	for _, i := range rebinds {
		h := hotkeys[i]
		if b.d.Host == nil {
			rollback()
			return nil, newErr(CodeHotkeyRegisterFail)
		}
		if err := b.d.Host.RebindHotkey(h.kind, *h.dst); err != nil {
			log.Printf("bridge: rebind %s failed: %v", h.kind, err)
			rollback()
			return nil, newErr(CodeHotkeyRegisterFail)
		}
		host, kind, old := b.d.Host, h.kind, h.old
		undo = append(undo, func() { _ = host.RebindHotkey(kind, old) })
		immediate = append(immediate, h.field)
	}
	if next.OverlayEnabled != cfg.OverlayEnabled {
		if err := b.setOverlayEnabled(next.OverlayEnabled); err != nil {
			rollback()
			return nil, err
		}
		old := cfg.OverlayEnabled
		undo = append(undo, func() { _ = b.setOverlayEnabled(old) })
		immediate = append(immediate, "overlay_enabled")
	}
	immediate = append(immediate, b.applyAutoActive(cfg, next, &undo)...)

	if err := b.d.Config.Update(func(c *model.AppConfig) { *c = withPersisted(*c, next) }); err != nil {
		log.Printf("bridge: persist settings failed: %v", err)
		rollback()
		return nil, newErr(CodeSaveFailed)
	}
	b.refreshOverlay(true)
	if len(immediate)+len(restart) > 0 {
		b.emit(EvSettingsChanged, settingsChangedEvent{Fields: append(append([]string{}, immediate...), restart...)})
		b.NotifyStateChanged()
	}
	return SettingsUpdateResult{
		Applied: nonNil(immediate), RestartRequired: nonNil(restart), State: b.currentState(),
	}, nil
}

// withPersisted copies the settings fields next owns onto current, keeping fields other
// goroutines changed meanwhile (window bounds, collapsed groups).
func withPersisted(current, next model.AppConfig) model.AppConfig {
	current.Theme = next.Theme
	current.GlobalHotkey, current.CaptureHotkey, current.OverlayHotkey = next.GlobalHotkey, next.CaptureHotkey, next.OverlayHotkey
	current.OverlayEnabled = next.OverlayEnabled
	current.KeyPressDurationMs = next.KeyPressDurationMs
	current.ScriptsDir = next.ScriptsDir
	current.AutoActiveEnabled, current.AutoActiveProcessName = next.AutoActiveEnabled, next.AutoActiveProcessName
	return current
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (b *Bridge) setOverlayEnabled(enabled bool) error {
	if b.d.Overlay == nil {
		return nil
	}
	err := b.callUI(func() error { return b.d.Overlay.SetEnabled(enabled) })
	if err != nil {
		if _, ok := err.(*Error); ok {
			return err
		}
		log.Printf("bridge: overlay enable failed: %v", err)
		return newErr(CodeInternal)
	}
	return nil
}

// applyAutoActive pushes auto-active changes (old -> next) to the watcher and returns the changed fields.
func (b *Bridge) applyAutoActive(old, next model.AppConfig, undo *[]func()) []string {
	w := b.d.AutoActive
	if w == nil {
		return nil
	}
	var fields []string
	if next.AutoActiveProcessName != old.AutoActiveProcessName {
		w.SetProcessName(next.AutoActiveProcessName)
		prev := old.AutoActiveProcessName
		*undo = append(*undo, func() { w.SetProcessName(prev) })
		fields = append(fields, "auto_active_process_name")
	}
	if next.AutoActiveEnabled != old.AutoActiveEnabled {
		if next.AutoActiveEnabled {
			w.Start()
			*undo = append(*undo, w.Stop)
		} else {
			w.Stop()
			*undo = append(*undo, w.Start)
		}
		fields = append(fields, "auto_active_enabled")
	}
	return fields
}

func (b *Bridge) overlaySet(p struct {
	Enabled   *bool `json:"enabled"`
	Visible   *bool `json:"visible"`
	Draggable *bool `json:"draggable"`
}) (any, error) {
	if b.d.Overlay == nil {
		return nil, newErr(CodeInternal)
	}
	if p.Enabled != nil {
		b.refreshOverlay(true)
		was := b.overlayState().Enabled
		if err := b.setOverlayEnabled(*p.Enabled); err != nil {
			return nil, err
		}
		if err := b.d.Config.Update(func(c *model.AppConfig) { c.OverlayEnabled = *p.Enabled }); err != nil {
			log.Printf("bridge: persist overlay_enabled failed: %v", err)
			if rerr := b.setOverlayEnabled(was); rerr != nil {
				log.Printf("bridge: overlay rollback failed: %v", rerr)
			}
			b.refreshOverlay(true)
			return nil, newErr(CodeSaveFailed)
		}
	}
	if p.Visible != nil || p.Draggable != nil {
		err := b.callUI(func() error {
			if p.Visible != nil {
				b.d.Overlay.SetVisible(*p.Visible)
			}
			if p.Draggable != nil {
				b.d.Overlay.SetDraggable(*p.Draggable)
			}
			return nil
		})
		if err != nil {
			b.refreshOverlay(true)
			return nil, err
		}
	}
	b.refreshOverlay(true)
	b.NotifyStateChanged()
	return b.overlayState(), nil
}

func (b *Bridge) autoActiveSet(p struct {
	Enabled     *bool   `json:"enabled"`
	ProcessName *string `json:"processName"`
}) (any, error) {
	cfg := b.d.Config.Get()
	next := cfg
	if p.ProcessName != nil {
		name := strings.TrimSpace(*p.ProcessName)
		if !validProcessName(name) {
			return nil, invalidSetting("auto_active_process_name")
		}
		next.AutoActiveProcessName = name
	}
	if p.Enabled != nil {
		next.AutoActiveEnabled = *p.Enabled
	}
	var undo []func()
	fields := b.applyAutoActive(cfg, next, &undo)
	if err := b.d.Config.Update(func(c *model.AppConfig) {
		c.AutoActiveEnabled, c.AutoActiveProcessName = next.AutoActiveEnabled, next.AutoActiveProcessName
	}); err != nil {
		log.Printf("bridge: persist auto-active failed: %v", err)
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return nil, newErr(CodeSaveFailed)
	}
	if len(fields) > 0 {
		b.emit(EvSettingsChanged, settingsChangedEvent{Fields: fields})
		b.NotifyStateChanged()
	}
	return struct {
		Enabled     bool   `json:"enabled"`
		ProcessName string `json:"processName"`
	}{next.AutoActiveEnabled, next.AutoActiveProcessName}, nil
}
