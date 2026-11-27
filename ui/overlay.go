//go:build windows

package ui

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/ui/webview"
)

var (
	user32DLL            = syscall.NewLazyDLL("user32.dll")
	procUpdateLayeredWin = user32DLL.NewProc("UpdateLayeredWindow")
	colorGreenActive     = [3]uint8{0x00, 0xFF, 0x00}
	colorRedInactive     = [3]uint8{0xFF, 0x00, 0x00}
	colorGrayIdle        = [3]uint8{0x80, 0x80, 0x80}
)

const (
	ulwAlpha       = 0x02
	acSrcOver      = 0x00
	acSrcAlpha     = 0x01
	wmExitSizeMove = 0x0232
)

type blendFunction struct{ BlendOp, BlendFlags, SourceConstantAlpha, AlphaFormat byte }

// OverlayWindow is the always-on-top HUD showing global status. It has no message loop of
// its own: it lives on the UI thread and depends on the pump (walk's MainWindow.Run).
type OverlayWindow struct {
	windowHandle  win.HWND
	appController *app.Controller
	positionX     int
	positionY     int
	isVisible     bool
	isDraggable   bool
	paints        atomic.Int64
	threadID      uint32
	style         model.ResolvedOverlayStyle
	lastPix       []byte

	// OnMoved is called on the UI thread when a drag ends.
	OnMoved func(x, y int)
}

var (
	overlayClassOnce sync.Once
	overlayClassErr  error
	overlayClassName *uint16
	overlayCurrent   atomic.Pointer[OverlayWindow]
)

// overlayWindowProc is the single, process-wide window procedure (NewCallback slots are never
// freed, so it is created once, not per overlay instance).
func overlayWindowProc(hwnd win.HWND, message uint32, wParam, lParam uintptr) uintptr {
	if ow := overlayCurrent.Load(); ow != nil {
		switch message {
		case win.WM_PAINT:
			ow.paint()
			return 0
		case win.WM_RBUTTONUP:
			return 0 // context menu is handled via the tray
		case win.WM_NCHITTEST:
			return win.HTCAPTION // dragging by treating the whole window as title bar
		case win.WM_MOVE:
			ow.positionX = int(int16(lParam & 0xFFFF))
			ow.positionY = int(int16((lParam >> 16) & 0xFFFF))
		case wmExitSizeMove:
			if ow.OnMoved != nil {
				ow.OnMoved(ow.positionX, ow.positionY)
			}
		}
	}
	return win.DefWindowProc(hwnd, message, wParam, lParam)
}

// NewOverlayWindow creates the HUD overlay as a layered, topmost, toolwindow.
func NewOverlayWindow(appController *app.Controller, initialX, initialY int, startVisible bool, style model.ResolvedOverlayStyle) (*OverlayWindow, error) {
	if overlayCurrent.Load() != nil {
		return nil, fmt.Errorf("overlay already exists")
	}
	overlayWindow := &OverlayWindow{
		appController: appController,
		positionX:     initialX,
		positionY:     initialY,
		isVisible:     startVisible,
		style:         style,
		threadID:      win.GetCurrentThreadId(),
	}

	hInstance := win.GetModuleHandle(nil)
	overlayClassOnce.Do(func() {
		overlayClassName, _ = syscall.UTF16PtrFromString("supcua_overlay")
		windowClass := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			LpfnWndProc:   webview.NewCallback(overlayWindowProc),
			HInstance:     hInstance,
			LpszClassName: overlayClassName,
			HbrBackground: win.HBRUSH(win.GetStockObject(win.BLACK_BRUSH)),
		}
		if win.RegisterClassEx(&windowClass) == 0 {
			overlayClassErr = fmt.Errorf("failed to register overlay class")
		}
	})
	if overlayClassErr != nil {
		return nil, overlayClassErr
	}

	windowTitle, _ := syscall.UTF16PtrFromString("supcua-v2 Overlay")
	extendedStyle := uint32(win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW | win.WS_EX_LAYERED | win.WS_EX_TRANSPARENT)

	overlayCurrent.Store(overlayWindow)
	overlayWindow.windowHandle = win.CreateWindowEx(
		extendedStyle,
		overlayClassName,
		windowTitle,
		win.WS_POPUP,
		int32(initialX), int32(initialY),
		int32(style.Width), int32(style.Height),
		0, 0, hInstance, nil,
	)
	if overlayWindow.windowHandle == 0 {
		overlayCurrent.Store(nil)
		return nil, fmt.Errorf("failed to create overlay window")
	}

	overlayWindow.render(true)

	if startVisible {
		win.ShowWindow(overlayWindow.windowHandle, win.SW_SHOWNOACTIVATE)
	}

	return overlayWindow, nil
}

