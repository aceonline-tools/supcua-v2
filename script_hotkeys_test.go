package main

import (
	"errors"
	"sync"
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

func TestScriptHotkeySetConcurrentNoOrphans(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	scripts := []model.Script{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	s := newScriptHotkeySet(f.register, f.unregister)
	s.SetActive(true)
	s.SetScripts(scripts)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.SetActive((i+j)%2 == 0)
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

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(f.live) != len(s.ids) {
		t.Fatalf("live OS registrations %d != tracked %d", len(f.live), len(s.ids))
	}
	for _, id := range s.ids {
		if !f.live[id] {
			t.Fatalf("tracked id %d not registered", id)
		}
	}
	if !s.active && len(f.live) != 0 {
		t.Fatalf("inactive but %d registrations remain", len(f.live))
	}
}

func TestScriptHotkeySetInactiveStartsEmpty(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	s := newScriptHotkeySet(f.register, f.unregister)
	s.SetScripts([]model.Script{{ID: "a"}, {ID: "b"}})
	if len(f.live) != 0 {
		t.Fatal("registered while inactive")
	}
	s.SetActive(true)
	if len(f.live) != 2 {
		t.Fatalf("want 2 live, got %d", len(f.live))
	}
	s.SetActive(false)
	if len(f.live) != 0 {
		t.Fatal("registrations remain after deactivate")
	}
}

func TestScriptHotkeySetActiveBeforeScriptsRegistersNothing(t *testing.T) {
	f := &fakeOS{live: map[int]bool{}}
	s := newScriptHotkeySet(f.register, f.unregister)
	s.SetActive(true)
	if len(f.live) != 0 {
		t.Fatal("registered with no scripts")
	}
	s.SetScripts([]model.Script{{ID: "a"}})
	if len(f.live) != 1 {
		t.Fatalf("want 1 live, got %d", len(f.live))
	}
}
