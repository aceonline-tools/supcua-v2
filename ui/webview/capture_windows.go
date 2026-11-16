package webview

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// KeyEvent is one low-level keyboard event seen while a capture is active.
type KeyEvent struct {
	VK               uint32
	Down             bool
	Ctrl, Shift, Alt bool
}

// ErrCaptureActive: only one keyboard capture can run at a time.
var ErrCaptureActive = errors.New("a keyboard capture is already active")

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
)

type kbdllhookstruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

type keySession struct {
	isActive func() bool
	onKey    func(KeyEvent) bool
	hook     atomic.Uintptr
}

var (
	activeKeySession atomic.Pointer[keySession]
	keyHookOnce      sync.Once
	keyHookCallback  uintptr // created exactly once per process (NewCallback slots are never freed)
)

func keyHookProc(nCode int, wParam, lParam uintptr) uintptr {
	s := activeKeySession.Load()
	if s == nil {
		r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return r
	}
	if nCode >= 0 && s.isActive() {
		k := (*kbdllhookstruct)(ptrFrom(lParam))
		isAlt := k.vkCode == 0x12 || k.vkCode == 0xA4 || k.vkCode == 0xA5
		if isAlt {
			return 1 // keep Windows out of menu mode while capturing (parity: script_edit_dialog.go)
		}
		down := wParam == 0x0100 || wParam == 0x0104
		if down || wParam == 0x0101 || wParam == 0x0105 {
			ev := KeyEvent{VK: k.vkCode, Down: down, Ctrl: asyncDown(0x11), Shift: asyncDown(0x10), Alt: asyncDown(0x12)}
			if s.onKey(ev) {
				return 1
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(s.hook.Load(), uintptr(nCode), wParam, lParam)
	return r
}

func asyncDown(vk uintptr) bool {
	s, _, _ := procGetAsyncKeyState.Call(vk)
	return uint16(s)&0x8000 != 0
}

// StartKeyCapture installs a WH_KEYBOARD_LL hook on its own locked thread (T_CAPTURE) with
// its own message pump, so the hook answers inside LowLevelHooksTimeout even if the UI
// thread is busy. isActive gates events (e.g. host window is foreground); onKey runs on the
// hook thread, must not block, and returns true to swallow the key. Alt is always swallowed
// while active. stop unhooks and waits for the thread (2 s bound); it is idempotent.
func StartKeyCapture(isActive func() bool, onKey func(KeyEvent) bool) (stop func(), err error) {
	keyHookOnce.Do(func() { keyHookCallback = newCallback(keyHookProc) })
	s := &keySession{isActive: isActive, onKey: onKey}
	if !activeKeySession.CompareAndSwap(nil, s) {
		return nil, ErrCaptureActive
	}
	type started struct {
		tid uint32
		err error
	}
	ready := make(chan started, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		h, _, e := procSetWindowsHookExW.Call(13, keyHookCallback, 0, 0) // WH_KEYBOARD_LL
		if h == 0 {
			ready <- started{err: e}
			return
		}
		s.hook.Store(h)
		ready <- started{tid: windows.GetCurrentThreadId()}
		var msg win.MSG
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 { // WM_QUIT or error
				break
			}
		}
		procUnhookWindowsHookEx.Call(h)
	}()
	st := <-ready
	if st.err != nil {
		activeKeySession.Store(nil)
		<-done
		return nil, st.err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			activeKeySession.Store(nil)
			procPostThreadMessageW.Call(uintptr(st.tid), 0x0012, 0, 0) // WM_QUIT
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		})
	}, nil
}

// CursorPos returns the current cursor position in screen pixels.
func CursorPos() (x, y int, ok bool) {
	var p win.POINT
	if !win.GetCursorPos(&p) {
		return 0, 0, false
	}
	return int(p.X), int(p.Y), true
}

// StartMousePick polls GetAsyncKeyState(vk) every 50 ms (the "P key" coordinate capture,
// parity with ui/script_edit_dialog.go registerPickHotkey) and calls onPick with the cursor
// position on every press. One goroutine per call, ended by stop (idempotent, waits for it).
func StartMousePick(vk uint32, onPick func(x, y int)) (stop func()) {
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-t.C:
				if vk == 0 {
					continue
				}
				if r, _, _ := procGetAsyncKeyState.Call(uintptr(vk)); r&1 != 0 { // pressed since last call
					if x, y, ok := CursorPos(); ok {
						onPick(x, y)
					}
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(quit); <-done }) }
}

// ForegroundIs returns an isActive gate: true while hwnd() is the foreground window.
func ForegroundIs(hwnd func() uintptr) func() bool {
	return func() bool {
		fg, _, _ := procGetForegroundWindow.Call()
		return fg != 0 && fg == hwnd()
	}
}
