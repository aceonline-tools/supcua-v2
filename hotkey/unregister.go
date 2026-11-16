package hotkey

import (
	"fmt"
	"sync"
)

// removeRegistration drops the callback for id and then asks the OS to release
// the registration. The callback is dropped even when the OS call fails: a
// failed UnregisterHotKey almost always means the ID is no longer registered,
// and keeping the callback would let a hotkey the caller believes removed keep
// triggering actions. An OS registration left behind without a callback is inert.
func removeRegistration(mu *sync.RWMutex, callbacks map[int]HotkeyCallback, id int, osUnregister func(int) error) error {
	mu.Lock()
	delete(callbacks, id)
	mu.Unlock()
	if err := osUnregister(id); err != nil {
		return fmt.Errorf("UnregisterHotKey failed for ID %d: %w", id, err)
	}
	return nil
}
