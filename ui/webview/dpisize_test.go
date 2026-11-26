package webview

import "testing"

func TestScaleDPI(t *testing.T) {
	for _, c := range []struct{ v, dpi, want int }{{1000, 96, 1000}, {1000, 120, 1250}, {1000, 144, 1500}, {700, 192, 1400}, {640, 0, 640}} {
		if got := scaleDPI(c.v, c.dpi); got != c.want {
			t.Errorf("scaleDPI(%d,%d)=%d want %d", c.v, c.dpi, got, c.want)
		}
	}
}

func TestFitRect(t *testing.T) {
	type c struct {
		name           string
		dpi            int
		work           rect
		x, y           int
		wx, wy, ww, wh int
	}
	for _, k := range []c{
		{"100% roomy", 96, rect{0, 0, 1920, 1040}, 100, 100, 100, 80, 1000, 960},
		{"150% roomy", 144, rect{0, 0, 2560, 1500}, 50, 50, 50, 50, 1500, 1440},
		{"150% 1080p cascade origin", 144, rect{0, 0, 1920, 1040}, 60, 60, 60, 0, 1500, 1040},
		{"200% on 1200x800", 192, rect{0, 0, 1200, 800}, 100, 100, 0, 0, 1200, 800},
		{"positive origin near bottom/right", 144, rect{0, 0, 1920, 1040}, 1800, 900, 420, 0, 1500, 1040},
		{"secondary monitor offset", 96, rect{1920, 0, 3840, 1040}, 3700, 600, 2840, 80, 1000, 960},
		{"unknown work area", 144, rect{}, 10, 10, 10, 10, 1500, 1440},
	} {
		x, y, w, h := fitRect(k.dpi, k.work, k.x, k.y, 640, 420)
		if x != k.wx || y != k.wy || w != k.ww || h != k.wh {
			t.Errorf("%s: got %d,%d %dx%d want %d,%d %dx%d", k.name, x, y, w, h, k.wx, k.wy, k.ww, k.wh)
		}
	}
}

func TestScaledMinCappedToWorkArea(t *testing.T) {
	if w, h := scaledMin(192, 640, 420, 1200, 800); w != 1200 || h != 800 {
		t.Errorf("got %dx%d", w, h)
	}
	if w, h := scaledMin(144, 640, 420, 1920, 1040); w != 960 || h != 630 {
		t.Errorf("got %dx%d", w, h)
	}
	if w, h := scaledMin(144, 640, 420, 0, 0); w != 960 || h != 630 {
		t.Errorf("unknown area: %dx%d", w, h)
	}
}

func TestUseDefaultSize(t *testing.T) {
	for _, c := range []struct {
		name  string
		w, h  int
		reset bool
		want  bool
	}{
		{"no saved size", 0, 0, false, true},
		{"no saved height", 1000, 0, false, true},
		{"current layout keeps 1000x700", 1000, 700, false, false},
		{"current layout keeps a scaled size", 1250, 875, false, false},
		{"current layout keeps the new default", 1000, 960, false, false},
		{"stale layout resets 1000x700", 1000, 700, true, true},
		{"stale layout resets any size", 1337, 811, true, true},
		{"stale layout resets the new default size", 1000, 960, true, true},
	} {
		if got := useDefaultSize(c.w, c.h, c.reset); got != c.want {
			t.Errorf("%s: useDefaultSize(%d,%d,%v)=%v want %v", c.name, c.w, c.h, c.reset, got, c.want)
		}
	}
}

// The default window must leave the Scripts screen (about 880 CSS px of content, measured) a margin
// of at least 24 CSS px at 100% on a 1080p work area, and a saved 1000x700 migrates to exactly it.
func TestDefaultHeightFitsScriptsContent(t *testing.T) {
	const contentCSS, margin, caption, border = 879, 24, 31, 8
	work := rect{0, 0, 1920, 1032}
	for _, saved := range [][2]int{{1000, 700}, {1337, 811}} {
		if !useDefaultSize(saved[0], saved[1], true) {
			t.Fatalf("saved %v not reset", saved)
		}
		_, _, w, h := fitRect(96, work, 300, 200, 640, 420)
		if w != 1000 || h != defaultH {
			t.Fatalf("migrated size %dx%d", w, h)
		}
		if client := h - caption - border; client < contentCSS+margin {
			t.Errorf("client height %d < content %d + margin %d", client, contentCSS, margin)
		}
	}
	for _, dpi := range []int{120, 144} {
		aw, ah := 1920, 1032-12*(dpi-96)/24
		_, _, w, h := fitRect(dpi, rect{0, 0, aw, ah}, 0, 0, 640, 420)
		if w > aw || h > ah || h != min(scaleDPI(defaultH, dpi), ah) {
			t.Errorf("dpi %d: %dx%d not the capped default", dpi, w, h)
		}
	}
}
