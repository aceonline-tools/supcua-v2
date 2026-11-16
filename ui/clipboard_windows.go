//go:build windows

package ui

import (
	"errors"
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
)

// WriteClipboard puts text on the clipboard as CF_UNICODETEXT. Safe from any goroutine.
func WriteClipboard(text string) error {
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	var opened bool
	for i := 0; i < 10 && !opened; i++ { // the clipboard may be briefly held by another process
		r, _, _ := procOpenClipboard.Call(0)
		opened = r != 0
		if !opened {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !opened {
		return errors.New("clipboard busy")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	const gmemMoveable, cfUnicodeText = 0x0002, 13
	size := uintptr(len(u) * 2)
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return errors.New("clipboard alloc failed")
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return errors.New("clipboard lock failed")
	}
	copy(unsafe.Slice((*uint16)(*(*unsafe.Pointer)(unsafe.Pointer(&p))), len(u)), u)
	procGlobalUnlock.Call(h)
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 { // on success the system owns h
		return errors.New("set clipboard failed")
	}
	return nil
}
