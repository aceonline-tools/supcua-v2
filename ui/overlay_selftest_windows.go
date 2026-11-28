//go:build windows

package ui

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/ui/webview"
)

var (
	procFillRect      = user32DLL.NewProc("FillRect")
	procCreateBrush   = syscall.NewLazyDLL("gdi32.dll").NewProc("CreateSolidBrush")
	procDefWindowProc = user32DLL.NewProc("DefWindowProcW")
)

const (
	overlayShotsEnv   = "SUPCUA_OVERLAY_SHOTS"
	overlayShotPad    = 20
	overlayShotTries  = 6
	overlayAlphaTol   = 0.03
	wsExNoActivateBit = 0x08000000
)

type overlayCase struct {
	name         string
	style        model.OverlayStyle
	x, y         int
	wantBgEff    float64 // background pixel opacity as seen over a white backdrop
	wantTextEff  float64
	textRGB      [3]uint8 // forced text colour (custom case); zero = status colour
	checkLegacy  bool
	wantCornerTr bool
}

func fp64(v float64) *float64 { return &v }

func (s *SelfTest) onUI(f func()) bool {
	done := make(chan struct{})
	s.UI.Post(func() { defer close(done); f() })
	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

// overlayVisual renders the real overlay in the default and a custom style over known backdrops,
// asserts buffer alpha, screen-blended pixels, geometry and (default) equality with the previous
// GDI painter, and saves screenshots when SUPCUA_OVERLAY_SHOTS is set.
func (s *SelfTest) overlayVisual(res *webview.Result) {
	dir := os.Getenv(overlayShotsEnv)
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	cases := []overlayCase{
		{name: "default", style: model.OverlayStyle{}, x: 260, y: 180,
			wantBgEff: model.OverlayDefaultOpacity, wantTextEff: model.OverlayDefaultOpacity, checkLegacy: true},
		{name: "custom", x: 260, y: 180,
			style:     model.OverlayStyle{Radius: 16, Width: 220, Height: 48, Background: "#102030", TextColor: "#FF0000", BackgroundOpacity: fp64(0.5), Opacity: 0.8},
			wantBgEff: 0.5 * 0.8, wantTextEff: 0.8, textRGB: [3]uint8{255, 0, 0}, wantCornerTr: true},
	}
	for _, c := range cases {
		s.overlayCase(res, dir, c)
	}
	mons := Monitors()
	res.Step("monitors enumerated without callbacks", len(mons) > 0, fmt.Sprintf("%v", mons))
	s.dragClampCheck(res)
	s.onUI(func() { s.Overlay.ApplyStyle(model.OverlayStyle{}, 10, 10) })
}

// dragClampCheck simulates a drag that ends far off every monitor and expects the overlay to be
// moved back onto one (the same path as a real HTCAPTION drag end).
func (s *SelfTest) dragClampCheck(res *webview.Result) {
	var rect win.RECT
	var onMonitor bool
	var moved int32
	s.onUI(func() {
		ow := s.Overlay.Window()
		if ow == nil {
			return
		}
		hwndU, _ := ow.Handle()
		hwnd := win.HWND(hwndU)
		win.SetWindowPos(hwnd, 0, -6000, -6000, 0, 0, win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
		win.SendMessage(hwnd, wmExitSizeMove, 0, 0)
		win.GetWindowRect(hwnd, &rect)
		moved = rect.Left
		st := ow.Style()
		x, y := model.ClampOverlayToMonitors(int(rect.Left), int(rect.Top), st.Width, st.Height, Monitors())
		onMonitor = x == int(rect.Left) && y == int(rect.Top)
	})
	res.Step("overlay dragged off every monitor is moved back onto one at drag end", onMonitor && moved > -6000,
		fmt.Sprintf("rect=(%d,%d)", rect.Left, rect.Top))
}

func (s *SelfTest) overlayCase(res *webview.Result, dir string, c overlayCase) {
	st := c.style.Resolve()
	w, h := st.Width, st.Height
	var white, navy win.HWND
	var hwnd win.HWND
	if !s.onUI(func() {
		ow := s.Overlay.Window()
		if ow == nil {
			return
		}
		s.Overlay.ApplyStyle(c.style, c.x, c.y)
		ow.SetVisible(true)
		hwndU, _ := ow.Handle()
		hwnd = win.HWND(hwndU)
		white = newBackdrop("supcua_selftest_white", 0x00FFFFFF, c.x-overlayShotPad, c.y-overlayShotPad, w+2*overlayShotPad, h+2*overlayShotPad)
		navy = newBackdrop("supcua_selftest_navy", 0x00603020, c.x+140, c.y-overlayShotPad, w-140+overlayShotPad, h+2*overlayShotPad)
		win.SetWindowPos(hwnd, win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	}) || hwnd == 0 {
		res.Step("overlay visual "+c.name, false, "overlay window unavailable")
		return
	}
	defer s.onUI(func() { win.DestroyWindow(white); win.DestroyWindow(navy) })

	sx, sy := c.x-overlayShotPad, c.y-overlayShotPad
	sw, sh := w+2*overlayShotPad, h+2*overlayShotPad
	var shot []byte
	var err error
	for try := 0; try < overlayShotTries; try++ {
		time.Sleep(250 * time.Millisecond)
		if shot, err = captureScreen(sx, sy, sw, sh); err == nil && shot[(2*sw+2)*4+1] >= 250 {
			break // white backdrop is on screen behind the overlay
		}
	}
	if err != nil || shot == nil {
		res.Step("overlay visual "+c.name, false, fmt.Sprint("screen capture failed: ", err))
		return
	}
	screen := func(x, y int) (b, g, r byte) {
		i := ((y+overlayShotPad)*sw + x + overlayShotPad) * 4
		return shot[i], shot[i+1], shot[i+2]
	}

	var pix []byte
	var rect win.RECT
	var legacyDiff = -1
	var exStyle int32
	s.onUI(func() {
		ow := s.Overlay.Window()
		ow.Refresh()
		pix, _, _ = ow.Snapshot()
		win.GetWindowRect(hwnd, &rect)
		exStyle = win.GetWindowLong(hwnd, win.GWL_EXSTYLE)
		if c.checkLegacy {
			legacyDiff = diffAgainstLegacy(ow, pix)
		}
	})
	alpha := func(x, y int) byte { return pix[(y*w+x)*4+3] }
	bgR, _, _ := parseHex(st.Background)

	res.Step("overlay "+c.name+": position and size match the style",
		int(rect.Left) == c.x && int(rect.Top) == c.y && int(rect.Right-rect.Left) == w && int(rect.Bottom-rect.Top) == h,
		fmt.Sprintf("rect=(%d,%d %dx%d) want=(%d,%d %dx%d)", rect.Left, rect.Top, rect.Right-rect.Left, rect.Bottom-rect.Top, c.x, c.y, w, h))
	res.Step("overlay "+c.name+": still layered, topmost and click-through",
		exStyle&int32(win.WS_EX_LAYERED|win.WS_EX_TOPMOST|win.WS_EX_TRANSPARENT) == int32(win.WS_EX_LAYERED|win.WS_EX_TOPMOST|win.WS_EX_TRANSPARENT),
		fmt.Sprintf("exstyle=%#x", exStyle))

	if c.wantCornerTr {
		cb, cg, cr := screen(0, 0)
		res.Step("overlay "+c.name+": corner pixel is transparent (buffer alpha 0, backdrop shows through)",
			alpha(0, 0) == 0 && cb >= 253 && cg >= 253 && cr >= 253, fmt.Sprintf("alpha=%d screen=(%d,%d,%d)", alpha(0, 0), cr, cg, cb))
	}

	by := h / 2
	_, _, sr := screen(6, by)
	bgEff := (255 - float64(sr)) / (255 - float64(bgR))
	wantA := c.wantBgEff / st.Opacity * 255
	res.Step(fmt.Sprintf("overlay %s: background alpha = %.2f x %.2f", c.name, st.BackgroundOpacity, st.Opacity),
		math.Abs(float64(alpha(6, by))-st.BackgroundOpacity*255) <= 1 && math.Abs(bgEff-c.wantBgEff) <= overlayAlphaTol,
		fmt.Sprintf("buffer alpha=%d (want %.0f) effective over white=%.3f want %.3f", alpha(6, by), wantA, bgEff, c.wantBgEff))

	tx, ty, found := -1, -1, false
	for y := 0; y < h && !found; y++ {
		for x := 0; x < w && x < 138 && !found; x++ {
			i := (y*w + x) * 4
			if pix[i+3] != 255 {
				continue
			}
			if c.textRGB != [3]uint8{} && !(pix[i+2] == c.textRGB[0] && pix[i+1] == c.textRGB[1] && pix[i] == c.textRGB[2]) {
				continue
			}
			if c.textRGB == [3]uint8{} && pix[i] == byte(0x40) && pix[i+1] == 0x40 && pix[i+2] == 0x40 {
				continue
			}
			tx, ty, found = x, y, true
		}
	}
	if !found {
		res.Step("overlay "+c.name+": text pixel found", false, "no solid glyph pixel left of the navy zone")
	} else {
		sb, sg, sr := screen(tx, ty)
		i := (ty*w + tx) * 4
		glyph, seen := [3]float64{float64(pix[i]), float64(pix[i+1]), float64(pix[i+2])}, [3]float64{float64(sb), float64(sg), float64(sr)}
		ch := 0
		for k := 1; k < 3; k++ {
			if glyph[k] < glyph[ch] {
				ch = k
			}
		}
		textEff := (255 - seen[ch]) / (255 - glyph[ch]) // glyph over white: the darkest channel shows the blend
		res.Step(fmt.Sprintf("overlay %s: text pixel alpha = overall opacity %.2f", c.name, st.Opacity),
			alpha(tx, ty) == 255 && math.Abs(textEff-c.wantTextEff) <= overlayAlphaTol,
			fmt.Sprintf("glyph (%d,%d) buffer alpha=%d effective=%.3f want %.3f", tx, ty, alpha(tx, ty), textEff, c.wantTextEff))
	}
	if c.checkLegacy {
		res.Step("overlay default renders identically to the previous GDI painter (exact, tolerance 0)", legacyDiff == 0,
			fmt.Sprintf("%d of %d pixels differ", legacyDiff, w*h))
	}

	if dir != "" {
		savePNG(filepath.Join(dir, c.name+".png"), shot, sw, sh, 1)
		savePNG(filepath.Join(dir, c.name+"-x4.png"), shot, sw, sh, 4)
		res.Step("overlay "+c.name+": screenshot saved", true, filepath.Join(dir, c.name+".png"))
	}
}

func newBackdrop(class string, rgb uint32, x, y, w, h int) win.HWND {
	name, _ := syscall.UTF16PtrFromString(class)
	hInstance := win.GetModuleHandle(nil)
	brushH, _, _ := procCreateBrush.Call(uintptr(rgb))
	brush := win.HGDIOBJ(brushH)
	wc := win.WNDCLASSEX{
		CbSize: uint32(unsafe.Sizeof(win.WNDCLASSEX{})), LpfnWndProc: procDefWindowProc.Addr(),
		HInstance: hInstance, LpszClassName: name, HbrBackground: win.HBRUSH(brush),
	}
	win.RegisterClassEx(&wc) // already registered on the second use: harmless
	hwnd := win.CreateWindowEx(win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW|wsExNoActivateBit, name, nil, win.WS_POPUP,
		int32(x), int32(y), int32(w), int32(h), 0, 0, hInstance, nil)
	win.ShowWindow(hwnd, win.SW_SHOWNOACTIVATE)
	win.UpdateWindow(hwnd)
	return hwnd
}

func captureScreen(x, y, w, h int) ([]byte, error) {
	screenDC := win.GetDC(0)
	defer win.ReleaseDC(0, screenDC)
	memDC := win.CreateCompatibleDC(screenDC)
	defer win.DeleteDC(memDC)
	var bits unsafe.Pointer
	hdr := win.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(w), BiHeight: -int32(h), BiPlanes: 1, BiBitCount: 32, BiCompression: win.BI_RGB}
	bmp := win.CreateDIBSection(memDC, &hdr, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bmp == 0 || bits == nil {
		return nil, fmt.Errorf("CreateDIBSection failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(bmp))
	old := win.SelectObject(memDC, win.HGDIOBJ(bmp))
	defer win.SelectObject(memDC, old)
	if !win.BitBlt(memDC, 0, 0, int32(w), int32(h), screenDC, int32(x), int32(y), win.SRCCOPY|win.CAPTUREBLT) {
		return nil, fmt.Errorf("BitBlt failed")
	}
	procGdiFlush.Call()
	return append([]byte(nil), unsafe.Slice((*byte)(bits), w*h*4)...), nil
}

// diffAgainstLegacy repaints the overlay the way the previous builds did (GDI FillRect +
// DrawTextEx, no per-pixel alpha) and counts pixels whose colour differs from the new buffer.
func diffAgainstLegacy(ow *OverlayWindow, pix []byte) int {
	w, h := ow.style.Width, ow.style.Height
	text, rgb := ow.statusText()
	screenDC := win.GetDC(0)
	defer win.ReleaseDC(0, screenDC)
	memDC := win.CreateCompatibleDC(screenDC)
	defer win.DeleteDC(memDC)
	var bits unsafe.Pointer
	hdr := win.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(w), BiHeight: -int32(h), BiPlanes: 1, BiBitCount: 32, BiCompression: win.BI_RGB}
	bmp := win.CreateDIBSection(memDC, &hdr, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bmp == 0 {
		return -1
	}
	defer win.DeleteObject(win.HGDIOBJ(bmp))
	old := win.SelectObject(memDC, win.HGDIOBJ(bmp))
	defer win.SelectObject(memDC, old)
	brushH, _, _ := procCreateBrush.Call(0x00404040)
	brush := win.HGDIOBJ(brushH)
	rect := win.RECT{Right: int32(w), Bottom: int32(h)}
	procFillRect.Call(uintptr(memDC), uintptr(unsafe.Pointer(&rect)), uintptr(brush))
	win.DeleteObject(brush)
	win.SetBkMode(memDC, win.TRANSPARENT)
	win.SetTextColor(memDC, win.COLORREF(uint32(rgb[0])|uint32(rgb[1])<<8|uint32(rgb[2])<<16))
	t, _ := syscall.UTF16FromString(text)
	win.DrawTextEx(memDC, &t[0], int32(len(t)-1), &rect, win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE, nil)
	procGdiFlush.Call()
	legacy := unsafe.Slice((*byte)(bits), w*h*4)
	diff := 0
	for i := 0; i < w*h; i++ {
		if legacy[4*i] != pix[4*i] || legacy[4*i+1] != pix[4*i+1] || legacy[4*i+2] != pix[4*i+2] || pix[4*i+3] != 255 {
			diff++
		}
	}
	return diff
}

func savePNG(path string, bgra []byte, w, h, scale int) {
	img := image.NewNRGBA(image.Rect(0, 0, w*scale, h*scale))
	for y := 0; y < h*scale; y++ {
		for x := 0; x < w*scale; x++ {
			i := ((y/scale)*w + x/scale) * 4
			o := img.PixOffset(x, y)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = bgra[i+2], bgra[i+1], bgra[i], 255
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	_ = png.Encode(f, img)
}

type overlayObs struct {
	text string
	rgb  [3]uint8
	pix  []byte
	w, h int
	// screen is a BitBlt of the overlay's on-screen rectangle (what a user sees), BGRA top-down.
	screen   []byte
	sw, sh   int
	screenOK bool
}

func (s *SelfTest) observeOverlay() (o overlayObs) {
	s.onUI(func() {
		if ow := s.Overlay.Window(); ow != nil {
			o.text, o.rgb = ow.LastStatus()
			o.pix, o.w, o.h = ow.Snapshot()
			hwnd, _ := ow.Handle()
			var r win.RECT
			if win.GetWindowRect(win.HWND(hwnd), &r) {
				o.sw, o.sh = int(r.Right-r.Left), int(r.Bottom-r.Top)
				if px, err := captureScreen(int(r.Left), int(r.Top), o.sw, o.sh); err == nil {
					o.screen, o.screenOK = px, true
				}
			}
		}
	})
	return o
}

// screenHue counts on-screen pixels whose dominant channel matches rgb (R, G or B) by a margin;
// for a grey target it counts neutral pixels clearly brighter than the dark background.
func screenHue(o overlayObs, rgb [3]uint8) int {
	n := 0
	for i := 0; i+3 < len(o.screen); i += 4 {
		b, g, r := int(o.screen[i]), int(o.screen[i+1]), int(o.screen[i+2])
		mx, mn := max(r, g, b), min(r, g, b)
		switch {
		case rgb[0] == rgb[1] && rgb[1] == rgb[2]:
			if mx-mn < 16 && mx > 0x70 {
				n++
			}
		case mx-mn > 60 && ((rgb[0] > 0x80) == (r == mx || r > mn+60)) && ((rgb[1] > 0x80) == (g > mn+60)) && ((rgb[2] > 0x80) == (b > mn+60)):
			n++
		}
	}
	return n
}

func (s *SelfTest) waitOverlay(cond func(overlayObs) bool) overlayObs {
	var o overlayObs
	waitFor(4*time.Second, func() bool { o = s.observeOverlay(); return cond(o) })
	return o
}

// solidGlyphPixels counts fully covered pixels that equal the text colour exactly.
func solidGlyphPixels(o overlayObs, rgb [3]uint8) int {
	n := 0
	for i := 0; i+3 < len(o.pix); i += 4 {
		if o.pix[i+3] == 255 && o.pix[i] == rgb[2] && o.pix[i+1] == rgb[1] && o.pix[i+2] == rgb[0] {
			n++
		}
	}
	return n
}

// overlayStatusE2E drives the real controller through the production hotkey path (hotkey ->
// StartScript -> status hub -> Post -> Refresh) and asserts the overlay text, colour and pixels for
// running, idle and global-off. The global hotkey is toggled off and left off.
func (s *SelfTest) overlayStatusE2E(res *webview.Result, h ProductionHooks, name, colour string, fire func(model.Hotkey, string, func() bool) bool) {
	s.statusLogLines() // installs the tap and drops earlier lines
	s.onUI(func() { s.Overlay.ApplyStyle(model.OverlayStyle{TextColor: colour}, 260, 180) })
	dir := os.Getenv(overlayShotsEnv)
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	want := func(green [3]uint8) [3]uint8 {
		if colour != "" {
			r, g, b := parseHex(colour)
			return [3]uint8{r, g, b}
		}
		return green
	}
	check := func(label, prefix string, rgb [3]uint8, o overlayObs) {
		n := solidGlyphPixels(o, rgb)
		res.Step(fmt.Sprintf("overlay e2e [%s] %s", name, label),
			len(o.text) >= len(prefix) && o.text[:len(prefix)] == prefix && o.rgb == rgb && n >= 10,
			fmt.Sprintf("text=%q rgb=%v want=%q/%v glyphPixels=%d", o.text, o.rgb, prefix, rgb, n))
		sn := screenHue(o, rgb)
		res.Step(fmt.Sprintf("overlay e2e [%s] %s on screen", name, label), o.screenOK && sn >= 10,
			fmt.Sprintf("screenOK=%v %dx%d screenPixels(%v)=%d", o.screenOK, o.sw, o.sh, rgb, sn))
		if dir != "" && len(o.pix) > 0 {
			savePNG(filepath.Join(dir, fmt.Sprintf("e2e-%s-%s.png", name, label)), o.pix, o.w, o.h, 4)
		}
		if dir != "" && len(o.screen) > 0 {
			savePNG(filepath.Join(dir, fmt.Sprintf("e2e-%s-%s-screen.png", name, label)), o.screen, o.sw, o.sh, 4)
		}
	}
	if !fire(h.ProbeHotkey, "overlay e2e ["+name+"] script started via hotkey", func() bool { return h.RunningScriptID() == h.ProbeID }) {
		return
	}
	run := s.waitOverlay(func(o overlayObs) bool { return len(o.text) >= 5 && o.text[:5] == "RUN: " })
	check("running", "RUN: ", want(colorGreenActive), run)
	fire(h.ProbeHotkey, "overlay e2e ["+name+"] script stopped via hotkey", func() bool { return h.RunningScriptID() == "" })
	idle := s.waitOverlay(func(o overlayObs) bool { return o.text == "IDLE" })
	check("idle", "IDLE", want(colorGrayIdle), idle)
	fire(h.GlobalHotkey, "overlay e2e ["+name+"] global off via hotkey", func() bool { return !h.GlobalActive() })
	off := s.waitOverlay(func(o overlayObs) bool { return o.text == "DISABLE" })
	check("disabled", "DISABLE", want(colorRedInactive), off)
	logs := s.statusLogLines()
	res.Step(fmt.Sprintf("overlay e2e [%s] status log lines (state and shown: RUN, IDLE, DISABLE)", name),
		containsAll(logs, `overlay: state "IDLE" → "RUN: fixture_probe"`, `overlay: shown `, `→ "RUN: fixture_probe" colour`, `→ "IDLE" colour`, `→ "DISABLE" colour`, `overlay: state "RUN: fixture_probe" → "IDLE"`, `→ "DISABLE" (script=`), strings.Join(logs, " | "))
	fire(h.GlobalHotkey, "overlay e2e ["+name+"] global on via hotkey", h.GlobalActive)
	waitFor(4*time.Second, func() bool { return h.ScriptHotkeyCount() > 0 })
}

var (
	tapMu    sync.Mutex
	tapLines []string
	tapOnce  sync.Once
)

// statusLogLines returns the "overlay: state/shown" lines logged since the previous call.
func (s *SelfTest) statusLogLines() []string {
	tapOnce.Do(func() {
		overlayLog.Tap(func(l string) {
			if strings.HasPrefix(l, "overlay: state ") || strings.HasPrefix(l, "overlay: shown ") {
				tapMu.Lock()
				tapLines = append(tapLines, l)
				tapMu.Unlock()
			}
		})
	})
	tapMu.Lock()
	defer tapMu.Unlock()
	out := tapLines
	tapLines = nil
	return out
}

func containsAll(lines []string, subs ...string) bool {
	joined := strings.Join(lines, "\n")
	for _, s := range subs {
		if !strings.Contains(joined, s) {
			return false
		}
	}
	return true
}
