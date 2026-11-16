package app

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/model"
)

func TestMain(m *testing.M) {
	dequeueDelay = 5 * time.Millisecond
	os.Exit(m.Run())
}

type fakeSim struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeSim) rec(e string) {
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
}

func (f *fakeSim) count(e string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, g := range f.events {
		if g == e {
			n++
		}
	}
	return n
}

func (f *fakeSim) order() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func (f *fakeSim) MoveMouse(x, y int) error { return nil }
func (f *fakeSim) GetCursorPos() (int, int) { return 0, 0 }
func (f *fakeSim) MouseLeftDown()           {}
func (f *fakeSim) MouseLeftUp()             {}
func (f *fakeSim) MouseRightDown()          {}
func (f *fakeSim) MouseRightUp()            {}
func (f *fakeSim) KeyDown(k string) error {
	f.rec("run:" + k)
	return nil
}
func (f *fakeSim) KeyUp(k string) error { return nil }
func (f *fakeSim) LockMouse()           {}
func (f *fakeSim) UnlockMouse()         {}
func (f *fakeSim) LockKeyboard()        {}
func (f *fakeSim) UnlockKeyboard()      {}
func (f *fakeSim) UnlockAll()           { f.rec("unlockall") }

func newCtl(t *testing.T) (*Controller, *fakeSim) {
	t.Helper()
	sim := &fakeSim{}
	c := NewController(executor.NewExecutor(sim, nil, 1), nil, nil, nil)
	t.Cleanup(func() {
		c.StopRunningScript()
		eventually(t, "controller idle at teardown", func() bool { return c.GetRunningScriptID() == "" })
	})
	return c, sim
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func marker(id string) model.Action { return model.Action{Type: model.ActionKeyDown, Key: id} }

func quick(id string) model.Script {
	return model.Script{ID: id, Name: id, Enabled: true, RunMode: model.RunModeOnce, Actions: []model.Action{marker(id)}}
}

func blocking(id string) model.Script {
	return model.Script{ID: id, Name: id, Enabled: true, RunMode: model.RunModeOnce, Actions: []model.Action{
		marker(id), {Type: model.ActionDelay, DelayMs: 3600000}}}
}

func infinite(id string, empty bool) model.Script {
	s := model.Script{ID: id, Name: id, Enabled: true, RunMode: model.RunModeInfinite}
	if !empty {
		s.Actions = []model.Action{marker(id)}
	}
	return s
}

func idle(c *Controller) func() bool {
	return func() bool { return c.GetRunningScriptID() == "" }
}

func TestStart_RunsAndClears(t *testing.T) {
	c, sim := newCtl(t)
	c.StartScript(quick("a"))
	eventually(t, "script ran", func() bool { return sim.count("run:a") == 1 })
	eventually(t, "idle", idle(c))
	if sim.count("unlockall") != 1 {
		t.Fatalf("unlockall = %d", sim.count("unlockall"))
	}
	if c.GetRunningScriptName() != "" {
		t.Fatal("name not cleared")
	}
}

func TestRunningNameSnakeCase(t *testing.T) {
	c, _ := newCtl(t)
	s := blocking("a")
	s.Name = "My Script 1"
	c.StartScript(s)
	if got := c.GetRunningScriptName(); got != "my_script_1" {
		t.Fatalf("name = %q", got)
	}
	if c.GetRunningScriptID() != "a" {
		t.Fatal("id not set synchronously")
	}
	c.StopRunningScript()
	eventually(t, "idle", idle(c))
}

func TestStart_SecondQueuedWhileRunning(t *testing.T) {
	c, sim := newCtl(t)
	c.StartScript(blocking("a"))
	c.StartScript(quick("b"))
	if c.GetQueueLength() != 1 || c.GetRunningScriptID() != "a" {
		t.Fatalf("queue=%d running=%q", c.GetQueueLength(), c.GetRunningScriptID())
	}
	if sim.count("run:b") != 0 {
		t.Fatal("b ran concurrently")
	}
	c.StopRunningScript()
	eventually(t, "idle", idle(c))
}

func TestStart_QueueDedupSameID(t *testing.T) {
	c, _ := newCtl(t)
	c.StartScript(blocking("a"))
	c.StartScript(quick("b"))
	c.StartScript(quick("b"))
	c.StartScript(quick("b"))
	if c.GetQueueLength() != 1 {
		t.Fatalf("queue = %d, want 1", c.GetQueueLength())
	}
	c.StopRunningScript()
	eventually(t, "idle", idle(c))
}

func TestStart_QueueFIFOAfterFinish(t *testing.T) {
	c, sim := newCtl(t)
	c.StartScript(blocking("a"))
	c.StartScript(quick("b"))
	c.StartScript(quick("c"))
	c.StartScript(quick("d"))
	if c.GetQueueLength() != 3 {
		t.Fatalf("queue = %d", c.GetQueueLength())
	}
	eventually(t, "a started", func() bool { return sim.count("run:a") == 1 })
	// finish "a" without clearing the queue: cancel its context directly.
	c.stateMutex.Lock()
	cancel := c.cancelRunningScript
	c.stateMutex.Unlock()
	cancel()
	eventually(t, "queue drained", func() bool { return sim.count("run:d") == 1 && c.GetRunningScriptID() == "" })
	got := fmt.Sprint(sim.order())
	want := "[run:a unlockall run:b unlockall run:c unlockall run:d unlockall]"
	if got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
}

func TestStart_IgnoredWhenDisabledInactiveOrEditing(t *testing.T) {
	c, sim := newCtl(t)
	d := quick("d")
	d.Enabled = false
	c.StartScript(d)
	c.ToggleGlobalStatus()
	c.StartScript(quick("g"))
	c.ToggleGlobalStatus()
	c.SetEditDialogOpen(true)
	c.StartScript(quick("e"))
	c.SetEditDialogOpen(false)
	if c.GetRunningScriptID() != "" || c.GetQueueLength() != 0 {
		t.Fatal("ignored start changed state")
	}
	time.Sleep(30 * time.Millisecond)
	if n := len(sim.order()); n != 0 {
		t.Fatalf("something ran: %v", sim.order())
	}
}

func TestStatusChangeCallbackCalledOnStartAndFinish(t *testing.T) {
	c, _ := newCtl(t)
	var mu sync.Mutex
	calls := 0
	c.OnStatusChange = func() { mu.Lock(); calls++; mu.Unlock() }
	c.StartScript(quick("a"))
	eventually(t, "start+finish callbacks", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls >= 2
	})
}

