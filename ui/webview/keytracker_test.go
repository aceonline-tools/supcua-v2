package webview

import "testing"

const (
	vkCtrl, vkLCtrl, vkRCtrl = 0x11, 0xA2, 0xA3
	vkLShift, vkRShift       = 0xA0, 0xA1
	vkLAlt                   = 0xA4
	vkLWin, vkRWin           = 0x5B, 0x5C
	vkA, vkB                 = 0x41, 0x42
)

func none(uint32) bool { return false }

func feed(t *keyTracker, active bool, seq ...[2]uintptr) []hookResult {
	var out []hookResult
	for _, s := range seq {
		out = append(out, t.process(uint32(s[0]), s[1], active, none, none))
	}
	return out
}

func dn(vk uintptr) [2]uintptr { return [2]uintptr{vk, wmKeyDown} }
func up(vk uintptr) [2]uintptr { return [2]uintptr{vk, wmKeyUp} }

func TestModState_CtrlThenA(t *testing.T) {
	tr := newKeyTracker(none)
	r := feed(tr, true, dn(vkLCtrl), dn(vkA))
	if !r[0].Emit || r[0].Event.VK != vkLCtrl {
		t.Fatalf("modifier-only events are still reported: %+v", r[0])
	}
	if h := r[1].Event.Held; !h.Ctrl || h.Shift || h.Alt || h.Win {
		t.Fatalf("A after Ctrl: %+v", h)
	}
}

func TestModState_HeldAcrossKeysThenReleased(t *testing.T) {
	tr := newKeyTracker(none)
	r := feed(tr, true, dn(vkLCtrl), dn(vkA), up(vkA), dn(vkB), up(vkB), up(vkLCtrl), dn(vkA))
	if !r[1].Event.Held.Ctrl || !r[3].Event.Held.Ctrl {
		t.Fatal("Ctrl must stay held across A and B")
	}
	if r[6].Event.Held.Ctrl {
		t.Fatal("Ctrl released before the last A")
	}
}

func TestModState_LeftRightVariantsAndGeneric(t *testing.T) {
	tr := newKeyTracker(none)
	r := feed(tr, true, dn(vkLCtrl), dn(vkRCtrl), up(vkLCtrl), dn(vkA), up(vkRCtrl), dn(vkB))
	if !r[3].Event.Held.Ctrl {
		t.Fatal("right Ctrl still holds the modifier after left is released")
	}
	if r[5].Event.Held.Ctrl {
		t.Fatal("both released")
	}
	r = feed(tr, true, dn(vkCtrl), dn(vkA))
	if !r[1].Event.Held.Ctrl {
		t.Fatal("generic VK_CONTROL")
	}
	tr = newKeyTracker(none)
	r = feed(tr, true, dn(vkLShift), dn(vkRShift), dn(vkA))
	if h := r[2].Event.Held; !h.Shift || h.Ctrl {
		t.Fatalf("shift: %+v", h)
	}
}

func TestModState_WinKeys(t *testing.T) {
	tr := newKeyTracker(none)
	r := feed(tr, true, dn(vkLWin), dn(vkA), up(vkLWin), dn(vkRWin), dn(vkB), up(vkRWin))
	if !r[1].Event.Held.Win || r[1].Event.Held.Ctrl {
		t.Fatalf("Win+A: %+v", r[1].Event.Held)
	}
	if !r[4].Event.Held.Win {
		t.Fatal("right Win")
	}
}

func TestModState_AltIsSwallowedButTracked(t *testing.T) {
	tr := newKeyTracker(none)
	r := feed(tr, true, dn(vkLAlt), dn(vkA))
	if !r[0].SwallowAlt || r[0].Emit {
		t.Fatalf("Alt is swallowed and not reported: %+v", r[0])
	}
	if !r[1].Event.Held.Alt {
		t.Fatal("Alt+A must carry alt")
	}
}

func TestModState_SeededFromAlreadyHeld(t *testing.T) {
	probe := func(vk uint32) bool { return vk == vkLShift }
	tr := newKeyTracker(probe)
	r := tr.process(vkA, wmKeyDown, true, probe, none)
	if !r.Event.Held.Shift {
		t.Fatal("shift held before capture started")
	}
	tr.process(vkLShift, wmKeyUp, true, probe, none)
	if tr.process(vkA, wmKeyDown, true, probe, none).Event.Held.Shift {
		t.Fatal("seeded modifier released during capture")
	}
}

func TestModState_ResetOnFocusLossAndReseed(t *testing.T) {
	tr := newKeyTracker(none)
	feed(tr, true, dn(vkLCtrl))
	feed(tr, false, up(vkLCtrl), dn(vkA)) // focus lost: key-up unseen, events not reported
	if tr.mods.Held().Ctrl {
		t.Fatal("state must be dropped while inactive")
	}
	probe := func(vk uint32) bool { return vk == vkLCtrl }
	r := tr.process(vkA, wmKeyDown, true, probe, none)
	if !r.Event.Held.Ctrl {
		t.Fatal("re-seed on re-activation picks up a modifier still held")
	}
	var m ModState
	m.Update(vkLCtrl, true)
	m.Reset()
	if m.Held() != (Held{}) {
		t.Fatal("Reset clears")
	}
}

func TestKeyTracker_InactiveEmitsNothing_AsyncFlagsKeptForHotkeyCapture(t *testing.T) {
	tr := newKeyTracker(none)
	if r := feed(tr, false, dn(vkA))[0]; r.Emit || r.SwallowAlt {
		t.Fatalf("inactive: %+v", r)
	}
	async := func(vk uint32) bool { return vk == 0x11 || vk == 0x10 }
	r := tr.process(vkA, wmKeyDown, true, none, async)
	if !r.Event.Ctrl || !r.Event.Shift || r.Event.Alt || r.Event.Held.Ctrl {
		t.Fatalf("legacy async flags unchanged and independent of Held: %+v", r.Event)
	}
}
