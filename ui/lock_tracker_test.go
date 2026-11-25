package ui

import "testing"

type fakeSim struct {
	calls []string
}

func (f *fakeSim) MoveMouse(int, int) error { return nil }
func (f *fakeSim) GetCursorPos() (int, int) { return 0, 0 }
func (f *fakeSim) MouseLeftDown()           {}
func (f *fakeSim) MouseLeftUp()             {}
func (f *fakeSim) MouseRightDown()          {}
func (f *fakeSim) MouseRightUp()            {}
func (f *fakeSim) KeyDown(string) error     { return nil }
func (f *fakeSim) KeyUp(string) error       { return nil }
func (f *fakeSim) LockMouse()               { f.calls = append(f.calls, "lm") }
func (f *fakeSim) UnlockMouse()             { f.calls = append(f.calls, "um") }
func (f *fakeSim) LockKeyboard()            { f.calls = append(f.calls, "lk") }
func (f *fakeSim) UnlockKeyboard()          { f.calls = append(f.calls, "uk") }
func (f *fakeSim) UnlockAll()               { f.calls = append(f.calls, "ua") }

func TestLockTrackerMirrorsStateAndNotifiesOnChangeOnly(t *testing.T) {
	f := &fakeSim{}
	n := 0
	tr := NewLockTracker(f, func() { n++ })
	tr.LockMouse()
	tr.LockMouse() // no change
	tr.LockKeyboard()
	if !tr.MouseLocked() || !tr.KeyboardLocked() || n != 2 {
		t.Fatalf("locked=%v/%v notifications=%d", tr.MouseLocked(), tr.KeyboardLocked(), n)
	}
	tr.UnlockAll()
	if tr.MouseLocked() || tr.KeyboardLocked() || n != 4 {
		t.Fatalf("after UnlockAll locked=%v/%v notifications=%d", tr.MouseLocked(), tr.KeyboardLocked(), n)
	}
	tr.UnlockMouse() // already unlocked: forwarded, no notification
	want := []string{"lm", "lm", "lk", "ua", "um"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls %v want %v", f.calls, want)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Fatalf("calls %v want %v", f.calls, want)
		}
	}
	if n != 4 {
		t.Fatalf("notifications=%d", n)
	}
}
