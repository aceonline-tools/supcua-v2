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
	StartScriptFromHotkey(model.Script, uint64)
	StopRunningScriptFromHotkey(uint64)
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
	pending    bool // startup deferred: record state, register nothing until Start
	stopped    bool // shutdown: never register again
	touched    bool // SetScripts was called (the list is newer than any startup snapshot)
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
	if s.stopped {
		return
	}
	active := s.isActive()
	if active == s.active {
		return
	}
	s.active = active
	if s.pending {
		return
	}
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
	s.touched = true
	if s.stopped || s.pending {
		return
	}
	s.active = s.isActive()
	s.clearLocked()
	if s.active {
		s.ids = s.register(s.scripts)
	}
}

// Defer postpones every registration until Start, so construction on the UI thread never waits
// on the hotkey listener.
func (s *scriptHotkeySet) Defer() {
	s.mu.Lock()
	s.pending = true
	s.mu.Unlock()
}

// Start ends the deferred phase and registers for the latest state. load supplies the startup
// script list and is only used when no SetScripts (save/delete/sync) has already provided a
// newer one; it runs outside the lock and may touch the filesystem.
func (s *scriptHotkeySet) Start(load func() []model.Script) {
	var loaded []model.Script
	s.mu.Lock()
	need := !s.touched
	s.mu.Unlock()
	if need && load != nil {
		loaded = load()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = false
	if s.stopped {
		return
	}
	if !s.touched {
		s.scripts = append([]model.Script(nil), loaded...)
	}
	s.active = s.isActive()
	s.clearLocked()
	if s.active {
		s.ids = s.register(s.scripts)
	}
}

// Stop forbids further registrations (shutdown); it waits for one in flight.
func (s *scriptHotkeySet) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
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
				id, err := hotkeyManager.Register(*script.ToggleHotkey, func(hotkeyID int, epoch uint64) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScriptFromHotkey(epoch)
					} else {
						appController.StartScriptFromHotkey(currentScript, epoch)
					}
				})
				if err != nil {
					log.Printf("warning: failed to register toggle hotkey for script %s: %v", currentScript.Name, err)
				} else {
					registeredIDs = append(registeredIDs, id)
					bindStopPreview(hotkeyManager, appController, id, currentScript.ID)
				}
			}
		case "separate":
			if script.StartHotkey != nil {
				id, err := hotkeyManager.Register(*script.StartHotkey, func(hotkeyID int, epoch uint64) {
					appController.StartScriptFromHotkey(currentScript, epoch)
				})
				if err != nil {
					log.Printf("warning: failed to register start hotkey for script %s: %v", currentScript.Name, err)
				} else {
					registeredIDs = append(registeredIDs, id)
				}
			}
			if script.StopHotkey != nil {
				id, err := hotkeyManager.Register(*script.StopHotkey, func(hotkeyID int, epoch uint64) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScriptFromHotkey(epoch)
					}
				})
				if err != nil {
					log.Printf("warning: failed to register stop hotkey for script %s: %v", currentScript.Name, err)
				} else {
					registeredIDs = append(registeredIDs, id)
					bindStopPreview(hotkeyManager, appController, id, currentScript.ID)
				}
			}
		}
	}
	return registeredIDs
}

type previewBinder interface {
	SetPreview(int, hotkey.Preview) bool
}

type stopPreviewer interface {
	BeginStopPreview(scriptID string, epoch uint64) uint64
	CancelPreview(token uint64)
}

// bindStopPreview lets a polled bare-modifier script hotkey (e.g. RCtrl) show "stopped" on the overlay
// at key-down while its script is running. It is a no-op for other hotkeys and test doubles.
func bindStopPreview(reg hotkeyRegistrar, runner scriptRunner, id int, scriptID string) {
	b, ok := reg.(previewBinder)
	sp, ok2 := runner.(stopPreviewer)
	if !ok || !ok2 {
		return
	}
	b.SetPreview(id, hotkey.Preview{
		Begin:  func(epoch uint64) uint64 { return sp.BeginStopPreview(scriptID, epoch) },
		Cancel: sp.CancelPreview,
	})
}
