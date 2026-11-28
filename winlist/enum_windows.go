//go:build windows

package winlist

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	dwmapi                       = windows.NewLazySystemDLL("dwmapi.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procGetWindow                = user32.NewProc("GetWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procDwmGetWindowAttribute    = dwmapi.NewProc("DwmGetWindowAttribute")
)

const (
	gwlExStyle       = ^uintptr(19) // -20
	wsExToolWindow   = 0x00000080
	wsExAppWindow    = 0x00040000
	gwOwner          = 4
	dwmwaCloaked     = 14
	maxTitleUTF16    = 512
	maxImageNameUnit = 32768
)

// SystemSource reads the live desktop.
type SystemSource struct{}

// Self is this process's id.
func Self() uint32 { return uint32(os.Getpid()) }

// Windows enumerates top-level windows via EnumWindows.
func (SystemSource) Windows(visit func(Window) bool) {
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if !visit(describe(hwnd)) {
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
}

func describe(hwnd uintptr) Window {
	var w Window
	if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
		return w
	}
	w.Visible = true
	ex, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle)
	w.ToolWindow = ex&wsExToolWindow != 0
	w.AppWindow = ex&wsExAppWindow != 0
	if o, _, _ := procGetWindow.Call(hwnd, gwOwner); o != 0 {
		w.Owned = true
	}
	var cloaked uint32
	if hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), 4); hr == 0 && cloaked != 0 {
		w.Cloaked = true
	}
	if n, _, _ := procGetWindowTextLengthW.Call(hwnd); n > 0 {
		buf := make([]uint16, maxTitleUTF16)
		l, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		w.Title = syscall.UTF16ToString(buf[:l])
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	w.PID = pid
	return w
}

// ExeName returns the executable base name using PROCESS_QUERY_LIMITED_INFORMATION only.
func (SystemSource) ExeName(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n := uint32(len(buf))
		err = windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
		if err == nil {
			return baseName(syscall.UTF16ToString(buf[:n])), nil
		}
		if err != windows.ERROR_INSUFFICIENT_BUFFER || len(buf) >= maxImageNameUnit {
			return "", err
		}
		buf = make([]uint16, len(buf)*2)
	}
}
