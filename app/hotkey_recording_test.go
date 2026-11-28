package app

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestHotkeyRecordingBlocksScriptStartQueueAndToggles(t *testing.T) {
	c := NewController(nil, nil, nil, nil)
	c.SetHotkeyRecording(true)
	s := model.Script{ID: "s1", Name: "S", Enabled: true}
	if c.CanStartScript(s) {
		t.Fatal("CanStartScript during recording")
	}
	c.StartScript(s) // would launch on the nil executor and panic if not gated
	if c.GetRunningScriptID() != "" {
		t.Fatal("a script started during recording")
	}
	c.SetRunningScriptID("busy")
	c.StartScript(s)
	if c.GetQueueLength() != 0 {
		t.Fatal("a script was queued during recording")
	}
	c.SetRunningScriptID("")
	c.ToggleGlobalFromHotkey(c.HotkeyEpoch())
	if !c.IsGlobalActive() {
		t.Fatal("the global hotkey toggled during recording")
	}
	if c.RunHotkeyAction(c.HotkeyEpoch(), func() { t.Fatal("overlay toggle ran during recording") }) {
		t.Fatal("RunHotkeyAction reported running")
	}
	c.SetHotkeyRecording(false)
	c.ToggleGlobalFromHotkey(c.HotkeyEpoch())
	if c.IsGlobalActive() {
		t.Fatal("the global hotkey must work again after recording")
	}
	ran := false
	if !c.RunHotkeyAction(c.HotkeyEpoch(), func() { ran = true }) || !ran {
		t.Fatal("hotkey actions must run again after recording")
	}
}

func TestHotkeyRecordingOldEndDoesNotClearNewerGate(t *testing.T) {
	c := NewController(nil, nil, nil, nil)
	c.SetHotkeyRecording(true)  // capture A
	c.SetHotkeyRecording(true)  // capture B replaces A
	c.SetHotkeyRecording(false) // A's end
	c.ToggleGlobalFromHotkey(c.HotkeyEpoch())
	if !c.IsGlobalActive() {
		t.Fatal("the end of an older recording cleared the newer recording's gate")
	}
	c.SetHotkeyRecording(false)
	c.SetHotkeyRecording(false)
	c.SetHotkeyRecording(true)
	if c.RunHotkeyAction(c.HotkeyEpoch(), func() {}) {
		t.Fatal("an extra end must not leave a later recording ungated")
	}
}

func TestHotkeyRecordingStartWaitsForAnActionInProgress(t *testing.T) {
	c := NewController(nil, nil, nil, nil)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	finished := false
	go c.RunHotkeyAction(c.HotkeyEpoch(), func() {
		close(entered)
		<-release
		finished = true
	})
	<-entered
	go func() {
		c.SetHotkeyRecording(true)
		if !finished { // ordered by hotkeyActionMu; -race flags it if Set did not wait
			t.Error("SetHotkeyRecording(true) returned while an action was still running")
		}
		close(done)
	}()
	close(release)
	<-done
	if c.RunHotkeyAction(c.HotkeyEpoch(), func() { t.Error("action ran after SetHotkeyRecording(true) returned") }) {
		t.Fatal("ran")
	}
}

func TestHotkeyActionReenteringSetHotkeyRecordingDoesNotDeadlock(t *testing.T) {
	c := NewController(nil, nil, nil, nil)
	c.SetHotkeyRecording(true)
	c.SetHotkeyRecording(false)
	if !c.RunHotkeyAction(c.HotkeyEpoch(), func() { c.SetHotkeyRecording(false) }) {
		t.Fatal("action did not run")
	}
	c.SetRunningScriptID("busy")
	c.ToggleGlobalFromHotkey(c.HotkeyEpoch()) // goes inactive: StopRunningScript + notify re-take stateMutex
	c.SetHotkeyRecording(true)
	c.SetHotkeyRecording(false)
}

func TestHotkeyEventStampedBeforeRecordingNeverRunsAfterIt(t *testing.T) {
	c := NewController(nil, nil, nil, nil)
	stale := c.HotkeyEpoch() // detected on the listener thread
	c.SetHotkeyRecording(true)
	c.SetHotkeyRecording(false)
	c.SetRunningScriptID("busy")
	c.StartScriptFromHotkey(model.Script{ID: "s1", Enabled: true}, stale)
	if c.GetQueueLength() != 0 {
		t.Fatal("a stale script start was queued")
	}
	c.SetRunningScriptID("")
	c.StartScriptFromHotkey(model.Script{ID: "s1", Enabled: true}, stale) // nil executor: panics if launched
	c.ToggleGlobalFromHotkey(stale)
	if !c.IsGlobalActive() {
		t.Fatal("a stale global toggle ran")
	}
	if c.RunHotkeyAction(stale, func() { t.Error("stale overlay toggle ran") }) {
		t.Fatal("ran")
	}
	if !c.RunHotkeyAction(c.HotkeyEpoch(), func() {}) {
		t.Fatal("a fresh event must run")
	}
}

func TestScriptStopHotkeyIsGatedEmergencyStopIsNot(t *testing.T) {
	c := NewController(nil, nil, nil, nil)
	cancelled := 0
	queued := model.Script{ID: "q", Enabled: true}
	c.stateMutex.Lock()
	c.runningScriptID = "busy"
	c.cancelRunningScript = func() { cancelled++ }
	c.scriptQueue = []model.Script{queued}
	c.stateMutex.Unlock()
	stale := c.HotkeyEpoch()
	c.SetHotkeyRecording(true)
	c.StopRunningScriptFromHotkey(c.HotkeyEpoch())
	c.SetHotkeyRecording(false)
	c.StopRunningScriptFromHotkey(stale)
	if cancelled != 0 || c.GetQueueLength() != 1 {
		t.Fatalf("stop hotkey during/after recording acted: cancelled=%d queue=%d", cancelled, c.GetQueueLength())
	}
	c.StopRunningScriptFromHotkey(c.HotkeyEpoch())
	if cancelled != 1 || c.GetQueueLength() != 0 {
		t.Fatal("a fresh stop hotkey must stop")
	}
	c.SetHotkeyRecording(true)
	c.stateMutex.Lock()
	c.cancelRunningScript = func() { cancelled++ }
	c.stateMutex.Unlock()
	c.StopRunningScript() // emergency/tray path
	if cancelled != 2 {
		t.Fatal("the ungated stop must work during recording")
	}
}
