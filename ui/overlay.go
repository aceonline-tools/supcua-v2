//go:build windows

package ui

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/app"
)

var (
	user32DLL                      = syscall.NewLazyDLL("user32.dll")
	gdi32DLL                       = syscall.NewLazyDLL("gdi32.dll")
	procCreateSolidBrush           = gdi32DLL.NewProc("CreateSolidBrush")
	procFillRect                   = user32DLL.NewProc("FillRect")
	procSetLayeredWindowAttributes = user32DLL.NewProc("SetLayeredWindowAttributes")
)

const (
	overlayWidth  = 180
	overlayHeight = 32

	colorGreenActive = 0x0000FF00
	colorRedInactive = 0x000000FF
	colorWhiteText   = 0x00FFFFFF

	layeredWindowAttributeAlpha = 0x02
)

// OverlayWindow is the always-on-top HUD showing global status.
type OverlayWindow struct {
	windowHandle  win.HWND
	appController *app.Controller
	positionX     int
	positionY     int
	isVisible     bool
}

// NewOverlayWindow creates the HUD overlay as a layered, topmost, toolwindow.
func NewOverlayWindow(appController *app.Controller, initialX, initialY int, startVisible bool) (*OverlayWindow, error) {
	overlayWindow := &OverlayWindow{
		appController: appController,
		positionX:     initialX,
		positionY:     initialY,
		isVisible:     startVisible,
	}

	className, err := syscall.UTF16PtrFromString("supcua_overlay")
	if err != nil {
		return nil, err
	}

	windowClass := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		LpfnWndProc:   syscall.NewCallback(overlayWindow.windowProcedure),
		HInstance:      win.GetModuleHandle(nil),
		LpszClassName: className,
		HbrBackground: win.HBRUSH(win.GetStockObject(win.BLACK_BRUSH)),
	}
	win.RegisterClassEx(&windowClass)

	windowTitle, _ := syscall.UTF16PtrFromString("supcua-v2 Overlay")

	extendedStyle := uint32(win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW | win.WS_EX_LAYERED)

	overlayWindow.windowHandle = win.CreateWindowEx(
		extendedStyle,
		className,
		windowTitle,
		win.WS_POPUP,
		int32(initialX), int32(initialY),
		overlayWidth, overlayHeight,
		0, 0, windowClass.HInstance, nil,
	)

	if overlayWindow.windowHandle == 0 {
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

func (overlayWindow *OverlayWindow) windowProcedure(hwnd win.HWND, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case win.WM_PAINT:
		overlayWindow.paint()
		return 0
	case win.WM_RBUTTONUP:
		// Right-click could show context menu — handled via tray for now
		return 0
	case win.WM_NCHITTEST:
		// Allow dragging by treating entire window as title bar
		return win.HTCAPTION
	case win.WM_MOVE:
		overlayWindow.positionX = int(int16(lParam & 0xFFFF))
		overlayWindow.positionY = int(int16((lParam >> 16) & 0xFFFF))
	}
	return win.DefWindowProc(hwnd, message, wParam, lParam)
}

func (overlayWindow *OverlayWindow) paint() {
	var paintStruct win.PAINTSTRUCT
	deviceContext := win.BeginPaint(overlayWindow.windowHandle, &paintStruct)
	defer win.EndPaint(overlayWindow.windowHandle, &paintStruct)

	var backgroundColor uint32
	var statusText string
	if overlayWindow.appController.IsGlobalActive() {
		backgroundColor = colorGreenActive
		statusText = "ACTIVE"
		runningScriptName := overlayWindow.appController.GetRunningScriptName()
		if runningScriptName != "" {
			statusText = "RUN: " + runningScriptName
		}
	} else {
		backgroundColor = colorRedInactive
		statusText = "INACTIVE"
	}

	brushHandle, _, _ := procCreateSolidBrush.Call(uintptr(backgroundColor))
	clientRect := win.RECT{Left: 0, Top: 0, Right: overlayWidth, Bottom: overlayHeight}
	procFillRect.Call(uintptr(deviceContext), uintptr(unsafe.Pointer(&clientRect)), brushHandle)
	win.DeleteObject(win.HGDIOBJ(brushHandle))

	win.SetBkMode(deviceContext, win.TRANSPARENT)
	win.SetTextColor(deviceContext, win.COLORREF(colorWhiteText))

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
	overlayWindow.isVisible = !overlayWindow.isVisible
	if overlayWindow.isVisible {
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

// Destroy cleans up the overlay window.
func (overlayWindow *OverlayWindow) Destroy() {
	win.DestroyWindow(overlayWindow.windowHandle)
}
