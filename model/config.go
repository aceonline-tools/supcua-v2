package model

// WindowPos stores a window's screen position and size.
type WindowPos struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

// AppConfig holds global application settings.
type AppConfig struct {
	GlobalHotkey        Hotkey    `json:"global_hotkey"`
	CaptureHotkey       Hotkey    `json:"capture_hotkey"`
	OverlayHotkey       Hotkey    `json:"overlay_hotkey"`
	OverlayEnabled      bool      `json:"overlay_enabled"`
	OverlayPosition     WindowPos `json:"overlay_position"`
	MainWindowPos       WindowPos `json:"main_window_pos"`
	EditDialogPos       WindowPos `json:"edit_dialog_pos"`
	ScriptsDir          string    `json:"scripts_dir"`
	KeyPressDurationMs  int       `json:"key_press_duration_ms"`
}

// DefaultConfig returns sensible defaults for a fresh install.
func DefaultConfig() AppConfig {
	return AppConfig{
		GlobalHotkey:       Hotkey{Modifiers: []string{}, Key: "rctrl"},
		CaptureHotkey:      Hotkey{Modifiers: []string{}, Key: "p"},
		OverlayHotkey:      Hotkey{Modifiers: []string{"ctrl"}, Key: "o"},
		OverlayEnabled:     true,
		OverlayPosition:    WindowPos{X: 10, Y: 10},
		ScriptsDir:         "./scripts",
		KeyPressDurationMs: 10,
	}
}
