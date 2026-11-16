package model

import "testing"

func sampleScript() Script {
	return Script{
		ID:           "original-id",
		Name:         "Farm",
		Enabled:      true,
		Group:        "Games",
		RunMode:      RunModeInfinite,
		HotkeyMode:   HotkeyModeSeparate,
		ToggleHotkey: &Hotkey{Modifiers: []string{"Ctrl"}, Key: "F1"},
		StartHotkey:  &Hotkey{Modifiers: []string{"Alt"}, Key: "F2"},
		StopHotkey:   &Hotkey{Key: "F3"},
		Actions: []Action{
			{Type: ActionLoop, LoopCount: 3, Actions: []Action{
				{Type: ActionKeyPress, Key: "a"},
			}},
			{
				Type:         ActionIfImage,
				SearchRegion: &Region{X: 1, Y: 2, Width: 3, Height: 4},
				ThenActions:  []Action{{Type: ActionDelay, DelayMs: 10}},
				ElseActions:  []Action{{Type: ActionKeyDown, Key: "b"}},
			},
		},
	}
}

func neverTaken(string) bool { return false }

func TestCloneScriptBasicFields(t *testing.T) {
	original := sampleScript()
	clone := CloneScript(original, neverTaken)

	if clone.ID == "" || clone.ID == original.ID {
		t.Errorf("expected new unique ID, got %q", clone.ID)
	}
	if clone.Name != "Farm (copy)" {
		t.Errorf("name = %q", clone.Name)
	}
	if clone.Enabled {
		t.Error("clone must be disabled")
	}
	if clone.Group != original.Group {
		t.Errorf("group = %q, want %q", clone.Group, original.Group)
	}
	if clone.RunMode != original.RunMode || clone.HotkeyMode != original.HotkeyMode {
		t.Error("run/hotkey mode should be preserved")
	}
	if clone.ToggleHotkey != nil || clone.StartHotkey != nil || clone.StopHotkey != nil {
		t.Error("hotkeys must be cleared")
	}
	if original.ToggleHotkey == nil || original.StartHotkey == nil || original.StopHotkey == nil || !original.Enabled {
		t.Error("original must be untouched")
	}
}

func TestCloneScriptNameDisambiguation(t *testing.T) {
	taken := map[string]bool{"Farm (copy)": true, "Farm (copy 2)": true}
	clone := CloneScript(sampleScript(), func(name string) bool { return taken[name] })
	if clone.Name != "Farm (copy 3)" {
		t.Errorf("name = %q, want Farm (copy 3)", clone.Name)
	}

	taken = map[string]bool{"Farm (copy)": true}
	clone = CloneScript(sampleScript(), func(name string) bool { return taken[name] })
	if clone.Name != "Farm (copy 2)" {
		t.Errorf("name = %q, want Farm (copy 2)", clone.Name)
	}
}

func TestCloneScriptDeepCopyIndependence(t *testing.T) {
	original := sampleScript()
	clone := CloneScript(original, neverTaken)

	clone.Actions[0].LoopCount = 99
	clone.Actions[0].Actions[0].Key = "z"
	clone.Actions[1].SearchRegion.X = 500
	clone.Actions[1].ThenActions[0].DelayMs = 999
	clone.Actions[1].ElseActions[0].Key = "q"
	clone.Actions[0].Actions = append(clone.Actions[0].Actions, Action{Type: ActionDelay})
	clone.Actions = append(clone.Actions, Action{Type: ActionDelay})

	want := sampleScript()
	if len(original.Actions) != 2 || len(original.Actions[0].Actions) != 1 {
		t.Fatal("original slice lengths changed")
	}
	if original.Actions[0].LoopCount != want.Actions[0].LoopCount ||
		original.Actions[0].Actions[0].Key != "a" ||
		original.Actions[1].SearchRegion.X != 1 ||
		original.Actions[1].ThenActions[0].DelayMs != 10 ||
		original.Actions[1].ElseActions[0].Key != "b" {
		t.Errorf("original mutated: %+v", original.Actions)
	}
}

func TestCloneScriptNilActionsAndNilPredicate(t *testing.T) {
	clone := CloneScript(Script{Name: "Empty"}, nil)
	if clone.Actions != nil || clone.Name != "Empty (copy)" {
		t.Errorf("unexpected clone: %+v", clone)
	}
}
