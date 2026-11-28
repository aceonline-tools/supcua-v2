//go:build windows

package ui

import (
	"errors"
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
	procGdiFlush         = syscall.NewLazyDLL("gdi32.dll").NewProc("GdiFlush")
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
	lastText      string
	lastRGB       [3]uint8

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
			ow.clampAfterDrag()
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
			overlayClassErr = fmt.Errorf("failed to register overlay class: %w", syscall.GetLastError())
			overlayLog.Failure("create", "%v", overlayClassErr)
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
		err := fmt.Errorf("failed to create overlay window: %w", syscall.GetLastError())
		overlayLog.Failure("create", "%v", err)
		return nil, err
	}

	if err := overlayWindow.render(true); err != nil {
		overlayLog.Failure("render", "initial render failed: %v", err)
		overlayCurrent.Store(nil)
		win.DestroyWindow(overlayWindow.windowHandle)
		return nil, err
	}

	if startVisible {
		win.ShowWindow(overlayWindow.windowHandle, win.SW_SHOWNOACTIVATE)
	}

	return overlayWindow, nil
}

func (overlayWindow *OverlayWindow) paint() {
	var paintStruct win.PAINTSTRUCT
	win.BeginPaint(overlayWindow.windowHandle, &paintStruct)
	win.EndPaint(overlayWindow.windowHandle, &paintStruct)
	if err := overlayWindow.render(false); err != nil {
		overlayLog.Failure("render", "render failed (paint): %v", err)
	}
}

func (overlayWindow *OverlayWindow) statusText() (text string, rgb [3]uint8) {
	return overlayStatus(overlayWindow.appController.Snapshot())
}

