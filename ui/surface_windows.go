//go:build windows

package ui

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/lxn/win"
)

// SurfaceSample is what the screen actually shows inside a window's client area.
type SurfaceSample struct {
	W, H     int
	Distinct int    // distinct colours among sampled pixels
	Dominant uint32 // most frequent colour, 0xRRGGBB
	WhiteRun bool   // true when the area is (nearly) one pure-white colour
}

// SampleClient copies the client area of hwnd from the screen (what DWM composited, so a
// WebView2 surface that never presented shows up as its default white background) and
// summarises it. The window must be visible and not covered.
func SampleClient(hwnd uintptr) (SurfaceSample, error) {
	var s SurfaceSample
	h := win.HWND(hwnd)
	var cr win.RECT
	if !win.GetClientRect(h, &cr) {
		return s, errors.New("GetClientRect failed")
	}
	w, ht := int(cr.Right-cr.Left), int(cr.Bottom-cr.Top)
	if w <= 0 || ht <= 0 {
		return s, fmt.Errorf("client area is %dx%d", w, ht)
	}
	pt := win.POINT{}
	if !win.ClientToScreen(h, &pt) {
		return s, errors.New("ClientToScreen failed")
	}
	screen := win.GetDC(0)
	if screen == 0 {
		return s, errors.New("GetDC(screen) failed")
	}
	defer win.ReleaseDC(0, screen)
	mem := win.CreateCompatibleDC(screen)
	if mem == 0 {
		return s, errors.New("CreateCompatibleDC failed")
	}
	defer win.DeleteDC(mem)
	bmp := win.CreateCompatibleBitmap(screen, int32(w), int32(ht))
	if bmp == 0 {
		return s, errors.New("CreateCompatibleBitmap failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(bmp))
	old := win.SelectObject(mem, win.HGDIOBJ(bmp))
	const captureBlt = 0x40000000
	blt := win.BitBlt(mem, 0, 0, int32(w), int32(ht), screen, pt.X, pt.Y, win.SRCCOPY|captureBlt)
	win.SelectObject(mem, old) // GetDIBits requires the bitmap not to be selected into a DC
	if !blt {
		return s, errors.New("BitBlt failed")
	}
	bi := win.BITMAPINFOHEADER{BiWidth: int32(w), BiHeight: -int32(ht), BiPlanes: 1, BiBitCount: 32, BiCompression: win.BI_RGB}
	bi.BiSize = uint32(unsafe.Sizeof(bi))
	buf := make([]byte, w*ht*4)
	if n := win.GetDIBits(mem, bmp, 0, uint32(ht), &buf[0], (*win.BITMAPINFO)(unsafe.Pointer(&bi)), win.DIB_RGB_COLORS); int(n) != ht {
		return s, fmt.Errorf("GetDIBits copied %d of %d scan lines", n, ht)
	}
	counts := map[uint32]int{}
	for i := 0; i+3 < len(buf); i += 4 * 3 {
		counts[uint32(buf[i+2])<<16|uint32(buf[i+1])<<8|uint32(buf[i])]++
	}
	best := 0
	for c, n := range counts {
		if n > best {
			best, s.Dominant = n, c
		}
	}
	s.W, s.H, s.Distinct = w, ht, len(counts)
	s.WhiteRun = s.Dominant == 0xFFFFFF && len(counts) < 4
	return s, nil
}
