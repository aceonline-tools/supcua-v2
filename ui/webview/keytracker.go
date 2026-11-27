package webview

// Held is a modifier snapshot.
type Held struct{ Ctrl, Shift, Alt, Win bool }

// KeyEvent is one low-level keyboard event seen while a capture is active.
// Ctrl/Shift/Alt are GetAsyncKeyState at hook time (kept for hotkey capture); Held is the
// session-tracked modifier state, which is the reliable one while the hook swallows keys.
type KeyEvent struct {
	VK               uint32
	Down             bool
	Ctrl, Shift, Alt bool
	Held             Held
}

const (
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
)

// modBit maps modifier VKs (generic and left/right variants) to one bit each so that
// releasing one side of a pair keeps the modifier held while the other side is down.
func modBit(vk uint32) (bit uint16, ok bool) {
	switch vk {
	case 0x11:
		return 1 << 0, true
	case 0xA2:
		return 1 << 1, true
	case 0xA3:
		return 1 << 2, true
	case 0x10:
		return 1 << 3, true
	case 0xA0:
		return 1 << 4, true
	case 0xA1:
		return 1 << 5, true
	case 0x12:
		return 1 << 6, true
	case 0xA4:
		return 1 << 7, true
	case 0xA5:
		return 1 << 8, true
	case 0x5B:
		return 1 << 9, true
	case 0x5C:
		return 1 << 10, true
	}
	return 0, false
}

const (
	ctrlBits  = 1<<0 | 1<<1 | 1<<2
	shiftBits = 1<<3 | 1<<4 | 1<<5
	altBits   = 1<<6 | 1<<7 | 1<<8
	winBits   = 1<<9 | 1<<10
)

// seedVKs are the sided VKs the hook reports. Generic VKs are never probed: on Windows
// GetAsyncKeyState(VK_SHIFT) is true for either side, and the hook only reports the sided key-up,
// so a seeded generic bit would never clear.
var seedVKs = []uint32{0xA2, 0xA3, 0xA0, 0xA1, 0xA4, 0xA5, 0x5B, 0x5C}

// genericBits maps a generic modifier VK to every bit of that modifier.
func genericBits(vk uint32) (uint16, bool) {
	switch vk {
	case 0x11:
		return ctrlBits, true
	case 0x10:
		return shiftBits, true
	case 0x12:
		return altBits, true
	}
	return 0, false
}

// ModState tracks held modifiers from the hook's own key events. A swallowed key-down does
// not update Windows' async key state, so GetAsyncKeyState cannot be trusted mid-capture.
type ModState struct{ bits uint16 }

// Update records a modifier key-down or key-up; other keys are ignored.
func (m *ModState) Update(vk uint32, down bool) {
	bit, ok := modBit(vk)
	if !ok {
		return
	}
	if all, generic := genericBits(vk); generic && !down {
		m.bits &^= all // a generic key-up clears both sides and the generic bit
		return
	}
	if down {
		m.bits |= bit
	} else {
		m.bits &^= bit
	}
}

// Seed marks the modifiers already held when the session starts.
func (m *ModState) Seed(held func(vk uint32) bool) {
	m.bits = 0
	for _, vk := range seedVKs {
		if held(vk) {
			m.Update(vk, true)
		}
	}
}

// Reset forgets everything (capture stop, focus loss).
func (m *ModState) Reset() { m.bits = 0 }

// Held returns the current snapshot.
func (m *ModState) Held() Held {
	return Held{Ctrl: m.bits&ctrlBits != 0, Shift: m.bits&shiftBits != 0, Alt: m.bits&altBits != 0, Win: m.bits&winBits != 0}
}

// keyTracker turns raw hook messages into KeyEvents with tracked modifier state.
type keyTracker struct {
	mods     ModState
	needSeed bool
	emitAlt  bool // also deliver Alt key events (still swallowed); off for the editor's captures
}

func newKeyTracker(probe func(vk uint32) bool) *keyTracker {
	t := &keyTracker{}
	t.mods.Seed(probe)
	return t
}

// hookResult says what the hook must do with one message.
type hookResult struct {
	SwallowAlt bool // Alt is always swallowed while active (keeps Windows out of menu mode)
	Emit       bool // deliver Event to the capture callback
	Event      KeyEvent
}

// process handles one WH_KEYBOARD_LL message. While the capture is inactive (focus lost) key
// events are not observed, so the state is dropped and re-seeded when it becomes active again.
func (t *keyTracker) process(vk uint32, wParam uintptr, active bool, probe func(vk uint32) bool, async func(vk uint32) bool) hookResult {
	if !active {
		t.mods.Reset()
		t.needSeed = true
		return hookResult{}
	}
	if t.needSeed {
		t.mods.Seed(probe)
		t.needSeed = false
	}
	down := wParam == wmKeyDown || wParam == wmSysKeyDown
	up := wParam == wmKeyUp || wParam == wmSysKeyUp
	if down || up {
		t.mods.Update(vk, down)
	}
	isAlt := vk == 0x12 || vk == 0xA4 || vk == 0xA5
	if isAlt && !t.emitAlt {
		return hookResult{SwallowAlt: true}
	}
	if !down && !up {
		return hookResult{}
	}
	return hookResult{SwallowAlt: isAlt, Emit: true, Event: KeyEvent{
		VK: vk, Down: down, Ctrl: async(0x11), Shift: async(0x10), Alt: async(0x12), Held: t.mods.Held(),
	}}
}
