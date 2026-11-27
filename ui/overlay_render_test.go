package ui

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func px(pix []byte, w, x, y int) (b, g, r, a byte) {
	i := (y*w + x) * 4
	return pix[i], pix[i+1], pix[i+2], pix[i+3]
}

func glyphMask(w, h int) []uint8 {
	m := make([]uint8, w*h)
	for x := 40; x < 50; x++ {
		m[16*w+x] = 255
	}
	return m
}

func TestComposeDefaultIsOpaqueBackgroundPlusText(t *testing.T) {
	st := model.OverlayStyle{}.Resolve()
	w, h := st.Width, st.Height
	pix := make([]byte, w*h*4)
	composeOverlay(pix, w, h, st, 0, 255, 0, glyphMask(w, h))
	if b, g, r, a := px(pix, w, 0, 0); b != 0x40 || g != 0x40 || r != 0x40 || a != 255 {
		t.Fatalf("default corner must be the opaque #404040: %d %d %d %d", b, g, r, a)
	}
	if b, g, r, a := px(pix, w, 45, 16); b != 0 || g != 255 || r != 0 || a != 255 {
		t.Fatalf("text pixel: %d %d %d %d", b, g, r, a)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if _, _, _, a := px(pix, w, x, y); a != 255 {
				t.Fatalf("default style must be fully opaque at %d,%d (a=%d)", x, y, a)
			}
		}
	}
}

func TestComposeRoundedCornerAndTranslucentBackground(t *testing.T) {
	op := 0.5
	st := model.OverlayStyle{Radius: 16, Width: 200, Height: 48, Background: "#102030", BackgroundOpacity: &op}.Resolve()
	w, h := st.Width, st.Height
	pix := make([]byte, w*h*4)
	composeOverlay(pix, w, h, st, 255, 0, 0, glyphMask(w, h))
	if _, _, _, a := px(pix, w, 0, 0); a != 0 {
		t.Fatalf("corner pixel must be transparent, a=%d", a)
	}
	_, _, _, edge := px(pix, w, 5, 3)
	if edge == 0 || edge >= 128 {
		t.Fatalf("anti-aliased edge expected partial alpha, got %d", edge)
	}
	b, g, r, a := px(pix, w, 100, 4)
	if a < 127 || a > 129 || r < 7 || r > 9 {
		t.Fatalf("background alpha ~0.5 premultiplied: %d %d %d %d", b, g, r, a)
	}
	if _, _, _, a := px(pix, w, 45, 16); a != 255 {
		t.Fatalf("text must stay solid over a translucent background, a=%d", a)
	}
}

func TestComposeHaloOnlyWhenBackgroundFaded(t *testing.T) {
	zero := 0.0
	st := model.OverlayStyle{Width: 200, Height: 48, BackgroundOpacity: &zero}.Resolve()
	w, h := st.Width, st.Height
	pix := make([]byte, w*h*4)
	composeOverlay(pix, w, h, st, 255, 255, 255, glyphMask(w, h))
	if _, _, _, a := px(pix, w, 5, 5); a != 0 {
		t.Fatalf("fully clear background must stay clear away from text, a=%d", a)
	}
	if _, _, _, a := px(pix, w, 44, 15); a < 150 {
		t.Fatalf("halo next to glyph expected alpha >= 0.6, got %d", a)
	}
	if _, _, _, a := px(pix, w, 45, 16); a != 255 {
		t.Fatalf("glyph alpha %d", a)
	}
}

func TestSanitizeStatusTextCutsAtNUL(t *testing.T) {
	if got := sanitizeStatusText("farm\x00evil"); got != "farm" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeStatusText("plain"); got != "plain" {
		t.Fatalf("got %q", got)
	}
}
