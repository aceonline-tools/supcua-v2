package app

import (
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/app/status"
)

// A stopped script must leave the status snapshot at once, not after dequeueDelay, while the slot
// stays held so a new start is still refused until the real dequeue.
func TestStop_SnapshotClearsBeforeSlotFrees(t *testing.T) {
	old := dequeueDelay
	dequeueDelay = 400 * time.Millisecond
	t.Cleanup(func() { dequeueDelay = old }) // registered before newCtl: runs after its idle wait

	c, sim := newCtl(t)
	var got []status.Snapshot
	c.Subscribe(func(s status.Snapshot) { got = append(got, s) })
	c.StartScript(infinite("a", false))
	eventually(t, "a iterating", func() bool { return sim.count("run:a") >= 2 })
	if c.Snapshot().RunningScriptName == "" {
		t.Fatal("running script missing from snapshot")
	}

	stopped := time.Now()
	c.StopRunningScript()
	if s := c.Snapshot(); s.RunningScriptID != "" || s.RunningScriptName != "" {
		t.Fatalf("snapshot still running right after stop: %+v", s)
	}
	if time.Since(stopped) > 100*time.Millisecond {
		t.Fatal("stop did not return promptly")
	}
	if last, _ := c.hub.Last(); last.RunningScriptName != "" {
		t.Fatalf("stop was not published: %+v", last)
	}
	if c.GetRunningScriptID() == "" {
		t.Fatal("slot freed before dequeueDelay")
	}
	eventually(t, "slot freed", idle(c))
	if c.Snapshot().RunningScriptName != "" {
		t.Fatal("snapshot running after the slot freed")
	}
	c.StartScript(quick("b"))
	eventually(t, "b ran after the stop", func() bool { return sim.count("run:b") == 1 })
}
