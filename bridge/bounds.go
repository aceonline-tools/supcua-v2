package bridge

import "github.com/yanmad27/supcua-v2/model"

// Rect is a screen rectangle in physical pixels (L,T inclusive; R,B exclusive).
type Rect struct{ L, T, R, B int }

func (r Rect) W() int { return r.R - r.L }
func (r Rect) H() int { return r.B - r.T }

const (
	// MinCssW × MinCssH is the compact-density floor (and the minimum window size) in CSS px.
	MinCssW, MinCssH = 720, 480
	// FallbackCssW × FallbackCssH is the failure-only size when measurement does not complete.
	FallbackCssW, FallbackCssH = 960, 720
	maxCss                     = 8192
	minVisibleW, minVisibleH   = 120, 60 // a remembered rect must overlap a work area by at least this
)

// BoundsIn is everything computeEditorBounds needs; the host fills the monitor facts.
type BoundsIn struct {
	CssW, CssH       int  // reported (or fallback) natural size in CSS px
	DPI              int  // real DPI of the target monitor (0 = 96)
	FrameW, FrameH   int  // non-client size at that DPI
	Work             Rect // target monitor work area
	OwnerCX, OwnerCY int  // centre of the owner window (placement when nothing is remembered)
	Remembered       model.EditorWindow
}

// BoundsOut is the editor's outer rectangle and the retry decision.
type BoundsOut struct {
	Outer         Rect
	Clamped       bool // the outer size was reduced to fit the work area
	WidthClamped  bool // the width was reduced: the content reflows, so ask for a remeasure
	RemeasureCssW int  // CSS width to lay out at when WidthClamped
	FromMemory    bool // remembered bounds were used (measured size ignored)
	Maximized     bool
}

func scalePx(css, dpi int) int { return (css*dpi + 95) / 96 } // ceil(css*dpi/96)

func clampInt(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ValidRemembered reports whether remembered bounds may be reused: set, current layout version,
// at least the minimum size and overlapping the work area by minVisibleW × minVisibleH.
func ValidRemembered(m model.EditorWindow, dpi int, work Rect) bool {
	if m.W <= 0 || m.H <= 0 || m.Version != model.EditorLayoutVersion {
		return false
	}
	if dpi <= 0 {
		dpi = 96
	}
	if m.W < scalePx(MinCssW, dpi) || m.H < scalePx(MinCssH, dpi) {
		return false
	}
	ox := min(m.X+m.W, work.R) - max(m.X, work.L)
	oy := min(m.Y+m.H, work.B) - max(m.Y, work.T)
	return ox >= minVisibleW && oy >= minVisibleH
}

// computeEditorBounds converts the page's natural CSS size into the editor's outer rectangle:
// scale by the real DPI, add the frame, raise to the minimum, clamp to the work area and place it.
// Remembered valid bounds take precedence (corrected into the work area).
func computeEditorBounds(in BoundsIn) BoundsOut {
	dpi := in.DPI
	if dpi <= 0 {
		dpi = 96
	}
	workW, workH := in.Work.W(), in.Work.H()
	minW := min(scalePx(MinCssW, dpi)+in.FrameW, workW)
	minH := min(scalePx(MinCssH, dpi)+in.FrameH, workH)

	if ValidRemembered(in.Remembered, dpi, in.Work) {
		w := clampInt(in.Remembered.W, minW, workW)
		h := clampInt(in.Remembered.H, minH, workH)
		x := clampInt(in.Remembered.X, in.Work.L, in.Work.R-w)
		y := clampInt(in.Remembered.Y, in.Work.T, in.Work.B-h)
		return BoundsOut{Outer: Rect{x, y, x + w, y + h}, FromMemory: true, Maximized: in.Remembered.Maximized}
	}

	cssW := clampInt(in.CssW, MinCssW, maxCss)
	cssH := clampInt(in.CssH, MinCssH, maxCss)
	w := scalePx(cssW, dpi) + in.FrameW
	h := scalePx(cssH, dpi) + in.FrameH
	out := BoundsOut{}
	if w > workW {
		w, out.Clamped, out.WidthClamped = workW, true, true
		out.RemeasureCssW = max(MinCssW, (workW-in.FrameW)*96/dpi)
	}
	if h > workH {
		h, out.Clamped = workH, true
	}
	w, h = max(w, minW), max(h, minH)
	x := clampInt(in.OwnerCX-w/2, in.Work.L, in.Work.R-w)
	y := clampInt(in.OwnerCY-h/2, in.Work.T, in.Work.B-h)
	out.Outer = Rect{x, y, x + w, y + h}
	return out
}