// render draws the overlay into a 32-bit premultiplied DIB and pushes it with UpdateLayeredWindow.
// GDI draws the glyphs (same system font as before) as a coverage mask; composeOverlay paints the
// anti-aliased rounded background and the solid text. withGeometry also moves/resizes the window.
func (overlayWindow *OverlayWindow) render(withGeometry bool) error {
	st := overlayWindow.style
	w, h := st.Width, st.Height
	req := presentFor(overlayWindow.appController.Snapshot(), st, overlayWindow.positionX, overlayWindow.positionY, withGeometry)
	text, rgb := req.Text, req.RGB
	overlayWindow.lastText, overlayWindow.lastRGB = text, rgb
	overlayLog.Shown(text, rgb, overlayWindow.appController.IsGlobalActive())
	overlayWindow.paints.Add(1)

	screenDC := win.GetDC(0)
	if screenDC == 0 {
		return errors.New("overlay: GetDC failed")
	}
	defer win.ReleaseDC(0, screenDC)
	memDC := win.CreateCompatibleDC(screenDC)
	if memDC == 0 {
		return errors.New("overlay: CreateCompatibleDC failed")
	}
	defer win.DeleteDC(memDC)
	var bits unsafe.Pointer
	bmi := win.BITMAPINFO{BmiHeader: win.BITMAPINFOHEADER{
		BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(w), BiHeight: -int32(h), BiPlanes: 1, BiBitCount: 32, BiCompression: win.BI_RGB,
	}}
	bitmap := win.CreateDIBSection(memDC, &bmi.BmiHeader, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bitmap == 0 || bits == nil {
		return errors.New("overlay: CreateDIBSection failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	previous := win.SelectObject(memDC, win.HGDIOBJ(bitmap))
	if previous == 0 {
		return errors.New("overlay: SelectObject failed")
	}
	defer win.SelectObject(memDC, previous)

	pix := unsafe.Slice((*byte)(bits), w*h*4)
	if win.SetBkMode(memDC, win.TRANSPARENT) == 0 {
		overlayLog.Failure("draw", "SetBkMode failed: %v", syscall.GetLastError())
	}
	if win.SetTextColor(memDC, win.COLORREF(0x00FFFFFF)) == win.CLR_INVALID {
		overlayLog.Failure("draw", "SetTextColor failed: %v", syscall.GetLastError())
	}
	textUTF16, err := syscall.UTF16FromString(text)
	if err != nil || len(textUTF16) < 2 {
		overlayLog.Failure("text-fallback", "status text %q is not representable as UTF-16, drawing \"?\" instead", text)
		textUTF16, _ = syscall.UTF16FromString("?")
	}
	textRect := win.RECT{Left: 0, Top: 0, Right: int32(w), Bottom: int32(h)}
	if win.DrawTextEx(memDC, &textUTF16[0], int32(len(textUTF16)-1), &textRect,
		win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE, nil) == 0 {
		overlayLog.Failure("draw", "DrawTextEx failed: %v", syscall.GetLastError())
	}
	// GDI batches drawing; the DIB bits are only valid to read after this
	if ok, _, ferr := procGdiFlush.Call(); ok == 0 {
		overlayLog.Failure("draw", "GdiFlush failed: %v", ferr)
	}
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
	if req.Dst != nil {
		dstPtr, sizePtr = uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&size))
	}
	ok, _, callErr := procUpdateLayeredWin.Call(uintptr(overlayWindow.windowHandle), 0, dstPtr, sizePtr,
		uintptr(memDC), uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
	if ok == 0 {
		return fmt.Errorf("overlay: UpdateLayeredWindow failed: %v", callErr)
	}
	return nil
}

// ApplyStyle applies a new style and position live. The position is moved onto a real monitor
// if needed; the effective position is returned. On a render error the previous style stays.
func (overlayWindow *OverlayWindow) ApplyStyle(style model.ResolvedOverlayStyle, x, y int) (int, int, error) {
	prevStyle, prevX, prevY := overlayWindow.style, overlayWindow.positionX, overlayWindow.positionY
	overlayWindow.style = style
	x, y = ClampToMonitors(x, y, style.Width, style.Height)
	overlayWindow.positionX, overlayWindow.positionY = x, y
	if err := overlayWindow.render(true); err != nil {
		overlayLog.Failure("apply-style", "apply style failed, keeping the previous style: %v", err)
		overlayWindow.style, overlayWindow.positionX, overlayWindow.positionY = prevStyle, prevX, prevY
		if rerr := overlayWindow.render(true); rerr != nil {
			overlayLog.Failure("render", "render failed (style rollback): %v", rerr)
		}
		return prevX, prevY, err
	}
	return x, y, nil
}

type displayDevice struct {
	Size         uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

type devMode struct {
	DeviceName                                                        [32]uint16
	SpecVersion, DriverVersion, Size, DriverExtra                     uint16
	Fields                                                            uint32
	PosX, PosY                                                        int32
	DisplayOrientation, DisplayFixedOutput                            uint32
	Color, Duplex, YResolution, TTOption, Collate                     int16
	FormName                                                          [32]uint16
	LogPixels                                                         uint16
	BitsPerPel, PelsWidth, PelsHeight, DisplayFlags, DisplayFrequency uint32
	ICMMethod, ICMIntent, MediaType, DitherType, Reserved1, Reserved2 uint32
	PanningWidth, PanningHeight                                       uint32
}

// DEVMODEW is 220 bytes; a wrong layout fails the build here.
var _ = [1]byte{}[unsafe.Sizeof(devMode{})-220]

const (
	enumCurrentSettings = ^uintptr(0) // (DWORD)-1
)

var (
	procEnumDisplayDevices  = user32DLL.NewProc("EnumDisplayDevicesW")
	procEnumDisplaySettings = user32DLL.NewProc("EnumDisplaySettingsW")
)

// Monitors lists the attached monitors' rectangles in virtual-screen pixels (the process is
// per-monitor DPI aware, so these match window coordinates). It uses EnumDisplayDevices, which needs
// no callback (the UI layer is limited to one window-procedure callback).
func Monitors() []model.Rect {
	var rects []model.Rect
	for i := uint32(0); i < 64; i++ {
		dd := displayDevice{Size: uint32(unsafe.Sizeof(displayDevice{}))}
		if ok, _, _ := procEnumDisplayDevices.Call(0, uintptr(i), uintptr(unsafe.Pointer(&dd)), 0); ok == 0 {
			break
		}
		if !monitorDeviceActive(dd.StateFlags) {
			continue
		}
		dm := devMode{Size: uint16(unsafe.Sizeof(devMode{}))}
		if ok, _, _ := procEnumDisplaySettings.Call(uintptr(unsafe.Pointer(&dd.DeviceName[0])), enumCurrentSettings, uintptr(unsafe.Pointer(&dm))); ok == 0 {
			continue
		}
		if dm.PelsWidth == 0 || dm.PelsHeight == 0 {
			continue
		}
		rects = append(rects, model.Rect{Left: int(dm.PosX), Top: int(dm.PosY), Right: int(dm.PosX) + int(dm.PelsWidth), Bottom: int(dm.PosY) + int(dm.PelsHeight)})
	}
	return rects
}

// ClampToMonitors keeps a top-left position where the overlay shows on an actual monitor.
func ClampToMonitors(x, y, w, h int) (int, int) {
	return model.ClampOverlayToMonitors(x, y, w, h, Monitors())
}

// clampAfterDrag moves a dragged overlay that ended off every monitor back onto the nearest one.
func (overlayWindow *OverlayWindow) clampAfterDrag() {
	x, y := ClampToMonitors(overlayWindow.positionX, overlayWindow.positionY, overlayWindow.style.Width, overlayWindow.style.Height)
	if x == overlayWindow.positionX && y == overlayWindow.positionY {
		return
	}
	overlayWindow.positionX, overlayWindow.positionY = x, y
	win.SetWindowPos(overlayWindow.windowHandle, 0, int32(x), int32(y), 0, 0, win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
}

// Snapshot returns a copy of the last rendered premultiplied BGRA buffer (self-test).
func (overlayWindow *OverlayWindow) Snapshot() (pix []byte, w, h int) {
	return append([]byte(nil), overlayWindow.lastPix...), overlayWindow.style.Width, overlayWindow.style.Height
}

// Style returns the style currently applied.
func (overlayWindow *OverlayWindow) Style() model.ResolvedOverlayStyle { return overlayWindow.style }

// Refresh triggers a repaint of the overlay.
func (overlayWindow *OverlayWindow) Refresh() {
	if err := overlayWindow.render(false); err != nil {
		overlayLog.Failure("render", "render failed (refresh): %v", err)
	}
}

// LastStatus returns the text and colour of the last render (self-test hook; UI thread).
func (overlayWindow *OverlayWindow) LastStatus() (string, [3]uint8) {
	return overlayWindow.lastText, overlayWindow.lastRGB
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
