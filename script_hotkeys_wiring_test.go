package main

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
)

type fakeRegistrar struct {
	mu      sync.Mutex
	next    int
	cbs     map[int]hotkey.HotkeyCallback
	keys    map[int]string
	failKey string
}

func newRegistrar() *fakeRegistrar {
	return &fakeRegistrar{cbs: map[int]hotkey.HotkeyCallback{}, keys: map[int]string{}}
}

func (r *fakeRegistrar) Register(h model.Hotkey, cb hotkey.HotkeyCallback) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.Key == r.failKey {
		return 0, errors.New("taken")
	}
	r.next++
	r.cbs[r.next] = cb
	r.keys[r.next] = h.Key
	return r.next, nil
}

func (r *fakeRegistrar) fire(t *testing.T, key string) {
	t.Helper()
	r.mu.Lock()
	var cb hotkey.HotkeyCallback
	var id int
	for i, k := range r.keys {
		if k == key {
			cb, id = r.cbs[i], i
		}
	}
	r.mu.Unlock()
	if cb == nil {
		t.Fatalf("no hotkey registered for %q", key)
	}
	cb(id, 0)
}

func (r *fakeRegistrar) has(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range r.keys {
		if k == key {
			return true
		}
	}
	return false
}

type fakeRunner struct {
	mu      sync.Mutex
	running string
	starts  []string
	stops   int
}

func (f *fakeRunner) GetRunningScriptID() string { f.mu.Lock(); defer f.mu.Unlock(); return f.running }
func (f *fakeRunner) StartScriptFromHotkey(s model.Script, _ uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, s.ID)
}
func (f *fakeRunner) StopRunningScriptFromHotkey(uint64) { f.mu.Lock(); defer f.mu.Unlock(); f.stops++ }
func (f *fakeRunner) set(id string)                      { f.mu.Lock(); defer f.mu.Unlock(); f.running = id }

func hk(k string) *model.Hotkey { return &model.Hotkey{Key: k} }

func TestToggleHotkey_StartsThenStops(t *testing.T) {
	reg, run := newRegistrar(), &fakeRunner{}
	ids := registerScriptHotkeys(run, reg, []model.Script{{ID: "a", Enabled: true, HotkeyMode: "toggle", ToggleHotkey: hk("f1")}})
	if len(ids) != 1 {
		t.Fatalf("ids = %v", ids)
	}
	reg.fire(t, "f1")
	if len(run.starts) != 1 || run.starts[0] != "a" || run.stops != 0 {
		t.Fatalf("starts=%v stops=%d", run.starts, run.stops)
	}
	run.set("a")
	reg.fire(t, "f1")
	if run.stops != 1 || len(run.starts) != 1 {
		t.Fatalf("starts=%v stops=%d", run.starts, run.stops)
	}
}

func TestToggleHotkey_OtherScriptRunningQueuesStart(t *testing.T) {
	reg, run := newRegistrar(), &fakeRunner{running: "other"}
	registerScriptHotkeys(run, reg, []model.Script{{ID: "a", Enabled: true, HotkeyMode: "toggle", ToggleHotkey: hk("f1")}})
	reg.fire(t, "f1")
	if len(run.starts) != 1 || run.stops != 0 {
		t.Fatalf("starts=%v stops=%d", run.starts, run.stops)
	}
}

func TestSeparateHotkeys_StartAndStop(t *testing.T) {
	reg, run := newRegistrar(), &fakeRunner{}
	ids := registerScriptHotkeys(run, reg, []model.Script{{ID: "a", Enabled: true, HotkeyMode: "separate", StartHotkey: hk("f1"), StopHotkey: hk("f2")}})
	if len(ids) != 2 {
		t.Fatalf("ids = %v", ids)
	}
	reg.fire(t, "f1")
	if len(run.starts) != 1 {
		t.Fatal("start not forwarded")
	}
	run.set("a")
	reg.fire(t, "f2")
	if run.stops != 1 {
		t.Fatal("stop not forwarded")
	}
}

func TestStopHotkeyIgnoredWhenOtherScriptRunning(t *testing.T) {
	reg, run := newRegistrar(), &fakeRunner{running: "other"}
	registerScriptHotkeys(run, reg, []model.Script{{ID: "a", Enabled: true, HotkeyMode: "separate", StopHotkey: hk("f2")}})
	reg.fire(t, "f2")
	run.set("")
	reg.fire(t, "f2")
	if run.stops != 0 {
		t.Fatalf("stops = %d", run.stops)
	}
}

