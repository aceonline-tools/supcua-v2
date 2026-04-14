package model

const (
	RunModeOnce     = "once"
	RunModeInfinite = "infinite"

	HotkeyModeToggle   = "toggle"
	HotkeyModeSeparate = "separate"
)

// Script represents a user-created automation script.
type Script struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	Group        string   `json:"group,omitempty"`
	RunMode      string   `json:"run_mode"`
	HotkeyMode   string   `json:"hotkey_mode"`
	ToggleHotkey *Hotkey  `json:"toggle_hotkey,omitempty"`
	StartHotkey  *Hotkey  `json:"start_hotkey,omitempty"`
	StopHotkey   *Hotkey  `json:"stop_hotkey,omitempty"`
	Actions      []Action `json:"actions"`
}
