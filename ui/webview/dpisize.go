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

// fitSize returns the DPI-scaled default window size, shrunk to the work area (aw x ah) when it
// does not fit, never below the scaled minimum.
func fitSize(dpi, aw, ah, minW, minH int) (int, int) {
	w, h := scaleDPI(defaultW, dpi), scaleDPI(defaultH, dpi)
	if aw > 0 && w > aw {
		w = aw
	}
	if ah > 0 && h > ah {
		h = ah
	}
	if mw := scaleDPI(minW, dpi); w < mw {
		w = mw
	}
	if mh := scaleDPI(minH, dpi); h < mh {
		h = mh
	}
	return w, h
}
