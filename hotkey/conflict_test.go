package hotkey

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestFindConflictingScript(t *testing.T) {
	conflictChecker := NewConflictChecker()

	scriptAlpha := model.Script{
		ID:           "script-alpha",
		Name:         "Alpha",
		HotkeyMode:   model.HotkeyModeToggle,
		ToggleHotkey: &model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "1"},
	}
	scriptBeta := model.Script{
		ID:           "script-beta",
		Name:         "Beta",
		HotkeyMode:   model.HotkeyModeSeparate,
		StartHotkey:  &model.Hotkey{Modifiers: []string{"ctrl"}, Key: "f5"},
		StopHotkey:   &model.Hotkey{Modifiers: []string{"ctrl"}, Key: "f6"},
	}

	conflictingScript, conflictingField := conflictChecker.FindConflict(
		model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "1"},
		[]model.Script{scriptAlpha, scriptBeta},
		"script-beta", // exclude self
	)

	if conflictingScript == nil {
		t.Fatal("expected to find a conflict with script-alpha")
	}
	if conflictingScript.ID != "script-alpha" {
		t.Errorf("expected conflict with script-alpha, got %s", conflictingScript.ID)
	}
	if conflictingField != "toggle_hotkey" {
		t.Errorf("expected field toggle_hotkey, got %s", conflictingField)
	}
}

func TestNoConflict(t *testing.T) {
	conflictChecker := NewConflictChecker()

	scriptAlpha := model.Script{
		ID:           "script-alpha",
		Name:         "Alpha",
		HotkeyMode:   model.HotkeyModeToggle,
		ToggleHotkey: &model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "1"},
	}

	conflictingScript, _ := conflictChecker.FindConflict(
		model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "2"},
		[]model.Script{scriptAlpha},
		"script-beta",
	)

	if conflictingScript != nil {
		t.Errorf("expected no conflict, got conflict with %s", conflictingScript.ID)
	}
}

func TestHotkeysEqual(t *testing.T) {
	conflictChecker := NewConflictChecker()

	testCases := []struct {
		name     string
		hotkeyA  model.Hotkey
		hotkeyB  model.Hotkey
		expected bool
	}{
		{"same combo", model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "1"}, model.Hotkey{Modifiers: []string{"shift", "ctrl"}, Key: "1"}, true},
		{"different key", model.Hotkey{Modifiers: []string{"ctrl"}, Key: "1"}, model.Hotkey{Modifiers: []string{"ctrl"}, Key: "2"}, false},
		{"different mods", model.Hotkey{Modifiers: []string{"ctrl"}, Key: "1"}, model.Hotkey{Modifiers: []string{"shift"}, Key: "1"}, false},
		{"single keys equal", model.Hotkey{Key: "f5"}, model.Hotkey{Key: "f5"}, true},
		{"single keys diff", model.Hotkey{Key: "f5"}, model.Hotkey{Key: "f6"}, false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := conflictChecker.HotkeysEqual(testCase.hotkeyA, testCase.hotkeyB)
			if result != testCase.expected {
				t.Errorf("expected %v, got %v", testCase.expected, result)
			}
		})
	}
}
