package ui

import (
	"sync/atomic"

	"github.com/yanmad27/supcua-v2/executor"
)

// LockTracker wraps the input simulator handed to the executor and mirrors the mouse/keyboard
// lock state (including UnlockAll, which the executor calls when a script ends) so the bridge
// can report it. It changes no executor behaviour: every call is forwarded unchanged first.
type LockTracker struct {
	executor.InputSimulatorInterface
	mouse, keyboard atomic.Bool
	onChange        func()
}

// NewLockTracker wraps inner; onChange (may be nil) runs after a lock flag actually changed.
func NewLockTracker(inner executor.InputSimulatorInterface, onChange func()) *LockTracker {
	return &LockTracker{InputSimulatorInterface: inner, onChange: onChange}
}

func (t *LockTracker) set(flag *atomic.Bool, v bool) {
	if flag.Swap(v) != v && t.onChange != nil {
		t.onChange()
	}
}

func (t *LockTracker) LockMouse()   { t.InputSimulatorInterface.LockMouse(); t.set(&t.mouse, true) }
func (t *LockTracker) UnlockMouse() { t.InputSimulatorInterface.UnlockMouse(); t.set(&t.mouse, false) }
func (t *LockTracker) LockKeyboard() {
	t.InputSimulatorInterface.LockKeyboard()
	t.set(&t.keyboard, true)
}
func (t *LockTracker) UnlockKeyboard() {
	t.InputSimulatorInterface.UnlockKeyboard()
	t.set(&t.keyboard, false)
}
func (t *LockTracker) UnlockAll() {
	t.InputSimulatorInterface.UnlockAll()
	t.set(&t.mouse, false)
	t.set(&t.keyboard, false)
}

// MouseLocked and KeyboardLocked implement bridge.LockState.
func (t *LockTracker) MouseLocked() bool    { return t.mouse.Load() }
func (t *LockTracker) KeyboardLocked() bool { return t.keyboard.Load() }
