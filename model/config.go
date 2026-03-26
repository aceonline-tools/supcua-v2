package model

// AppConfig holds global application settings.
type AppConfig struct {
	GlobalHotkey   Hotkey `json:"global_hotkey"`
	CaptureHotkey  Hotkey `json:"capture_hotkey"`
	OverlayEnabled bool   `json:"overlay_enabled"`
	OverlayPosition struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"overlay_position"`
	ScriptsDir string `json:"scripts_dir"`
}

// DefaultConfig returns sensible defaults for a fresh install.
func DefaultConfig() AppConfig {
	return AppConfig{
		GlobalHotkey:   Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "f1"},
		CaptureHotkey:  Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "f2"},
		OverlayEnabled: true,
		OverlayPosition: struct {
			X int `json:"x"`
			Y int `json:"y"`
		}{X: 10, Y: 10},
		ScriptsDir: "./scripts",
	}
}
