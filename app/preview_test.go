package app

import (
	"testing"
	"time"
)

func previewCtl(t *testing.T) (*Controller, *fakeSim, *int) {
	c, sim := newCtl(t)
	n := 0
	c.SetPreviewNotifier(func() { n++ })
	return c, sim, &n
}

func TestPreview_ToggleShowsDisableThenCommitClearsIt(t *testing.T) {
	c, _, notified := previewCtl(t)
	tok := c.BeginTogglePreview(c.HotkeyEpoch())
	if tok == 0 || *notified == 0 {
		t.Fatalf("no preview/notification: tok=%d n=%d", tok, *notified)
	}
	if c.OverlaySnapshot().GlobalActive || !c.Snapshot().GlobalActive {
		t.Fatalf("overlay %+v real %+v: preview must show DISABLE without changing real state", c.OverlaySnapshot(), c.Snapshot())
	}
	c.ToggleGlobalFromHotkey(c.HotkeyEpoch())
	if c.IsGlobalActive() || c.OverlaySnapshot() != c.Snapshot() {
		t.Fatal("commit did not flip the state / clear the preview")
	}
}

func TestPreview_CancelRestoresTheRealState(t *testing.T) {
	c, _, _ := previewCtl(t)
	tok := c.BeginTogglePreview(c.HotkeyEpoch())
	c.CancelPreview(tok)
	if c.OverlaySnapshot() != c.Snapshot() || !c.IsGlobalActive() {
		t.Fatal("preview survived Cancel or changed the real state")
	}
	c.CancelPreview(tok) // idempotent
}

func TestPreview_StaleTokenCannotCancelANewerPreview(t *testing.T) {
	c, _, _ := previewCtl(t)
	old := c.BeginTogglePreview(c.HotkeyEpoch())
	c.CancelPreview(old)
	c.BeginTogglePreview(c.HotkeyEpoch())
	c.CancelPreview(old)
	if c.OverlaySnapshot().GlobalActive {
		t.Fatal("stale token cancelled the newer preview")
	}
}

func TestPreview_NotShownWhenGatesAreClosed(t *testing.T) {
	c, _, notified := previewCtl(t)
	epoch := c.HotkeyEpoch()

	c.SetEditDialogOpen(true)
	if c.BeginTogglePreview(epoch) != 0 {
		t.Fatal("preview with the editor open")
	}
	c.SetEditDialogOpen(false)

	c.SetHotkeyRecording(true)
	if c.BeginTogglePreview(c.HotkeyEpoch()) != 0 {
		t.Fatal("preview while a recording is active")
	}
	c.SetHotkeyRecording(false)

	if c.BeginTogglePreview(epoch) != 0 { // epoch bumped by the recording
		t.Fatal("preview with a stale epoch")
	}
	if c.OverlaySnapshot() != c.Snapshot() {
		t.Fatal("overlay differs from the real state although no preview was shown")
	}
	if *notified != 0 {
		t.Fatalf("overlay repainted for a preview that was never shown (%d)", *notified)
	}
}

func TestPreview_RecordingStartingDropsItAndBlocksTheCommit(t *testing.T) {
	c, _, _ := previewCtl(t)
	epoch := c.HotkeyEpoch()
	c.BeginTogglePreview(epoch)
	c.SetHotkeyRecording(true)
	if c.OverlaySnapshot() != c.Snapshot() {
		t.Fatal("preview survived a recording start")
	}
	c.SetHotkeyRecording(false)
	c.ToggleGlobalFromHotkey(epoch)
	if !c.IsGlobalActive() {
		t.Fatal("a release stamped before the recording toggled the state")
	}
}

func TestPreview_GatedReleaseClearsItAndRepaints(t *testing.T) {
	c, _, notified := previewCtl(t)
	epoch := c.HotkeyEpoch()
	tok := c.BeginTogglePreview(epoch)
	before := *notified
	c.SetHotkeyRecording(true)
	c.SetHotkeyRecording(false)
	c.ToggleGlobalFromHotkey(epoch)
	c.CancelPreview(tok) // the manager ends a gated release's preview by its token
	if c.OverlaySnapshot() != c.Snapshot() || *notified <= before {
		t.Fatalf("overlay not restored/repainted: %+v n=%d", c.OverlaySnapshot(), *notified)
	}
}

