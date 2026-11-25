package webview

const (
	baseDPI  = 96
	defaultW = 1000
	defaultH = 700
)

// scaleDPI converts a size in 96-DPI pixels to physical pixels for dpi.
func scaleDPI(v, dpi int) int {
	if dpi <= 0 || dpi == baseDPI {
		return v
	}
	return (v*dpi + baseDPI/2) / baseDPI
}

// isDefaultSize reports whether a saved size is the legacy unscaled default (or unset): earlier
// builds created the window 1000x700 physical pixels at every DPI and saved it back on close.
func isDefaultSize(w, h int) bool {
	return w <= 0 || h <= 0 || (w == defaultW && h == defaultH)
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
