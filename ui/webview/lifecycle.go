package webview

import "sync/atomic"

// lifecycle separates two facts about a window that must not be conflated:
//
//	gone  delivery is invalidated: nothing is posted to the page any more (set first, from any
//	      goroutine, e.g. on a security failure);
//	torn  native teardown (controller, registry entry, HWND) has been claimed by exactly one caller.
//
// A window can be gone without being torn: the teardown that follows must still run.
type lifecycle struct{ gone, torn atomic.Bool }

// invalidate stops delivery; idempotent.
func (l *lifecycle) invalidate() { l.gone.Store(true) }

// beginTeardown reports whether the caller owns the native teardown (true exactly once).
func (l *lifecycle) beginTeardown() bool {
	l.gone.Store(true)
	return l.torn.CompareAndSwap(false, true)
}