func TestStop_CancelsRunningAndClearsQueue(t *testing.T) {
	c, sim := newCtl(t)
	c.StartScript(infinite("a", false))
	eventually(t, "a iterating", func() bool { return sim.count("run:a") >= 2 })
	c.StartScript(quick("b"))
	c.StartScript(quick("c"))
	c.StopRunningScript()
	if c.GetQueueLength() != 0 {
		t.Fatal("queue not cleared")
	}
	eventually(t, "idle", idle(c))
	if sim.count("run:b") != 0 || sim.count("run:c") != 0 {
		t.Fatalf("queued scripts ran: %v", sim.order())
	}
	if sim.count("unlockall") != 1 {
		t.Fatalf("unlockall = %d", sim.count("unlockall"))
	}
}

func TestStop_EmptyInfiniteScript(t *testing.T) {
	c, sim := newCtl(t)
	c.StartScript(infinite("a", true))
	if c.GetRunningScriptID() != "a" {
		t.Fatal("not running")
	}
	c.StopRunningScript()
	eventually(t, "idle", idle(c))
	if sim.count("unlockall") != 1 {
		t.Fatalf("unlockall = %d", sim.count("unlockall"))
	}
}

func TestStop_NoRunningIsNoop(t *testing.T) {
	c, _ := newCtl(t)
	c.StopRunningScript()
	if c.GetRunningScriptID() != "" || c.GetQueueLength() != 0 {
		t.Fatal("state changed")
	}
}

func TestToggleGlobalOffStops(t *testing.T) {
	c, sim := newCtl(t)
	c.StartScript(blocking("a"))
	c.StartScript(quick("b"))
	c.ToggleGlobalStatus()
	eventually(t, "idle", idle(c))
	if c.GetQueueLength() != 0 || sim.count("run:b") != 0 || sim.count("unlockall") != 1 {
		t.Fatalf("queue=%d events=%v", c.GetQueueLength(), sim.order())
	}
}

func TestEditDialogOpenStops(t *testing.T) {
	c, sim := newCtl(t)
	c.StartScript(blocking("a"))
	c.SetEditDialogOpen(true)
	eventually(t, "idle", idle(c))
	if sim.count("unlockall") != 1 {
		t.Fatalf("unlockall = %d", sim.count("unlockall"))
	}
	if c.CanStartScript(quick("x")) {
		t.Fatal("start allowed while dialog open")
	}
}

func TestStopThenImmediateStart_RunsAfterSlotFrees(t *testing.T) {
	c, sim := newCtl(t)
	c.StartScript(blocking("a"))
	c.StopRunningScript()
	c.StartScript(quick("b"))
	eventually(t, "b ran", func() bool { return sim.count("run:b") == 1 })
	eventually(t, "idle", idle(c))
}

func TestStopOneThenStartAnother(t *testing.T) {
	c, sim := newCtl(t)
	c.StartScript(blocking("a"))
	c.StopRunningScript()
	eventually(t, "idle", idle(c))
	c.StartScript(quick("b"))
	eventually(t, "b ran", func() bool { return sim.count("run:b") == 1 })
}
