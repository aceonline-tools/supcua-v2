package webview

// scRect is a screen rectangle (L,T inclusive; R,B exclusive) in physical pixels.
type scRect struct{ L, T, R, B int }

func (r scRect) w() int { return r.R - r.L }
func (r scRect) h() int { return r.B - r.T }

// Minimum overlap with a work area for a window to count as reachable (editor-window contract §9).
const minVisW, minVisH = 120, 60

// overlapsEnough reports whether r overlaps work by at least minVisW × minVisH.
func overlapsEnough(r, work scRect) bool {
	ox := min(r.R, work.R) - max(r.L, work.L)
	oy := min(r.B, work.B) - max(r.T, work.T)
	return ox >= minVisW && oy >= minVisH
}

// recoverRect decides where the editor goes after a display change (WM_DISPLAYCHANGE, or a DPI
// change that landed it off-screen). nearest is the work area of the monitor nearest to r; target
// is the main window's monitor work area. A window that is still reachable stays where it is;
// otherwise it is centred in target, its size clamped to it. moved reports whether it changed.
func recoverRect(r, nearest, target scRect) (out scRect, moved bool) {
	if overlapsEnough(r, nearest) {
		return r, false
	}
	w, h := min(r.w(), target.w()), min(r.h(), target.h())
	x := target.L + (target.w()-w)/2
	y := target.T + (target.h()-h)/2
	return scRect{x, y, x + w, y + h}, true
}
