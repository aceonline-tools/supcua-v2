package ui

import (
	"math"
	"strings"

	"github.com/yanmad27/supcua-v2/model"
)

// haloAlpha is the minimum alpha of the text halo (background colour, 1px around glyphs) so text
// stays legible when the background is faded out. With an opaque background it is invisible.
const haloAlpha = 0.65

func parseHex(s string) (r, g, b uint8) {
	var v uint32
	for i := 1; i < 7 && i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			c -= '0'
		case c >= 'a' && c <= 'f':
			c = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			c = c - 'A' + 10
		}
		v = v<<4 | uint32(c)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v)
}

// cornerCoverage is the anti-aliased fraction (0..1) of pixel (x, y) inside a w x h rounded
// rectangle of corner radius r.
func cornerCoverage(x, y, w, h, r int) float64 {
	if r <= 0 {
		return 1
	}
	cx, cy := float64(x)+0.5, float64(y)+0.5
	rf := float64(r)
	var ox, oy float64
	switch {
	case cx < rf:
		ox = rf
	case cx > float64(w)-rf:
		ox = float64(w) - rf
	default:
		return 1
	}
	switch {
	case cy < rf:
		oy = rf
	case cy > float64(h)-rf:
		oy = float64(h) - rf
	default:
		return 1
	}
	d := math.Hypot(cx-ox, cy-oy)
	return math.Min(math.Max(rf-d+0.5, 0), 1)
}

// composeOverlay renders the overlay into pix (w*h BGRA, premultiplied alpha, top-down).
// The background (rounded, anti-aliased) carries BackgroundOpacity as its alpha; glyphs (mask is
// per-pixel coverage 0..255, same size) are painted at full alpha in text colour (tr, tg, tb);
// a 1px halo in the background colour surrounds them. The overall opacity is NOT applied here: it
// is the BLENDFUNCTION.SourceConstantAlpha of UpdateLayeredWindow.
func composeOverlay(pix []byte, w, h int, st model.ResolvedOverlayStyle, tr, tg, tb uint8, mask []uint8) {
	br, bg, bb := parseHex(st.Background)
	bgA := st.BackgroundOpacity
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cov := cornerCoverage(x, y, w, h, st.Radius)
			a := bgA * cov
			if bgA < 1 {
				if hc := haloCoverage(mask, w, h, x, y); hc > 0 {
					a = math.Max(a, math.Min(haloAlpha*hc*cov, 1))
				}
			}
			pr, pg, pb := float64(br)*a, float64(bg)*a, float64(bb)*a
			if m := float64(mask[y*w+x]) / 255; m > 0 {
				pr = float64(tr)*m + pr*(1-m)
				pg = float64(tg)*m + pg*(1-m)
				pb = float64(tb)*m + pb*(1-m)
				a = m + a*(1-m)
			}
			i := (y*w + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = round8(pb), round8(pg), round8(pr), round8(a*255)
		}
	}
}

func round8(v float64) byte {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return byte(v + 0.5)
}

// haloCoverage is the strongest glyph coverage among the 8 neighbours of (x, y), 0..1.
func haloCoverage(mask []uint8, w, h, x, y int) float64 {
	var best uint8
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			nx, ny := x+dx, y+dy
			if (dx == 0 && dy == 0) || nx < 0 || ny < 0 || nx >= w || ny >= h {
				continue
			}
			if m := mask[ny*w+nx]; m > best {
				best = m
			}
		}
	}
	return float64(best) / 255
}

// sanitizeStatusText cuts at the first NUL (a Windows string cannot carry one) so the text always
// converts to UTF-16.
func sanitizeStatusText(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i]
	}
	return s
}
