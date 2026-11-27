package bridge

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

var work1080 = Rect{0, 0, 1920, 1040}

func TestBoundsScalesByRealDPIAndAddsFrame(t *testing.T) {
	for _, c := range []struct{ dpi, wantW, wantH int }{
		{96, 1000 + 16, 700 + 39}, {144, 1500 + 16, 1040}, {192, 1920, 1040}, // 150%: height clamps; 200%: both clamp
	} {
		o := computeEditorBounds(BoundsIn{CssW: 1000, CssH: 700, DPI: c.dpi, FrameW: 16, FrameH: 39, Work: work1080, OwnerCX: 960, OwnerCY: 520})
		if o.Outer.W() != c.wantW || o.Outer.H() != c.wantH {
			t.Errorf("dpi %d: %dx%d want %dx%d", c.dpi, o.Outer.W(), o.Outer.H(), c.wantW, c.wantH)
		}
	}
}

func TestBoundsWidthClampAsksForRemeasureAtTheClampedCssWidth(t *testing.T) {
	o := computeEditorBounds(BoundsIn{CssW: 1400, CssH: 800, DPI: 192, FrameW: 16, FrameH: 39, Work: work1080, OwnerCX: 960, OwnerCY: 520})
	if !o.WidthClamped || o.Outer.W() != 1920 || o.RemeasureCssW != (1920-16)*96/192 {
		t.Fatalf("%+v", o)
	}
	o = computeEditorBounds(BoundsIn{CssW: 1000, CssH: 3000, DPI: 96, FrameW: 16, FrameH: 39, Work: work1080, OwnerCX: 960, OwnerCY: 520})
	if o.WidthClamped || !o.Clamped || o.Outer.H() != 1040 {
		t.Fatalf("height-only clamp must not remeasure: %+v", o)
	}
}

func TestBoundsFloorAndTinyWorkArea(t *testing.T) {
	o := computeEditorBounds(BoundsIn{CssW: 100, CssH: 100, DPI: 96, FrameW: 16, FrameH: 39, Work: work1080, OwnerCX: 100, OwnerCY: 100})
	if o.Outer.W() != MinCssW+16 || o.Outer.H() != MinCssH+39 {
		t.Fatalf("floor: %+v", o)
	}
	tiny := Rect{0, 0, 1024, 576} // 1024x576 work area at 150%: the minimum no longer fits
	o = computeEditorBounds(BoundsIn{CssW: 900, CssH: 700, DPI: 144, FrameW: 24, FrameH: 58, Work: tiny, OwnerCX: 500, OwnerCY: 300})
	if o.Outer.W() > 1024 || o.Outer.H() > 576 || o.Outer.W() <= 0 || o.Outer.L < 0 || o.Outer.T < 0 || o.Outer.R > 1024 || o.Outer.B > 576 {
		t.Fatalf("tiny: %+v", o)
	}
}

func TestBoundsPlacementCentresOnOwnerInsideWorkArea(t *testing.T) {
	o := computeEditorBounds(BoundsIn{CssW: 800, CssH: 600, DPI: 96, FrameW: 16, FrameH: 39, Work: work1080, OwnerCX: 10, OwnerCY: 5})
	if o.Outer.L != 0 || o.Outer.T != 0 {
		t.Fatalf("must stay inside: %+v", o)
	}
	o = computeEditorBounds(BoundsIn{CssW: 800, CssH: 600, DPI: 96, FrameW: 16, FrameH: 39, Work: work1080, OwnerCX: 960, OwnerCY: 520})
	if o.Outer.L != 960-o.Outer.W()/2 {
		t.Fatalf("centred: %+v", o)
	}
}

func TestRememberedBoundsPrecedenceValidityAndCorrection(t *testing.T) {
	m := model.EditorWindow{X: 100, Y: 100, W: 900, H: 700, Version: model.EditorLayoutVersion}
	o := computeEditorBounds(BoundsIn{CssW: 1200, CssH: 900, DPI: 96, FrameW: 16, FrameH: 39, Work: work1080, Remembered: m})
	if !o.FromMemory || o.Outer != (Rect{100, 100, 1000, 800}) {
		t.Fatalf("%+v", o)
	}
	m.X = 1800 // mostly off the right edge: corrected into the work area
	o = computeEditorBounds(BoundsIn{DPI: 96, FrameW: 16, FrameH: 39, Work: work1080, Remembered: m})
	if !o.FromMemory || o.Outer.R != 1920 {
		t.Fatalf("%+v", o)
	}
	for name, bad := range map[string]model.EditorWindow{
		"old layout": {X: 0, Y: 0, W: 900, H: 700, Version: 0},
		"too small":  {X: 0, Y: 0, W: 300, H: 200, Version: model.EditorLayoutVersion},
		"off screen": {X: 5000, Y: 5000, W: 900, H: 700, Version: model.EditorLayoutVersion},
		"unset":      {},
	} {
		if ValidRemembered(bad, 96, work1080) {
			t.Errorf("%s accepted", name)
		}
	}
	o = computeEditorBounds(BoundsIn{CssW: 1000, CssH: 700, DPI: 96, FrameW: 16, FrameH: 39, Work: work1080, OwnerCX: 960, OwnerCY: 520, Remembered: model.EditorWindow{X: 5000, Y: 5000, W: 900, H: 700, Version: model.EditorLayoutVersion}})
	if o.FromMemory {
		t.Fatal("off-screen memory must fall back to measurement")
	}
}
