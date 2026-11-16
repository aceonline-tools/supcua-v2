package main

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

type fakeOS struct {
	mu     sync.Mutex
	next   int
	live   map[int]bool
	failed bool
}

func (f *fakeOS) register(scripts []model.Script) []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []int
	for range scripts {
		f.next++
		f.live[f.next] = true
		ids = append(ids, f.next)
	}
	return ids
}

func (f *fakeOS) unregister(id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.live[id] {
		return errors.New("not registered")
	}
	delete(f.live, id)
	return nil
}

func newSet(f *fakeOS, state *atomic.Bool) *scriptHotkeySet {
	return newScriptHotkeySet(state.Load, f.register, f.unregister)
}

func TestScriptHotkeySetConcurrentNoOrphans(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	var state atomic.Bool
	state.Store(true)
	scripts := []model.Script{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	s := newSet(f, &state)
	s.SetScripts(scripts)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				state.Store((i+j)%2 == 0)
				s.Reconcile()
			}
		}(i)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.SetScripts(scripts[:1+j%3])
			}
		}()
	}
	wg.Wait()
	state.Store(true)
	s.Reconcile()

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(f.live) != len(s.ids) || len(f.live) == 0 {
		t.Fatalf("live OS registrations %d != tracked %d", len(f.live), len(s.ids))
	}
	for _, id := range s.ids {
		if !f.live[id] {
			t.Fatalf("tracked id %d not registered", id)
		}
	}
}

func TestScriptHotkeySetInactiveStartsEmpty(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	var state atomic.Bool
	s := newSet(f, &state)
	s.SetScripts([]model.Script{{ID: "a"}, {ID: "b"}})
	if len(f.live) != 0 {
		t.Fatal("registered while inactive")
	}
	state.Store(true)
	s.Reconcile()
	if len(f.live) != 2 {
		t.Fatalf("want 2 live, got %d", len(f.live))
	}
	state.Store(false)
	s.Reconcile()
	if len(f.live) != 0 {
		t.Fatal("registrations remain after deactivate")
	}
}

func TestScriptHotkeySetActiveBeforeScriptsRegistersNothing(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	var state atomic.Bool
	state.Store(true)
	s := newSet(f, &state)
	s.Reconcile()
	if len(f.live) != 0 {
		t.Fatal("registered with no scripts")
	}
	s.SetScripts([]model.Script{{ID: "a"}})
	if len(f.live) != 1 {
		t.Fatalf("want 1 live, got %d", len(f.live))
	}
}

// A stale observation (callback A saw "inactive" before a later toggle to
// active) must not leave the app active with no hotkeys registered.
func TestScriptHotkeySetStaleObservationDoesNotWin(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	var state atomic.Bool
	state.Store(true)
	var calls atomic.Int32
	staleRead := make(chan struct{})
	flipped := make(chan struct{})
	isActive := func() bool {
		if calls.Add(1) == 2 { // A's read: force it stale
			v := state.Load()
			close(staleRead)
			<-flipped
			return v
		}
		return state.Load()
	}
	s := newScriptHotkeySet(isActive, f.register, f.unregister)
	s.SetScripts([]model.Script{{ID: "a"}}) // call 1: active, registered

	state.Store(false)
	aDone := make(chan struct{})
	go func() { s.Reconcile(); close(aDone) }() // call 2: observes false, stalls
	<-staleRead
	state.Store(true) // later toggle back to active
	close(flipped)
	<-aDone
	s.Reconcile() // B: runs after A, reads the latest state

	if len(f.live) != 1 {
		t.Fatalf("app active but %d registrations live, want 1", len(f.live))
	}
}