func TestDisabledAndHotkeylessScriptsNotRegistered(t *testing.T) {
	reg, run := newRegistrar(), &fakeRunner{}
	ids := registerScriptHotkeys(run, reg, []model.Script{
		{ID: "d", Enabled: false, HotkeyMode: "toggle", ToggleHotkey: hk("f1")},
		{ID: "n", Enabled: true, HotkeyMode: "toggle"},
		{ID: "m", Enabled: true, HotkeyMode: "", ToggleHotkey: hk("f3")},
		{ID: "s", Enabled: true, HotkeyMode: "separate"},
	})
	if len(ids) != 0 || len(reg.keys) != 0 {
		t.Fatalf("ids=%v keys=%v", ids, reg.keys)
	}
}

func TestRegisterErrorSkipsID(t *testing.T) {
	reg, run := newRegistrar(), &fakeRunner{}
	reg.failKey = "f1"
	ids := registerScriptHotkeys(run, reg, []model.Script{
		{ID: "a", Enabled: true, HotkeyMode: "separate", StartHotkey: hk("f1"), StopHotkey: hk("f2")},
	})
	if len(ids) != 1 || !reg.has("f2") || reg.has("f1") {
		t.Fatalf("ids=%v keys=%v", ids, reg.keys)
	}
}

func TestHotkeyCapturesEachScript(t *testing.T) {
	reg, run := newRegistrar(), &fakeRunner{}
	registerScriptHotkeys(run, reg, []model.Script{
		{ID: "a", Enabled: true, HotkeyMode: "separate", StartHotkey: hk("f1")},
		{ID: "b", Enabled: true, HotkeyMode: "separate", StartHotkey: hk("f2")},
	})
	reg.fire(t, "f1")
	reg.fire(t, "f2")
	if len(run.starts) != 2 || run.starts[0] != "a" || run.starts[1] != "b" {
		t.Fatalf("starts = %v", run.starts)
	}
}

type wireSim struct {
	mu        sync.Mutex
	unlockAll int
	downs     int
}

func (s *wireSim) KeyDown(string) error     { s.mu.Lock(); s.downs++; s.mu.Unlock(); return nil }
func (s *wireSim) KeyUp(string) error       { return nil }
func (s *wireSim) UnlockAll()               { s.mu.Lock(); s.unlockAll++; s.mu.Unlock() }
func (s *wireSim) MoveMouse(int, int) error { return nil }
func (s *wireSim) GetCursorPos() (int, int) { return 0, 0 }
func (s *wireSim) MouseLeftDown()           {}
func (s *wireSim) MouseLeftUp()             {}
func (s *wireSim) MouseRightDown()          {}
func (s *wireSim) MouseRightUp()            {}
func (s *wireSim) LockMouse()               {}
func (s *wireSim) UnlockMouse()             {}
func (s *wireSim) LockKeyboard()            {}
func (s *wireSim) UnlockKeyboard()          {}
func (s *wireSim) snap() (int, int)         { s.mu.Lock(); defer s.mu.Unlock(); return s.downs, s.unlockAll }

func waitFor(t *testing.T, what string, cond func() bool) {
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

func TestEndToEnd_HotkeyStartsThenStopsRealController(t *testing.T) {
	sim := &wireSim{}
	ctl := app.NewController(executor.NewExecutor(sim, nil, 1), nil, nil, nil)
	reg := newRegistrar()
	script := model.Script{
		ID: "a", Name: "a", Enabled: true, RunMode: model.RunModeInfinite,
		HotkeyMode: "separate", StartHotkey: hk("f1"), StopHotkey: hk("f2"),
		Actions: []model.Action{{Type: model.ActionKeyDown, Key: "x"}},
	}
	registerScriptHotkeys(ctl, reg, []model.Script{script})

	reg.fire(t, "f1")
	waitFor(t, "running", func() bool { return ctl.GetRunningScriptID() == "a" })
	waitFor(t, "iterating", func() bool { d, _ := sim.snap(); return d >= 3 })

	reg.fire(t, "f2")
	waitFor(t, "stopped", func() bool { return ctl.GetRunningScriptID() == "" })
	if _, u := sim.snap(); u != 1 {
		t.Fatalf("UnlockAll = %d, want 1", u)
	}
}

func TestEndToEnd_ToggleHotkeyStartsThenStops(t *testing.T) {
	sim := &wireSim{}
	ctl := app.NewController(executor.NewExecutor(sim, nil, 1), nil, nil, nil)
	reg := newRegistrar()
	script := model.Script{
		ID: "a", Name: "a", Enabled: true, RunMode: model.RunModeInfinite,
		HotkeyMode: "toggle", ToggleHotkey: hk("f1"),
		Actions: []model.Action{{Type: model.ActionKeyDown, Key: "x"}},
	}
	registerScriptHotkeys(ctl, reg, []model.Script{script})

	reg.fire(t, "f1")
	waitFor(t, "running", func() bool { return ctl.GetRunningScriptID() == "a" })
	reg.fire(t, "f1")
	waitFor(t, "stopped", func() bool { return ctl.GetRunningScriptID() == "" })
	if _, u := sim.snap(); u != 1 {
		t.Fatalf("UnlockAll = %d, want 1", u)
	}
}
