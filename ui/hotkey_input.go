//go:build windows

package ui

import (
	"fmt"
	"strings"

	"github.com/lxn/walk"

	"github.com/yanmad27/supcua-v2/model"
)

// HotkeyInputWidget displays the current hotkey and captures a new one on click.
type HotkeyInputWidget struct {
	displayLabel   *walk.LineEdit
	capturedHotkey *model.Hotkey
	isCapturing    bool
}

// FormatHotkey returns a human-readable string for a hotkey.
func FormatHotkey(hotkeyValue *model.Hotkey) string {
	if hotkeyValue == nil {
		return "(none)"
	}
	parts := make([]string, 0, len(hotkeyValue.Modifiers)+1)
	for _, modifierName := range hotkeyValue.Modifiers {
		parts = append(parts, capitalizeFirstLetter(modifierName))
	}
	parts = append(parts, strings.ToUpper(hotkeyValue.Key))
	return strings.Join(parts, " + ")
}

func capitalizeFirstLetter(inputString string) string {
	if len(inputString) == 0 {
		return inputString
	}
	return fmt.Sprintf("%s%s", strings.ToUpper(inputString[:1]), inputString[1:])
}

// GetCapturedHotkey returns the last captured hotkey.
func (hotkeyInput *HotkeyInputWidget) GetCapturedHotkey() *model.Hotkey {
	return hotkeyInput.capturedHotkey
}

// SetHotkey sets the displayed hotkey.
func (hotkeyInput *HotkeyInputWidget) SetHotkey(hotkeyValue *model.Hotkey) {
	hotkeyInput.capturedHotkey = hotkeyValue
	if hotkeyInput.displayLabel != nil {
		hotkeyInput.displayLabel.SetText(FormatHotkey(hotkeyValue))
	}
}
