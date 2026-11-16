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
	"github.com/yanmad27/supcua-v2/ui/webview"
)

var (
	user32DLL                      = syscall.NewLazyDLL("user32.dll")
	gdi32DLL                       = syscall.NewLazyDLL("gdi32.dll")
	procCreateSolidBrush           = gdi32DLL.NewProc("CreateSolidBrush")
	procFillRect                   = user32DLL.NewProc("FillRect")
	procSetLayeredWindowAttributes = user32DLL.NewProc("SetLayeredWindowAttributes")
)

const (
	overlayWidth  = 136
	overlayHeight = 32

	colorBackground  = 0x00404040 // dark gray (BGR)
	colorGreenActive = 0x0000FF00 // green text (BGR)
	colorRedInactive = 0x000000FF // red text (BGR)
	colorGrayIdle    = 0x00808080 // gray text (BGR)

	layeredWindowAttributeAlpha = 0x02
	wmExitSizeMove              = 0x0232
)

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
func NewOverlayWindow(appController *app.Controller, initialX, initialY int, startVisible bool) (*OverlayWindow, error) {
	if overlayCurrent.Load() != nil {
		return nil, fmt.Errorf("overlay already exists")
	}
	overlayWindow := &OverlayWindow{
		appController: appController,
		positionX:     initialX,
		positionY:     initialY,
		isVisible:     startVisible,
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
		overlayWidth, overlayHeight,
		0, 0, hInstance, nil,
	)
	if overlayWindow.windowHandle == 0 {
		overlayCurrent.Store(nil)
		return nil, fmt.Errorf("failed to create overlay window")
	}

	// Set 80% opacity
	procSetLayeredWindowAttributes.Call(
		uintptr(overlayWindow.windowHandle), 0, 200, layeredWindowAttributeAlpha,
	)

	if startVisible {
		win.ShowWindow(overlayWindow.windowHandle, win.SW_SHOWNOACTIVATE)
	}

	return overlayWindow, nil
}

func (overlayWindow *OverlayWindow) paint() {
	var paintStruct win.PAINTSTRUCT
	deviceContext := win.BeginPaint(overlayWindow.windowHandle, &paintStruct)
	defer win.EndPaint(overlayWindow.windowHandle, &paintStruct)
	overlayWindow.paints.Add(1)

	var textColor uint32
	var statusText string
	if overlayWindow.appController.IsGlobalActive() {
		textColor = colorGrayIdle
		statusText = "IDLE"
		runningScriptName := overlayWindow.appController.GetRunningScriptName()
		if runningScriptName != "" {
			textColor = colorGreenActive
			statusText = "RUN: " + runningScriptName
		}
	} else {
		textColor = colorRedInactive
		statusText = "DISABLE"
	}

	brushHandle, _, _ := procCreateSolidBrush.Call(uintptr(colorBackground))
	clientRect := win.RECT{Left: 0, Top: 0, Right: overlayWidth, Bottom: overlayHeight}
	procFillRect.Call(uintptr(deviceContext), uintptr(unsafe.Pointer(&clientRect)), brushHandle)
	win.DeleteObject(win.HGDIOBJ(brushHandle))

	win.SetBkMode(deviceContext, win.TRANSPARENT)
	win.SetTextColor(deviceContext, win.COLORREF(textColor))

	textUTF16, _ := syscall.UTF16FromString(statusText)
	textRect := clientRect
	win.DrawTextEx(deviceContext, &textUTF16[0], int32(len(textUTF16)-1), &textRect,
		win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE, nil)
}

// Refresh triggers a repaint of the overlay.
func (overlayWindow *OverlayWindow) Refresh() {
	win.InvalidateRect(overlayWindow.windowHandle, nil, true)
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
