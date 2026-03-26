package hotkey

import (
	"sort"
	"strings"

	"github.com/yanmad27/supcua-v2/model"
)

// ConflictChecker detects hotkey conflicts across scripts.
type ConflictChecker struct{}

// NewConflictChecker creates a ConflictChecker.
func NewConflictChecker() *ConflictChecker {
	return &ConflictChecker{}
}

// HotkeysEqual returns true if two hotkeys represent the same key combination.
// Modifier order does not matter.
func (conflictChecker *ConflictChecker) HotkeysEqual(hotkeyA, hotkeyB model.Hotkey) bool {
	if strings.ToLower(hotkeyA.Key) != strings.ToLower(hotkeyB.Key) {
		return false
	}

	normalizedModifiersA := normalizeModifiers(hotkeyA.Modifiers)
	normalizedModifiersB := normalizeModifiers(hotkeyB.Modifiers)

	if len(normalizedModifiersA) != len(normalizedModifiersB) {
		return false
	}
	for modifierIndex := range normalizedModifiersA {
		if normalizedModifiersA[modifierIndex] != normalizedModifiersB[modifierIndex] {
			return false
		}
	}
	return true
}

func normalizeModifiers(modifierList []string) []string {
	normalized := make([]string, len(modifierList))
	for index, modifierName := range modifierList {
		normalized[index] = strings.ToLower(modifierName)
	}
	sort.Strings(normalized)
	return normalized
}

// FindConflict checks if a hotkey conflicts with any script in the list.
// excludeScriptID is the script being edited (don't conflict with self).
// Returns the conflicting script and which field conflicts, or nil if no conflict.
func (conflictChecker *ConflictChecker) FindConflict(
	candidateHotkey model.Hotkey,
	allScripts []model.Script,
	excludeScriptID string,
) (*model.Script, string) {
	for scriptIndex := range allScripts {
		currentScript := &allScripts[scriptIndex]
		if currentScript.ID == excludeScriptID {
			continue
		}

		if currentScript.ToggleHotkey != nil && conflictChecker.HotkeysEqual(candidateHotkey, *currentScript.ToggleHotkey) {
			return currentScript, "toggle_hotkey"
		}
		if currentScript.StartHotkey != nil && conflictChecker.HotkeysEqual(candidateHotkey, *currentScript.StartHotkey) {
			return currentScript, "start_hotkey"
		}
		if currentScript.StopHotkey != nil && conflictChecker.HotkeysEqual(candidateHotkey, *currentScript.StopHotkey) {
			return currentScript, "stop_hotkey"
		}
	}
	return nil, ""
}

// ClearConflictingHotkey sets the conflicting hotkey field to nil on the given script.
func (conflictChecker *ConflictChecker) ClearConflictingHotkey(script *model.Script, fieldName string) {
	switch fieldName {
	case "toggle_hotkey":
		script.ToggleHotkey = nil
	case "start_hotkey":
		script.StartHotkey = nil
	case "stop_hotkey":
		script.StopHotkey = nil
	}
}
