package webview

import "testing"

func TestScaleDPI(t *testing.T) {
	for _, c := range []struct{ v, dpi, want int }{{1000, 96, 1000}, {1000, 120, 1250}, {1000, 144, 1500}, {700, 192, 1400}, {640, 0, 640}} {
		if got := scaleDPI(c.v, c.dpi); got != c.want {
			t.Errorf("scaleDPI(%d,%d)=%d want %d", c.v, c.dpi, got, c.want)
		}
	}
}

func TestFitSize(t *testing.T) {
	if w, h := fitSize(96, 1920, 1040, 640, 420); w != 1000 || h != 700 {
		t.Errorf("100%%: %dx%d", w, h)
	}
	if w, h := fitSize(144, 2560, 1400, 640, 420); w != 1500 || h != 1050 {
		t.Errorf("150%% roomy: %dx%d", w, h)
	}
	if w, h := fitSize(144, 1920, 1040, 640, 420); w != 1500 || h != 1040 {
		t.Errorf("150%% 1080p: %dx%d", w, h)
	}
	if w, h := fitSize(144, 800, 500, 640, 420); w != 960 || h != 630 {
		t.Errorf("tiny work area keeps scaled min: %dx%d", w, h)
	}
}

func TestIsDefaultSize(t *testing.T) {
	if !isDefaultSize(0, 0) || !isDefaultSize(1000, 700) || isDefaultSize(1000, 701) || isDefaultSize(1500, 1050) {
		t.Error("isDefaultSize")
	}
}
