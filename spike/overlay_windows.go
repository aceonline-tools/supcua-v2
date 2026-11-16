package main

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	gdi32                          = windows.NewLazySystemDLL("gdi32.dll")
	procCreateSolidBrush           = gdi32.NewProc("CreateSolidBrush")
	procFillRect                   = user32.NewProc("FillRect")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
)

const (
	overlayWidth  = 136
	overlayHeight = 32
)

// overlay is the raw layered topmost HUD, same pattern as ui/overlay.go. It has
// no message loop of its own: it relies on the UI thread's pump.
type overlay struct {
	hwnd      win.HWND
	threadID  uint32
	text      atomic.Pointer[string]
	paints    atomic.Int64
	visible   bool
	draggable bool
}

var theOverlay *overlay // one overlay per process; the single WndProc reads it

func overlayProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	o := theOverlay
	if o != nil {
		switch msg {
		case win.WM_PAINT:
			o.paint()
			return 0
		case win.WM_NCHITTEST:
			return win.HTCAPTION
		}
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

func newOverlay(x, y int32) (*overlay, error) {
	o := &overlay{visible: true}
	theOverlay = o
	className, _ := syscall.UTF16PtrFromString("spike_overlay")
	wc := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		LpfnWndProc:   newCallback(overlayProc),
		HInstance:     win.GetModuleHandle(nil),
		LpszClassName: className,
		HbrBackground: win.HBRUSH(win.GetStockObject(win.BLACK_BRUSH)),
	}
	win.RegisterClassEx(&wc)
	title, _ := syscall.UTF16PtrFromString("spike Overlay")
	ex := uint32(win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW | win.WS_EX_LAYERED | win.WS_EX_TRANSPARENT)
	o.hwnd = win.CreateWindowEx(ex, className, title, win.WS_POPUP, x, y, overlayWidth, overlayHeight, 0, 0, wc.HInstance, nil)
	if o.hwnd == 0 {
		return nil, fmt.Errorf("create overlay window failed")
	}
	o.threadID = win.GetCurrentThreadId()
	procSetLayeredWindowAttributes.Call(uintptr(o.hwnd), 0, 200, 0x02)
	s := "IDLE"
	o.text.Store(&s)
	win.ShowWindow(o.hwnd, win.SW_SHOWNOACTIVATE)
	return o, nil
}

func (o *overlay) paint() {
	var ps win.PAINTSTRUCT
	dc := win.BeginPaint(o.hwnd, &ps)
	defer win.EndPaint(o.hwnd, &ps)
	o.paints.Add(1)

	brush, _, _ := procCreateSolidBrush.Call(uintptr(0x00404040))
	rc := win.RECT{Left: 0, Top: 0, Right: overlayWidth, Bottom: overlayHeight}
	procFillRect.Call(uintptr(dc), uintptr(unsafe.Pointer(&rc)), brush)
	win.DeleteObject(win.HGDIOBJ(brush))

	win.SetBkMode(dc, win.TRANSPARENT)
	win.SetTextColor(dc, win.COLORREF(0x0000FF00))
	txt, _ := syscall.UTF16FromString(*o.text.Load())
	win.DrawTextEx(dc, &txt[0], int32(len(txt)-1), &rc, win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE, nil)
}

// Refresh must run on the UI thread.
func (o *overlay) Refresh(text string) {
	o.text.Store(&text)
	win.InvalidateRect(o.hwnd, nil, true)
	win.UpdateWindow(o.hwnd)
}

func (o *overlay) SetVisible(v bool) {
	o.visible = v
	if v {
		win.ShowWindow(o.hwnd, win.SW_SHOWNOACTIVATE)
	} else {
		win.ShowWindow(o.hwnd, win.SW_HIDE)
	}
}

func (o *overlay) SetDraggable(d bool) {
	o.draggable = d
	ex := win.GetWindowLong(o.hwnd, win.GWL_EXSTYLE)
	if d {
		ex &^= win.WS_EX_TRANSPARENT
	} else {
		ex |= win.WS_EX_TRANSPARENT
	}
	win.SetWindowLong(o.hwnd, win.GWL_EXSTYLE, ex)
}

func (o *overlay) Destroy() { win.DestroyWindow(o.hwnd) }
