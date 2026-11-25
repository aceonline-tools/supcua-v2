//go:build windows

package ui

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procOpenClipboard    = user32DLL.NewProc("OpenClipboard")
	procCloseClipboard   = user32DLL.NewProc("CloseClipboard")
	procEmptyClipboard   = user32DLL.NewProc("EmptyClipboard")
	procSetClipboardData = user32DLL.NewProc("SetClipboardData")
	procGlobalAlloc      = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalAlloc")
	procGlobalLock       = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalLock")
	procGlobalUnlock     = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalUnlock")
	procGlobalFree       = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalFree")
)

const (
	gmemMoveable  = 0x0002
	cfUnicodeText = 13
)

type winClip struct{}

func (winClip) Open(owner uintptr) bool {
	for i := 0; i < 10; i++ { // another process may hold the clipboard briefly
		if r, _, _ := procOpenClipboard.Call(owner); r != 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
func (winClip) Close()      { procCloseClipboard.Call() }
func (winClip) Empty() bool { r, _, _ := procEmptyClipboard.Call(); return r != 0 }
func (winClip) Alloc(units int) uintptr {
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(units*2))
	return h
}
func (winClip) Write(h uintptr, text []uint16) bool {
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return false
	}
	copy(unsafe.Slice((*uint16)(*(*unsafe.Pointer)(unsafe.Pointer(&p))), len(text)), text)
	procGlobalUnlock.Call(h)
	return true
}
func (winClip) Free(h uintptr) { procGlobalFree.Call(h) }
func (winClip) Set(h uintptr) bool {
	r, _, _ := procSetClipboardData.Call(cfUnicodeText, h)
	return r != 0
}

// WriteClipboard puts text on the clipboard as CF_UNICODETEXT owned by owner (the host window
// handle). Call it on the UI thread: the clipboard owner must be a window of the calling thread.
func WriteClipboard(owner uintptr, text string) error {
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	return writeClipboardText(winClip{}, owner, u)
}
