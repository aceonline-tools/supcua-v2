package model

// WindowPos stores a window's screen position and size.
type WindowPos struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

// EditorLayoutVersion is bumped when the editor layout changes materially: a size remembered under
// an older version is dropped once (the position is kept) and the measured size is used.
const EditorLayoutVersion = 1

// EditorWindow is the remembered bounds of the editor window, in physical pixels (contract §9).
// Zero W/H means "nothing remembered".
type EditorWindow struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	W         int  `json:"w"`
	H         int  `json:"h"`
	Maximized bool `json:"maximized,omitempty"`
	Version   int  `json:"version"`
}

// AppConfig holds global application settings.
type AppConfig struct {
	GlobalHotkey    Hotkey       `json:"global_hotkey"`
	CaptureHotkey   Hotkey       `json:"capture_hotkey"`
	OverlayHotkey   Hotkey       `json:"overlay_hotkey"`
	OverlayEnabled  bool         `json:"overlay_enabled"`
	OverlayPosition WindowPos    `json:"overlay_position"`
	OverlayStyle    OverlayStyle `json:"overlay_style"`
	MainWindowPos   WindowPos    `json:"main_window_pos"`
	// Deprecated: unused legacy field, still parsed and written as before (editor-window contract §9).
	EditDialogPos      WindowPos    `json:"edit_dialog_pos"`
	EditorWindow       EditorWindow `json:"editor_window,omitempty"`
	ScriptsDir         string       `json:"scripts_dir"`
	KeyPressDurationMs int          `json:"key_press_duration_ms"`

	AutoActiveEnabled     bool   `json:"auto_active_enabled"`
	AutoActiveProcessName string `json:"auto_active_process_name"`

	Theme           string   `json:"theme,omitempty"`
	Language        string   `json:"language,omitempty"`
	CollapsedGroups []string `json:"collapsed_groups,omitempty"`

	SidebarCollapsed bool `json:"sidebar_collapsed,omitempty"`
	// WindowLayout is the window-size layout version MainWindowPos was saved under (0 = before it existed).
	WindowLayout int `json:"window_layout,omitempty"`
}

// WindowLayoutVersion is bumped when the default window size changes so a size saved by an older
// build is replaced once by the new default (position kept) and user resizes are respected after.
const WindowLayoutVersion = 2

// WindowLayoutStale reports whether the saved window size predates the current layout version.
func (c AppConfig) WindowLayoutStale() bool { return c.WindowLayout < WindowLayoutVersion }

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
		Theme:              "system",
	}
}
