package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"github.com/lxn/win"
)

// host is the raw top-level window that carries the WebView2 controller.
// It is deliberately NOT a child of the walk form: walk's pump calls
// IsDialogMessage(form, msg) for every message (walk mainloop_default.go:35),
// which would swallow Tab/Enter/Esc destined for controls inside a child.
type host struct {
	hwnd     win.HWND
	browser  *edge.Chromium
	quitting bool
	onClose  func()
}

var theHost *host

func hostProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	h := theHost
	if h != nil {
		switch msg {
		case win.WM_SIZE:
			if h.browser != nil {
				h.browser.Resize()
			}
		case win.WM_MOVE, win.WM_MOVING:
			if h.browser != nil {
				_ = h.browser.NotifyParentWindowPositionChanged()
			}
		case win.WM_SETFOCUS:
			if h.browser != nil {
				h.browser.Focus()
			}
		case win.WM_SYSCOMMAND:
			if wp&0xFFF0 == win.SC_MINIMIZE { // minimize hides to tray, as ui/main_window.go:299
				win.ShowWindow(hwnd, win.SW_HIDE)
				return 0
			}
		case win.WM_CLOSE:
			if !h.quitting && h.onClose != nil { // X exits for real, as ui/main_window.go:308
				h.onClose()
				return 0
			}
		}
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

func newHost(title string) (*host, error) {
	h := &host{}
	theHost = h
	className, _ := syscall.UTF16PtrFromString("spike_host")
	wc := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		LpfnWndProc:   newCallback(hostProc),
		HInstance:     win.GetModuleHandle(nil),
		LpszClassName: className,
		HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)),
		HbrBackground: win.HBRUSH(win.GetStockObject(win.WHITE_BRUSH)),
	}
	if win.RegisterClassEx(&wc) == 0 {
		return nil, fmt.Errorf("register host class failed")
	}
	t, _ := syscall.UTF16PtrFromString(title)
	h.hwnd = win.CreateWindowEx(0, className, t, win.WS_OVERLAPPEDWINDOW,
		win.CW_USEDEFAULT, win.CW_USEDEFAULT, 900, 640, 0, 0, wc.HInstance, nil)
	if h.hwnd == 0 {
		return nil, fmt.Errorf("create host window failed")
	}
	return h, nil
}

// attachWebView creates the WebView2 environment/controller inside the host.
// Chromium.Embed pumps messages itself until the controller exists, so it must
// run on the UI thread, outside any WebView2 callback.
func (h *host) attachWebView(onMessage func(string), onNavDone func()) error {
	dataDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "spike-webview2")
	b := edge.NewChromium()
	b.DataPath = dataDir
	b.MessageCallback = onMessage
	b.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		onNavDone()
	}
	if !b.Embed(uintptr(h.hwnd)) {
		return fmt.Errorf("WebView2 environment creation failed")
	}
	if s, err := b.GetSettings(); err == nil && s != nil {
		_ = s.PutAreDefaultContextMenusEnabled(false)
		_ = s.PutIsStatusBarEnabled(false)
		_ = s.PutAreDefaultScriptDialogsEnabled(false)
	}
	h.browser = b
	b.Resize()
	return nil
}

func (h *host) Show() {
	win.ShowWindow(h.hwnd, win.SW_SHOW)
	win.SetForegroundWindow(h.hwnd)
}

func (h *host) Hide() { win.ShowWindow(h.hwnd, win.SW_HIDE) }

func (h *host) Visible() bool { return win.IsWindowVisible(h.hwnd) }

func (h *host) Destroy() {
	h.quitting = true
	win.DestroyWindow(h.hwnd)
}
