//go:build windows

package hotkey

import (
	"runtime"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// RegisterHotKey must run on the listener's locked OS thread (it owns the registration and
// receives WM_HOTKEY), never on the caller's thread, however Register is invoked.
func TestRegisterRunsOnTheListenerThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	callerTID := currentThreadID()

	got := make(chan uint32, 1)
	registerThreadHook = func(tid uint32) { got <- tid }
	defer func() { registerThreadHook = nil }()

	m := NewManager()
	go m.ListenForHotkeys()
	id, err := m.Register(model.Hotkey{Modifiers: []string{"ctrl", "alt", "shift"}, Key: "f12"}, func(int) {})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer m.Unregister(id)
	select {
	case tid := <-got:
		if tid == callerTID || tid == 0 || tid != m.ListenerThreadID() {
			t.Fatalf("RegisterHotKey ran on thread %d (caller %d, listener %d)", tid, callerTID, m.ListenerThreadID())
		}
	case <-time.After(time.Second):
		t.Fatal("registration hook never ran")
	}
}
