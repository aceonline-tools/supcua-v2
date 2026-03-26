package app

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestGlobalStatusToggle(t *testing.T) {
	appController := NewController(nil, nil, nil, nil)

	if appController.IsGlobalActive() {
		t.Error("expected inactive by default")
	}

	appController.ToggleGlobalStatus()

	if !appController.IsGlobalActive() {
		t.Error("expected active after toggle")
	}

	appController.ToggleGlobalStatus()

	if appController.IsGlobalActive() {
		t.Error("expected inactive after second toggle")
	}
}

func TestCanStartScript(t *testing.T) {
	appController := NewController(nil, nil, nil, nil)
	appController.ToggleGlobalStatus() // set active

	enabledScript := model.Script{ID: "s1", Enabled: true}
	disabledScript := model.Script{ID: "s2", Enabled: false}

	if !appController.CanStartScript(enabledScript) {
		t.Error("expected enabled script to be startable when global active")
	}

	if appController.CanStartScript(disabledScript) {
		t.Error("expected disabled script to NOT be startable")
	}

	appController.ToggleGlobalStatus() // set inactive

	if appController.CanStartScript(enabledScript) {
		t.Error("expected script to NOT be startable when global inactive")
	}
}

func TestOnlyOneScriptRunsAtATime(t *testing.T) {
	appController := NewController(nil, nil, nil, nil)
	appController.ToggleGlobalStatus()

	appController.SetRunningScriptID("script-1")

	scriptTwo := model.Script{ID: "script-2", Enabled: true}
	if appController.CanStartScript(scriptTwo) {
		t.Error("expected script-2 to NOT be startable while script-1 is running")
	}

	appController.ClearRunningScript()

	if !appController.CanStartScript(scriptTwo) {
		t.Error("expected script-2 to be startable after clearing")
	}
}
