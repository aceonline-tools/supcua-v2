package webview

import "testing"

func TestMouseCaptureIsGatedOnTheForegroundWindow(t *testing.T) {
	var picks [][2]int
	pick := func(x, y int) { picks = append(picks, [2]int{x, y}) }
	down := func(uint32) bool { return true }
	up := func(uint32) bool { return false }
	cur := func() (int, int, bool) { return 7, 9, true }

	mouseTick(0x50, down, func() bool { return false }, cur, pick) // another app is foreground
	mouseTick(0x50, up, func() bool { return true }, cur, pick)    // not pressed
	mouseTick(0, down, func() bool { return true }, cur, pick)     // no key configured
	if len(picks) != 0 {
		t.Fatalf("gated or idle ticks must not pick: %v", picks)
	}
	mouseTick(0x50, down, func() bool { return true }, cur, pick) // editor foreground + pressed
	mouseTick(0x50, down, nil, cur, pick)                         // ungated (legacy) still works
	if len(picks) != 2 || picks[0] != [2]int{7, 9} {
		t.Fatalf("%v", picks)
	}
	mouseTick(0x50, down, func() bool { return true }, func() (int, int, bool) { return 0, 0, false }, pick)
	if len(picks) != 2 {
		t.Fatal("no cursor position: no pick")
	}
}

func TestPressMadeWhileInactiveIsConsumedNotReplayed(t *testing.T) {
	pending := true // models GetAsyncKeyState bit 0: reading clears it
	pressed := func(uint32) bool { p := pending; pending = false; return p }
	n := 0
	mouseTick(0x50, pressed, func() bool { return false }, func() (int, int, bool) { return 1, 1, true }, func(int, int) { n++ })
	mouseTick(0x50, pressed, func() bool { return true }, func() (int, int, bool) { return 1, 1, true }, func(int, int) { n++ })
	if n != 0 {
		t.Fatal("a press while another app was foreground must not be delivered after the editor regains focus")
	}
}
