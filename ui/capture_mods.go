package ui

import (
	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/ui/webview"
)

// keyMods lists the held modifiers in canonical order; the result is never nil so the
// frontend can tell "no modifiers held" from "host reports no modifier state".
func keyMods(ctrl, shift, alt, win bool) []string {
	mods := []string{}
	if ctrl {
		mods = append(mods, "ctrl")
	}
	if alt {
		mods = append(mods, "alt")
	}
	if shift {
		mods = append(mods, "shift")
	}
	if win {
		mods = append(mods, "win")
	}
	return mods
}

// keyCaptureHandler is the key-mode capture callback: each key-down is forwarded with the
// session-tracked modifier state (ev.Held), never the async flags, which are stale while the
// hook swallows modifier presses. Returning true swallows the key (also keeps Tab from moving focus).
func keyCaptureHandler(sink bridge.CaptureSink, name func(vk uint32) string) func(webview.KeyEvent) bool {
	return func(ev webview.KeyEvent) bool {
		if !ev.Down {
			return false
		}
		sink.KeyWithMods(name(ev.VK), keyMods(ev.Held.Ctrl, ev.Held.Shift, ev.Held.Alt, ev.Held.Win))
		return true
	}
}

// sidedModifierNames maps the side-specific modifier VKs the low-level hook reports to the hotkey
// key names ParseHotkey and DisplayHotkey use ("RCtrl", "LShift", ...). The generic VKs never reach
// the hook, and Win is deliberately absent (a bare Win tap belongs to the Start menu).
var sidedModifierNames = map[uint32]string{
	0xA0: "lshift", 0xA1: "rshift", 0xA2: "lctrl", 0xA3: "rctrl", 0xA4: "lalt", 0xA5: "ralt",
}

// modTap tells a modifier tapped alone from one held as part of a combo, by release: a sided
// modifier is a candidate only if it went down while nothing else was held, and it counts as
// tapped when it comes back up with no other key having gone down in between. Any other key or
// modifier going down while it is held (Ctrl + Alt + F8, Ctrl then Shift, F8 held then Ctrl)
// cancels the candidate, so the combo's own modifiers never also fire as single-key hotkeys.
type modTap struct {
	held    map[uint32]bool
	pending uint32
}

func newModTap() *modTap { return &modTap{held: map[uint32]bool{}} }

func (t *modTap) down(vk uint32) {
	if t.held[vk] { // auto-repeat
		return
	}
	_, isMod := sidedModifierNames[vk]
	if isMod && len(t.held) == 0 {
		t.pending = vk
	} else {
		t.pending = 0
	}
	t.held[vk] = true
}

// up reports the modifier name when vk was tapped alone. noneHeld is the hook-tracked "no
// modifier is down any more" state; it drops keys whose release the hook never saw (focus loss).
func (t *modTap) up(vk uint32, noneHeld bool) (name string, tapped bool) {
	delete(t.held, vk)
	if n, ok := sidedModifierNames[vk]; ok && t.pending == vk {
		name, tapped = n, true
	}
	if t.pending == vk {
		t.pending = 0
	}
	if noneHeld {
		clear(t.held)
	}
	return name, tapped
}

// settingsHotkeyHandler is the Settings recorder's key callback. A non-modifier key down reports
// the combo with the modifiers held at that moment (Esc included: the page treats a bare Escape as
// cancel); a modifier tapped alone reports itself, with its side, on release. Modifier events are
// never swallowed (the hook swallows Alt itself); every other key is, so it never reaches the page.
func settingsHotkeyHandler(sink bridge.CaptureSink, name func(vk uint32) string) func(webview.KeyEvent) bool {
	tap := newModTap()
	return func(ev webview.KeyEvent) bool {
		if _, isMod := sidedModifierNames[ev.VK]; isMod || ev.VK == 0x10 || ev.VK == 0x11 || ev.VK == 0x12 {
			if ev.Down {
				tap.down(ev.VK)
			} else if n, ok := tap.up(ev.VK, !ev.Held.Ctrl && !ev.Held.Shift && !ev.Held.Alt && !ev.Held.Win); ok {
				sink.Hotkey(model.Hotkey{Modifiers: []string{}, Key: n})
			}
			return false
		}
		if !ev.Down {
			tap.up(ev.VK, false)
			return true
		}
		tap.down(ev.VK)
		key := name(ev.VK)
		if key == "" {
			return true
		}
		mods := []string{}
		if ev.Ctrl || ev.Held.Ctrl {
			mods = append(mods, "ctrl")
		}
		if ev.Alt || ev.Held.Alt {
			mods = append(mods, "alt")
		}
		if ev.Shift || ev.Held.Shift {
			mods = append(mods, "shift")
		}
		sink.Hotkey(model.Hotkey{Modifiers: mods, Key: key})
		return true
	}
}
