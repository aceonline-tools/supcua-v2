package main

import (
	"log"
	"sync"

	"github.com/yanmad27/supcua-v2/model"
)

// scriptHotkeySet serializes script-hotkey bookkeeping. Callers may be the UI
// thread, hotkey callbacks, or executor goroutines; every state transition
// happens under mu so no registration ID is ever lost or double-tracked.
// It keeps its own copy of the script list so goroutines never read UI state.
type scriptHotkeySet struct {
	mu         sync.Mutex
	ids        []int
	scripts    []model.Script
	active     bool
	register   func(scripts []model.Script) []int
	unregister func(id int) error
}

// newScriptHotkeySet starts inactive with no scripts, so SetActive/SetScripts
// are safe to call at any time; nothing is registered until scripts are set.
func newScriptHotkeySet(register func([]model.Script) []int, unregister func(int) error) *scriptHotkeySet {
	return &scriptHotkeySet{register: register, unregister: unregister}
}

func (s *scriptHotkeySet) clearLocked() {
	for _, id := range s.ids {
		if err := s.unregister(id); err != nil {
			log.Printf("warning: failed to unregister hotkey %d: %v", id, err)
		}
	}
	s.ids = nil
}

// SetActive reconciles registrations with the global active state; it is a
// no-op when the state did not change.
func (s *scriptHotkeySet) SetActive(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if active == s.active {
		return
	}
	s.active = active
	s.clearLocked()
	if active {
		s.ids = s.register(s.scripts)
	}
}

// SetScripts replaces the script list and re-registers if currently active.
func (s *scriptHotkeySet) SetScripts(scripts []model.Script) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts = append([]model.Script(nil), scripts...)
	s.clearLocked()
	if s.active {
		s.ids = s.register(s.scripts)
	}
}
