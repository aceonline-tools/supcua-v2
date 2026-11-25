package main

import (
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
)

type fakeRegistry struct {
	mu   sync.Mutex
	next int
	live map[int]string
}

func newFakeRegistry() *fakeRegistry { return &fakeRegistry{live: map[int]string{}} }

func (r *fakeRegistry) Register(hk model.Hotkey, _ hotkey.HotkeyCallback) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	r.live[r.next] = hk.Key
	return r.next, nil
}

func (r *fakeRegistry) Unregister(id int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live, id)
	return nil
}

func (r *fakeRegistry) keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, k := range r.live {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRebindBeforeStartupWinsOverTheStartupConfig(t *testing.T) {
	reg := newFakeRegistry()
	f := newFixedHotkeys(reg, true)
	f.add(bridge.HotkeyGlobal, model.Hotkey{Key: "f1"}, func(int) {})
	f.add(bridge.HotkeyOverlay, model.Hotkey{Key: "f2"}, func(int) {})
	if err := f.rebind(bridge.HotkeyGlobal, model.Hotkey{Key: "f9"}); err != nil {
		t.Fatal(err)
	}
	if len(reg.keys()) != 0 {
		t.Fatalf("registered before start: %v", reg.keys())
	}
	f.start()
	if got := reg.keys(); !eq(got, []string{"f2", "f9"}) {
		t.Fatalf("registered %v, want the rebound f9 and f2", got)
	}
	if err := f.rebind(bridge.HotkeyGlobal, model.Hotkey{Key: "f8"}); err != nil || !eq(reg.keys(), []string{"f2", "f8"}) {
		t.Fatalf("rebind after start: %v %v", err, reg.keys())
	}
}

func TestFixedHotkeysStopForbidsLateRegistration(t *testing.T) {
	reg := newFakeRegistry()
	f := newFixedHotkeys(reg, true)
	f.add(bridge.HotkeyGlobal, model.Hotkey{Key: "f1"}, func(int) {})
	f.stop()
	f.start()
	if len(reg.keys()) != 0 {
		t.Fatalf("registered after stop: %v", reg.keys())
	}
}

func scriptsNamed(names ...string) []model.Script {
	var out []model.Script
	for _, n := range names {
		out = append(out, model.Script{ID: n, Name: n, Enabled: true})
	}
	return out
}

func deferredSet(f *fakeOS, active bool) *scriptHotkeySet {
	var st atomic.Bool
	st.Store(active)
	s := newScriptHotkeySet(st.Load, f.register, f.unregister)
	s.Defer()
	return s
}

func TestScriptSaveOrDeleteBeforeStartupWinsOverTheSnapshot(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	s := deferredSet(f, true)
	snapshot := scriptsNamed("a", "b", "c")
	s.SetScripts(scriptsNamed("a", "d")) // user deleted b,c and added d before startup finished
	if len(f.live) != 0 {
		t.Fatal("registered before Start")
	}
	s.Start(func() []model.Script { return snapshot })
	if s.Count() != 2 || len(f.live) != 2 {
		t.Fatalf("count=%d live=%d, want the 2 current scripts", s.Count(), len(f.live))
	}
}

func TestStartupUsesLoaderWhenNothingNewerExists(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	s := deferredSet(f, true)
	loaded := 0
	s.Start(func() []model.Script { loaded++; return scriptsNamed("a", "b", "c") }) // seed failed: re-list
	if loaded != 1 || s.Count() != 3 {
		t.Fatalf("loaded=%d count=%d", loaded, s.Count())
	}
}

func TestStartupSkipsLoaderWhenAScriptSetWasAlreadyRecorded(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	s := deferredSet(f, true)
	s.SetScripts(scriptsNamed("a"))
	s.Start(func() []model.Script { t.Error("stale loader used"); return nil })
}

func TestScriptHotkeysStopForbidsRegistrationDuringTeardown(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	s := deferredSet(f, true)
	s.Stop()
	s.Start(func() []model.Script { return scriptsNamed("a") })
	s.SetScripts(scriptsNamed("a"))
	s.Reconcile()
	if len(f.live) != 0 || s.Count() != 0 {
		t.Fatalf("registered after Stop: %v", f.live)
	}
}

func TestDeferredReconcileRegistersNothingUntilStart(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	var st atomic.Bool
	s := newScriptHotkeySet(st.Load, f.register, f.unregister)
	s.Defer()
	s.SetScripts(scriptsNamed("a"))
	st.Store(true)
	s.Reconcile()
	if len(f.live) != 0 {
		t.Fatal("Reconcile registered before Start")
	}
	s.Start(nil)
	if s.Count() != 1 {
		t.Fatalf("count=%d", s.Count())
	}
}
