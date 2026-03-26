package model

// Hotkey represents a keyboard shortcut with optional modifiers.
// Modifiers can be empty for single-key hotkeys (e.g., F5).
type Hotkey struct {
	Modifiers []string `json:"modifiers"`
	Key       string   `json:"key"`
}