func TestPreview_StopShowsIdleOnlyForTheRunningScript(t *testing.T) {
	c, sim, _ := previewCtl(t)
	if c.BeginStopPreview("a", c.HotkeyEpoch()) != 0 {
		t.Fatal("stop preview with nothing running")
	}
	c.StartScript(infinite("a", false))
	eventually(t, "a iterating", func() bool { return sim.count("run:a") >= 2 })
	if c.BeginStopPreview("other", c.HotkeyEpoch()) != 0 {
		t.Fatal("stop preview for a script that is not running")
	}
	tok := c.BeginStopPreview("a", c.HotkeyEpoch())
	if tok == 0 || c.OverlaySnapshot().RunningScriptName != "" || c.Snapshot().RunningScriptName == "" {
		t.Fatalf("stop preview wrong: tok=%d overlay=%+v real=%+v", tok, c.OverlaySnapshot(), c.Snapshot())
	}
	c.StopRunningScriptFromHotkey(c.HotkeyEpoch())
	if c.OverlaySnapshot() != c.Snapshot() {
		t.Fatal("commit left the preview behind")
	}
}

func TestPreview_DroppedWhenTheStateChangesUnderneath(t *testing.T) {
	c, sim, _ := previewCtl(t)
	c.StartScript(quick("a"))
	eventually(t, "a ran", func() bool { return sim.count("run:a") == 1 })
	// The script ends on its own while RCtrl is held: the preview of "stop" must not stick.
	c.BeginStopPreview("a", c.HotkeyEpoch())
	eventually(t, "idle", idle(c))
	if got := c.OverlaySnapshot(); got != c.Snapshot() {
		t.Fatalf("preview stuck after the state changed: %+v", got)
	}

	c.BeginTogglePreview(c.HotkeyEpoch())
	c.SetGlobalActive(false) // the real state changed another way
	if got := c.OverlaySnapshot(); got != c.Snapshot() || got.GlobalActive {
		t.Fatalf("toggle preview stuck: %+v", got)
	}
	time.Sleep(time.Millisecond)
}

// Two previews overlap (A: toggle, B: stop): the latest begun wins and A's token is a no-op.
func overlap(t *testing.T) (c *Controller, a, b uint64, epoch uint64) {
	c, sim, _ := previewCtl(t)
	c.StartScript(infinite("s", false))
	eventually(t, "s iterating", func() bool { return sim.count("run:s") >= 2 })
	epoch = c.HotkeyEpoch()
	a = c.BeginTogglePreview(epoch)
	b = c.BeginStopPreview("s", epoch)
	if a == 0 || b == 0 || a == b {
		t.Fatalf("tokens a=%d b=%d", a, b)
	}
	return
}

func TestPreview_OverlapLatestWinsAndEarlierTokenIsNoOp(t *testing.T) {
	c, a, b, _ := overlap(t)
	if got := c.OverlaySnapshot(); !got.GlobalActive || got.RunningScriptName != "" {
		t.Fatalf("latest (stop) preview should show: %+v", got)
	}
	c.CancelPreview(a) // A's gated/unchanged release
	if got := c.OverlaySnapshot(); got.RunningScriptName != "" {
		t.Fatalf("A's cancel removed B's preview: %+v", got)
	}
	c.CancelPreview(b)
	if c.OverlaySnapshot() != c.Snapshot() {
		t.Fatal("B's own cancel did not end it")
	}
}

func TestPreview_OverlapAGatedReleaseKeepsB(t *testing.T) {
	c, a, _, epoch := overlap(t)
	c.ToggleGlobalFromHotkey(epoch + 1) // A's release is gated (stale epoch): no state change
	c.CancelPreview(a)                  // what the poll does after A's action returned
	if got := c.OverlaySnapshot(); got.RunningScriptName != "" || c.Snapshot().RunningScriptName == "" {
		t.Fatalf("B's preview lost by A's gated release: overlay=%+v real=%+v", got, c.Snapshot())
	}
}

func TestPreview_OverlapACommitDropsBOnlyBecauseTheStateChanged(t *testing.T) {
	c, a, _, epoch := overlap(t)
	c.ToggleGlobalFromHotkey(epoch) // A commits: global off, script stopped
	c.CancelPreview(a)
	if c.IsGlobalActive() {
		t.Fatal("A did not commit")
	}
	if c.OverlaySnapshot() != c.Snapshot() {
		t.Fatal("a preview computed from the old state outlived the commit")
	}
}