func (overlayWindow *OverlayWindow) paint() {
	var paintStruct win.PAINTSTRUCT
	win.BeginPaint(overlayWindow.windowHandle, &paintStruct)
	win.EndPaint(overlayWindow.windowHandle, &paintStruct)
	overlayWindow.render(false)
}

func (overlayWindow *OverlayWindow) statusText() (text string, rgb [3]uint8) {
	if !overlayWindow.appController.IsGlobalActive() {
		return "DISABLE", colorRedInactive
	}
	if name := overlayWindow.appController.GetRunningScriptName(); name != "" {
		return "RUN: " + name, colorGreenActive
	}
	return "IDLE", colorGrayIdle
}

// render draws the overlay into a 32-bit premultiplied DIB and pushes it with UpdateLayeredWindow.
// GDI draws the glyphs (same system font as before) as a coverage mask; composeOverlay paints the
// anti-aliased rounded background and the solid text. withGeometry also moves/resizes the window.
func (overlayWindow *OverlayWindow) render(withGeometry bool) {
	st := overlayWindow.style
	w, h := st.Width, st.Height
	text, rgb := overlayWindow.statusText()
	if st.TextColor != "" {
		rgb[0], rgb[1], rgb[2] = parseHex(st.TextColor)
	}
	overlayWindow.paints.Add(1)

	screenDC := win.GetDC(0)
	defer win.ReleaseDC(0, screenDC)
	memDC := win.CreateCompatibleDC(screenDC)
	defer win.DeleteDC(memDC)
	var bits unsafe.Pointer
	bmi := win.BITMAPINFO{BmiHeader: win.BITMAPINFOHEADER{
		BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(w), BiHeight: -int32(h), BiPlanes: 1, BiBitCount: 32, BiCompression: win.BI_RGB,
	}}
	bitmap := win.CreateDIBSection(memDC, &bmi.BmiHeader, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bitmap == 0 || bits == nil {
		return
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	previous := win.SelectObject(memDC, win.HGDIOBJ(bitmap))
	defer win.SelectObject(memDC, previous)

	pix := unsafe.Slice((*byte)(bits), w*h*4)
	win.SetBkMode(memDC, win.TRANSPARENT)
	win.SetTextColor(memDC, win.COLORREF(0x00FFFFFF))
	textUTF16, _ := syscall.UTF16FromString(text)
	textRect := win.RECT{Left: 0, Top: 0, Right: int32(w), Bottom: int32(h)}
	win.DrawTextEx(memDC, &textUTF16[0], int32(len(textUTF16)-1), &textRect,
		win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE, nil)
	mask := make([]uint8, w*h)
	for i := range mask {
		mask[i] = max(pix[4*i], pix[4*i+1], pix[4*i+2])
	}
	composeOverlay(pix, w, h, st, rgb[0], rgb[1], rgb[2], mask)
	overlayWindow.lastPix = append(overlayWindow.lastPix[:0], pix...)

	blend := blendFunction{BlendOp: acSrcOver, SourceConstantAlpha: byte(st.Opacity*255 + 0.5), AlphaFormat: acSrcAlpha}
	origin := win.POINT{}
	size := win.SIZE{CX: int32(w), CY: int32(h)}
	dst := win.POINT{X: int32(overlayWindow.positionX), Y: int32(overlayWindow.positionY)}
	var dstPtr, sizePtr uintptr
	if withGeometry {
		dstPtr, sizePtr = uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&size))
	}
	procUpdateLayeredWin.Call(uintptr(overlayWindow.windowHandle), 0, dstPtr, sizePtr,
		uintptr(memDC), uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
}

// ApplyStyle applies a new style and position live. The position is clamped so part of the
// overlay stays on the virtual screen; the effective position is returned.
func (overlayWindow *OverlayWindow) ApplyStyle(style model.ResolvedOverlayStyle, x, y int) (int, int) {
	overlayWindow.style = style
	x, y = ClampToVirtualScreen(x, y, style.Width, style.Height)
	overlayWindow.positionX, overlayWindow.positionY = x, y
	overlayWindow.render(true)
	return x, y
}

// ClampToVirtualScreen clamps a top-left position against the current virtual screen.
func ClampToVirtualScreen(x, y, w, h int) (int, int) {
	return model.ClampOverlayPosition(x, y, w, h,
		int(win.GetSystemMetrics(76)), int(win.GetSystemMetrics(77)), int(win.GetSystemMetrics(78)), int(win.GetSystemMetrics(79)))
}

// Snapshot returns a copy of the last rendered premultiplied BGRA buffer (self-test).
func (overlayWindow *OverlayWindow) Snapshot() (pix []byte, w, h int) {
	return append([]byte(nil), overlayWindow.lastPix...), overlayWindow.style.Width, overlayWindow.style.Height
}

// Style returns the style currently applied.
func (overlayWindow *OverlayWindow) Style() model.ResolvedOverlayStyle { return overlayWindow.style }

// Refresh triggers a repaint of the overlay.
func (overlayWindow *OverlayWindow) Refresh() {
	overlayWindow.render(false)
}

// ToggleVisibility shows or hides the overlay.
func (overlayWindow *OverlayWindow) ToggleVisibility() {
	overlayWindow.SetVisible(!overlayWindow.isVisible)
}

// SetVisible shows or hides the overlay.
func (overlayWindow *OverlayWindow) SetVisible(visible bool) {
	overlayWindow.isVisible = visible
	if visible {
		win.ShowWindow(overlayWindow.windowHandle, win.SW_SHOWNOACTIVATE)
	} else {
		win.ShowWindow(overlayWindow.windowHandle, win.SW_HIDE)
	}
}

// IsVisible returns whether the overlay is currently shown.
func (overlayWindow *OverlayWindow) IsVisible() bool {
	return overlayWindow.isVisible
}

// GetPosition returns the current overlay position.
func (overlayWindow *OverlayWindow) GetPosition() (int, int) {
	return overlayWindow.positionX, overlayWindow.positionY
}

// ToggleDraggable switches the overlay between click-through and draggable mode.
func (overlayWindow *OverlayWindow) ToggleDraggable() {
	overlayWindow.SetDraggable(!overlayWindow.isDraggable)
}

// SetDraggable sets click-through (false) or draggable (true) mode.
func (overlayWindow *OverlayWindow) SetDraggable(draggable bool) {
	overlayWindow.isDraggable = draggable
	exStyle := win.GetWindowLong(overlayWindow.windowHandle, win.GWL_EXSTYLE)
	if draggable {
		exStyle &^= win.WS_EX_TRANSPARENT
	} else {
		exStyle |= win.WS_EX_TRANSPARENT
	}
	win.SetWindowLong(overlayWindow.windowHandle, win.GWL_EXSTYLE, exStyle)
}

// IsDraggable returns whether the overlay is currently draggable.
func (overlayWindow *OverlayWindow) IsDraggable() bool {
	return overlayWindow.isDraggable
}

// Paints counts WM_PAINT handled (self-test).
func (overlayWindow *OverlayWindow) Paints() int64 { return overlayWindow.paints.Load() }

// Handle returns the window handle and its thread (self-test: must be the UI thread).
func (overlayWindow *OverlayWindow) Handle() (hwnd uintptr, threadID uint32) {
	return uintptr(overlayWindow.windowHandle), overlayWindow.threadID
}

// Destroy cleans up the overlay window.
func (overlayWindow *OverlayWindow) Destroy() {
	overlayCurrent.CompareAndSwap(overlayWindow, nil)
	win.DestroyWindow(overlayWindow.windowHandle)
}
