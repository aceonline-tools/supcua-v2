package app

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestGlobalStatusToggle(t *testing.T) {
	appController := NewController(nil, nil, nil, nil)

	if !appController.IsGlobalActive() {
		t.Error("expected active by default")
	}

	appController.ToggleGlobalStatus()

	if appController.IsGlobalActive() {
		t.Error("expected inactive after toggle")
	}

	appController.ToggleGlobalStatus()

	if !appController.IsGlobalActive() {
		t.Error("expected active after second toggle")
	}
}

func TestCanStartScript(t *testing.T) {
	appController := NewController(nil, nil, nil, nil)

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

func TestEnqueueDedupsByID(t *testing.T) {
	c := NewController(nil, nil, nil, nil)
	c.enqueueLocked(model.Script{ID: "a"})
	c.enqueueLocked(model.Script{ID: "b"})
	c.enqueueLocked(model.Script{ID: "a"})
	if got := c.GetQueueLength(); got != 2 {
		t.Fatalf("queue len = %d, want 2", got)
	}
}

func TestDequeueZeroesConsumedAndResets(t *testing.T) {
	c := NewController(nil, nil, nil, nil)
	c.enqueueLocked(model.Script{ID: "a", Name: "A"})
	c.enqueueLocked(model.Script{ID: "b", Name: "B"})
	backing := c.scriptQueue

	first, ok := c.popQueueLocked()
	if !ok || first.ID != "a" {
		t.Fatalf("pop = %v %v", first, ok)
	}
	if backing[0].ID != "" || backing[0].Name != "" {
		t.Fatal("consumed entry not zeroed")
	}
	if _, ok := c.popQueueLocked(); !ok {
		t.Fatal("expected second pop")
	}
	if c.scriptQueue != nil {
		t.Fatal("queue not reset when empty")
	}
	if _, ok := c.popQueueLocked(); ok {
		t.Fatal("pop on empty returned ok")
	}
}
