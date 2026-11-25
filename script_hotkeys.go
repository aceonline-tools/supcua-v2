package main

import (
	"log"
	"sync"

	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
)

type hotkeyRegistrar interface {
	Register(model.Hotkey, hotkey.HotkeyCallback) (int, error)
}

type scriptRunner interface {
	GetRunningScriptID() string
	StartScript(model.Script)
	StopRunningScript()
}

// scriptHotkeySet serializes script-hotkey bookkeeping. Callers may be the UI
// thread, hotkey callbacks, or executor goroutines; every state transition
// happens under mu so no registration ID is ever lost or double-tracked.
// It keeps its own copy of the script list so goroutines never read UI state.
type scriptHotkeySet struct {
	mu         sync.Mutex
	ids        []int
	scripts    []model.Script
	active     bool
	isActive   func() bool
	register   func(scripts []model.Script) []int
	unregister func(id int) error
}

// newScriptHotkeySet starts inactive with no scripts, so SetActive/SetScripts
// are safe to call at any time; nothing is registered until scripts are set.
// isActive is read under the set's mutex, so the final registrations always
// reflect the latest state regardless of caller interleaving.
func newScriptHotkeySet(isActive func() bool, register func([]model.Script) []int, unregister func(int) error) *scriptHotkeySet {
	return &scriptHotkeySet{isActive: isActive, register: register, unregister: unregister}
}

func (s *scriptHotkeySet) clearLocked() {
	for _, id := range s.ids {
		if err := s.unregister(id); err != nil {
			log.Printf("warning: failed to unregister hotkey %d: %v", id, err)
		}
	}
	s.ids = nil
}

// Reconcile aligns registrations with the current global active state; it is
// a no-op when the state did not change.
func (s *scriptHotkeySet) Reconcile() {
	s.mu.Lock()
	defer s.mu.Unlock()
	active := s.isActive()
	if active == s.active {
		return
	}
	s.active = active
	s.clearLocked()
	if active {
		s.ids = s.register(s.scripts)
	}
}

// Count is the number of currently registered script hotkeys.
func (s *scriptHotkeySet) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ids)
}

// SetScripts replaces the script list and re-registers if currently active.
func (s *scriptHotkeySet) SetScripts(scripts []model.Script) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts = append([]model.Script(nil), scripts...)
	s.active = s.isActive()
	s.clearLocked()
	if s.active {
		s.ids = s.register(s.scripts)
	}
}

func registerScriptHotkeys(
	appController scriptRunner,
	hotkeyManager hotkeyRegistrar,
	scripts []model.Script,
) []int {
	var registeredIDs []int

	for _, script := range scripts {
		if !script.Enabled {
			continue
		}
		currentScript := script // capture for closure

		switch script.HotkeyMode {
		case "toggle":
			if script.ToggleHotkey != nil {
				id, err := hotkeyManager.Register(*script.ToggleHotkey, func(hotkeyID int) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScript()
					} else {
						appController.StartScript(currentScript)
					}
				})
				if err != nil {
					log.Printf("warning: failed to register toggle hotkey for script %s: %v", currentScript.Name, err)
				} else {
					registeredIDs = append(registeredIDs, id)
				}
			}
		case "separate":
			if script.StartHotkey != nil {
				id, err := hotkeyManager.Register(*script.StartHotkey, func(hotkeyID int) {
					appController.StartScript(currentScript)
				})
				if err != nil {
					log.Printf("warning: failed to register start hotkey for script %s: %v", currentScript.Name, err)
				} else {
					registeredIDs = append(registeredIDs, id)
				}
			}
			if script.StopHotkey != nil {
				id, err := hotkeyManager.Register(*script.StopHotkey, func(hotkeyID int) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScript()
					}
				})
				if err != nil {
					log.Printf("warning: failed to register stop hotkey for script %s: %v", currentScript.Name, err)
				} else {
					registeredIDs = append(registeredIDs, id)
				}
			}
		}
	}
	return registeredIDs
}
