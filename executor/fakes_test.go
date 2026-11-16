package executor

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

func TestMain(m *testing.M) {
	waitPollInterval = 5 * time.Millisecond
	os.Exit(m.Run())
}

type fakeSim struct {
	mu      sync.Mutex
	events  []string
	cursorX int
	cursorY int
	moveErr error
	keyErr  error
}

func (f *fakeSim) rec(e string) {
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
}

func (f *fakeSim) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func (f *fakeSim) count(e string) int {
	n := 0
	for _, got := range f.log() {
		if got == e {
			n++
		}
	}
	return n
}

func (f *fakeSim) MoveMouse(x, y int) error {
	f.rec(fmt.Sprintf("move:%d,%d", x, y))
	return f.moveErr
}
func (f *fakeSim) GetCursorPos() (int, int) { return f.cursorX, f.cursorY }
func (f *fakeSim) MouseLeftDown()           { f.rec("ldown") }
func (f *fakeSim) MouseLeftUp()             { f.rec("lup") }
func (f *fakeSim) MouseRightDown()          { f.rec("rdown") }
func (f *fakeSim) MouseRightUp()            { f.rec("rup") }
func (f *fakeSim) KeyDown(k string) error {
	f.rec("kd:" + k)
	return f.keyErr
}
func (f *fakeSim) KeyUp(k string) error {
	f.rec("ku:" + k)
	return nil
}
func (f *fakeSim) LockMouse()      { f.rec("lockm") }
func (f *fakeSim) UnlockMouse()    { f.rec("unlockm") }
func (f *fakeSim) LockKeyboard()   { f.rec("lockk") }
func (f *fakeSim) UnlockKeyboard() { f.rec("unlockk") }
func (f *fakeSim) UnlockAll()      { f.rec("unlockall") }

type fakeDetector struct {
	mu         sync.Mutex
	foundAfter int // found once calls > foundAfter; negative = never
	x, y       int
	err        error
	calls      int
	lastRegion *model.Region
	lastImage  string
}

func (d *fakeDetector) FindImage(img string, region *model.Region) (int, int, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.lastRegion = region
	d.lastImage = img
	if d.err != nil {
		return 0, 0, false, d.err
	}
	if d.foundAfter >= 0 && d.calls > d.foundAfter {
		return d.x, d.y, true, nil
	}
	return 0, 0, false, nil
}

func (d *fakeDetector) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
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

func runAsync(ex *Executor, ctx context.Context, s model.Script) <-chan error {
	done := make(chan error, 1)
	go func() { done <- ex.Execute(ctx, s) }()
	return done
}

func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return")
		return nil
	}
}

func assertUnlockAllLast(t *testing.T, sim *fakeSim) {
	t.Helper()
	ev := sim.log()
	if got := sim.count("unlockall"); got != 1 {
		t.Fatalf("UnlockAll count = %d, want 1; events=%v", got, ev)
	}
	if len(ev) == 0 || ev[len(ev)-1] != "unlockall" {
		t.Fatalf("UnlockAll must be the last event; events=%v", ev)
	}
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events\n got: %v\nwant: %v", got, want)
	}
}

func once(actions ...model.Action) model.Script {
	return model.Script{ID: "s", RunMode: model.RunModeOnce, Actions: actions}
}

func act(typ string) model.Action { return model.Action{Type: typ} }

// strip removes the trailing unlockall event so tests can compare action events.
func strip(ev []string) []string {
	if n := len(ev); n > 0 && ev[n-1] == "unlockall" {
		return ev[:n-1]
	}
	return ev
}
