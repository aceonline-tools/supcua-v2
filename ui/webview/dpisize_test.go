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
		{"100% roomy", 96, rect{0, 0, 1920, 1040}, 100, 100, 100, 100, 1000, 700},
		{"150% roomy", 144, rect{0, 0, 2560, 1400}, 50, 50, 50, 50, 1500, 1050},
		{"150% 1080p cascade origin", 144, rect{0, 0, 1920, 1040}, 60, 60, 60, 0, 1500, 1040},
		{"200% on 1200x800", 192, rect{0, 0, 1200, 800}, 100, 100, 0, 0, 1200, 800},
		{"positive origin near bottom/right", 144, rect{0, 0, 1920, 1040}, 1800, 900, 420, 0, 1500, 1040},
		{"secondary monitor offset", 96, rect{1920, 0, 3840, 1040}, 3700, 600, 2840, 340, 1000, 700},
		{"unknown work area", 144, rect{}, 10, 10, 10, 10, 1500, 1050},
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

func TestIsDefaultSize(t *testing.T) {
	if !isDefaultSize(0, 0) || !isDefaultSize(1000, 700) || isDefaultSize(1000, 701) || isDefaultSize(1500, 1050) {
		t.Error("isDefaultSize")
	}
}
