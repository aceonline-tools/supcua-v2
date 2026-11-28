//go:build windows

package winlist

import (
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	dwmapi                       = windows.NewLazySystemDLL("dwmapi.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
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

var (
	enumOnce sync.Once
	enumCB   uintptr
	enumMu   sync.Mutex
	enumNext uintptr
	enumReg  = map[uintptr]enumState{}
)

type enumState struct {
	visit   func(Window) bool
	expired func() bool
}

// enumProc is the single EnumWindows callback for the whole process: syscall.NewCallback slots
// are never freed, so per-call state travels through lParam into enumReg instead.
func enumProc(hwnd, lparam uintptr) uintptr {
	enumMu.Lock()
	st, ok := enumReg[lparam]
	enumMu.Unlock()
	if !ok {
		return 0
	}
	w, fine := describe(hwnd, st.expired)
	if !fine || !st.visit(w) {
		return 0
	}
	return 1
}

// Windows enumerates top-level windows via EnumWindows. Only cheap attributes are read here; the
// title is fetched later through Title, for windows that survive the filters.
func (SystemSource) Windows(visit func(Window) bool, expired func() bool) {
	enumOnce.Do(func() { enumCB = syscall.NewCallback(enumProc) })
	enumMu.Lock()
	enumNext++
	id := enumNext
	enumReg[id] = enumState{visit, expired}
	enumMu.Unlock()
	defer func() {
		enumMu.Lock()
		delete(enumReg, id)
		enumMu.Unlock()
	}()
	procEnumWindows.Call(enumCB, id)
}

// describe reads the cheap window attributes, checking the budget between native calls; ok is
// false when the budget ran out part-way.
func describe(hwnd uintptr, expired func() bool) (Window, bool) {
	w := Window{H: hwnd}
	if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
		return w, true
	}
	w.Visible = true
	if expired() {
		return w, false
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	w.PID = pid
	if expired() {
		return w, false
	}
	ex, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle)
	w.ToolWindow = ex&wsExToolWindow != 0
	w.AppWindow = ex&wsExAppWindow != 0
	if o, _, _ := procGetWindow.Call(hwnd, gwOwner); o != 0 {
		w.Owned = true
	}
	if expired() {
		return w, false
	}
	var cloaked uint32
	if hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), 4); hr == 0 && cloaked != 0 {
		w.Cloaked = true
	}
	return w, true
}

// Title reads the window text. The caller has already excluded this process's own windows.
func (SystemSource) Title(h uintptr) string {
	buf := make([]uint16, maxTitleUTF16)
	l, _, _ := procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:l])
}

// ExeName returns the executable base name using PROCESS_QUERY_LIMITED_INFORMATION only.
func (SystemSource) ExeName(pid uint32, expired func() bool) (string, error) {
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
		if err != windows.ERROR_INSUFFICIENT_BUFFER || len(buf) >= maxImageNameUnit || expired() {
			return "", err
		}
		buf = make([]uint16, len(buf)*2)
	}
}
