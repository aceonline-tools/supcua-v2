package model

// WindowPos stores a window's screen position.
type WindowPos struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// AppConfig holds global application settings.
type AppConfig struct {
	GlobalHotkey    Hotkey    `json:"global_hotkey"`
	CaptureHotkey   Hotkey    `json:"capture_hotkey"`
	OverlayEnabled  bool      `json:"overlay_enabled"`
	OverlayPosition WindowPos `json:"overlay_position"`
	MainWindowPos   WindowPos `json:"main_window_pos"`
	EditDialogPos   WindowPos `json:"edit_dialog_pos"`
	ScriptsDir      string    `json:"scripts_dir"`
}

// DefaultConfig returns sensible defaults for a fresh install.
func DefaultConfig() AppConfig {
	return AppConfig{
		GlobalHotkey:    Hotkey{Modifiers: []string{}, Key: "rctrl"},
		CaptureHotkey:   Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "f2"},
		OverlayEnabled:  true,
		OverlayPosition: WindowPos{X: 10, Y: 10},
		ScriptsDir:      "./scripts",
	}
}
