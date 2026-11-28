//go:build windows

package winlist

import "testing"

// syscall.NewCallback slots are never freed (about 2000 per process); 5000 enumerations prove the
// single package-level callback does not consume one per call.
func TestRepeatedEnumerationDoesNotExhaustCallbacks(t *testing.T) {
	for i := 0; i < 5000; i++ {
		n := 0
		SystemSource{}.Windows(func(Window) bool { n++; return n < 8 })
	}
	enumMu.Lock()
	left := len(enumReg)
	enumMu.Unlock()
	if left != 0 {
		t.Fatalf("%d registrations leaked", left)
	}
}
