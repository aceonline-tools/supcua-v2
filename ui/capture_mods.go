package ui

// keyMods lists the held modifiers in canonical order; the result is never nil so the
// frontend can tell "no modifiers held" from "host reports no modifier state".
func keyMods(ctrl, shift, alt bool) []string {
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
	return mods
}
