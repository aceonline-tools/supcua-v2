package webview

const (
	baseDPI  = 96
	defaultW = 1000
	// defaultH is tall enough for the Scripts screen with all groups expanded (about 880 CSS px of
	// content) at 100% on a 1080p work area; fitRect still caps it to the work area.
	defaultH = 960
)

// scaleDPI converts a size in 96-DPI pixels to physical pixels for dpi.
func scaleDPI(v, dpi int) int {
	if dpi <= 0 || dpi == baseDPI {
		return v
	}
	return (v*dpi + baseDPI/2) / baseDPI
}

// useDefaultSize reports whether the window starts at the DPI-scaled default size instead of the
// saved one: when no size was saved, or when reset is set because the persisted window layout is
// older than the current one (that covers the legacy unscaled 1000x700 earlier builds saved back on
// close). Once the layout is current a saved size is always respected, 1000x700 included.
func useDefaultSize(w, h int, reset bool) bool {
	return reset || w <= 0 || h <= 0
}

// rect is a window or work-area rectangle in screen pixels.
type rect struct{ L, T, R, B int }

func clampInt(v, lo, hi int) int {
	if v > hi {
		v = hi
	}
	if v < lo {
		v = lo
	}
	return v
}

// scaledMin is the DPI-scaled minimum size, never larger than the work area (0 = unknown).
func scaledMin(dpi, minW, minH, aw, ah int) (int, int) {
	mw, mh := scaleDPI(minW, dpi), scaleDPI(minH, dpi)
	if aw > 0 && mw > aw {
		mw = aw
	}
	if ah > 0 && mh > ah {
		mh = ah
	}
	return mw, mh
}

// fitRect returns the DPI-scaled default window rectangle for a window currently at (x, y): the
// size is capped to the work area (never below the capped minimum) and the whole rectangle is
// moved inside the work area.
func fitRect(dpi int, work rect, x, y, minW, minH int) (int, int, int, int) {
	aw, ah := work.R-work.L, work.B-work.T
	mw, mh := scaledMin(dpi, minW, minH, aw, ah)
	w, h := scaleDPI(defaultW, dpi), scaleDPI(defaultH, dpi)
	if aw > 0 {
		w = clampInt(w, mw, aw)
	} else if w < mw {
		w = mw
	}
	if ah > 0 {
		h = clampInt(h, mh, ah)
	} else if h < mh {
		h = mh
	}
	if aw > 0 {
		x = clampInt(x, work.L, work.R-w)
	}
	if ah > 0 {
		y = clampInt(y, work.T, work.B-h)
	}
	return x, y, w, h
}
