package webview

import "testing"

func TestTeardownRunsOnceEvenAfterDeliveryWasInvalidated(t *testing.T) {
	var l lifecycle
	l.invalidate() // security failure: delivery cut first
	if !l.gone.Load() || l.torn.Load() {
		t.Fatal("invalidate must not claim teardown")
	}
	if !l.beginTeardown() {
		t.Fatal("teardown after invalidate must still run (HWND, registry and controller would leak)")
	}
	if l.beginTeardown() || l.beginTeardown() {
		t.Fatal("teardown must be claimed exactly once")
	}
}

func TestTeardownWithoutPriorInvalidate(t *testing.T) {
	var l lifecycle
	if !l.beginTeardown() || !l.gone.Load() || l.beginTeardown() {
		t.Fatal("first teardown claims and invalidates; second is a no-op")
	}
}
