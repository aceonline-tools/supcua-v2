package webview

import (
	"fmt"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	dwmapi                = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttrb = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	dwmaUseImmersiveDarkMode    = 20 // Windows 10 20H1+ / 11
	dwmaUseImmersiveDarkModeOld = 19 // Windows 10 1903-1909
)

// ApplyDarkTitleBar switches the native title bar of hwnd between dark and light
// (DWMWA_USE_IMMERSIVE_DARK_MODE) and forces a frame repaint. UI thread only.
func ApplyDarkTitleBar(hwnd uintptr, dark bool) error {
	if err := procDwmSetWindowAttrb.Find(); err != nil {
		return err
	}
	v := int32(0)
	if dark {
		v = 1
	}
	r, _, _ := procDwmSetWindowAttrb.Call(hwnd, dwmaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	if int32(r) < 0 {
		r, _, _ = procDwmSetWindowAttrb.Call(hwnd, dwmaUseImmersiveDarkModeOld, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
		if int32(r) < 0 {
			return fmt.Errorf("DwmSetWindowAttribute: HRESULT 0x%08X", uint32(r))
		}
	}
	win.SetWindowPos(win.HWND(hwnd), 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
	return nil
}
