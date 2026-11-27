package ui

import (
	"github.com/yanmad27/supcua-v2/bridge"
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
