//go:build windows

package hotkey

import (
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	procSetWindowsHookEx    = user32DLL.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32DLL.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32DLL.NewProc("CallNextHookEx")
	procGetMessage          = user32DLL.NewProc("GetMessageW")
	procPeekMessageHook     = user32DLL.NewProc("PeekMessageW")
	procPostThreadMessage   = user32DLL.NewProc("PostThreadMessageW")

	previewHookCB  = syscall.NewCallback(previewHookProc)
	hookSink       atomic.Pointer[hookTarget]
	acceptInjected atomic.Bool
)

type hookTarget struct {
	mask *keyMask
	emit func(previewEvent)
}

// SetAcceptInjected lets the preview hook treat SendInput keys as real ones. Self-test only: a
// production hook ignores injected keys so a script's own key events never drive the overlay.
func SetAcceptInjected(on bool) { acceptInjected.Store(on) }

// StartPreview enables the event-driven key-down detection, installed with the first bare-modifier
// hotkey: a WH_KEYBOARD_LL hook on its own thread that never swallows a key and only enqueues
// registered keys. It costs nothing while no key is pressed.
func (m *Manager) StartPreview() { m.previewWanted.Store(true) }

// StopPreview unhooks and ends the worker (app shutdown). A later registration does not restart it.
func (m *Manager) StopPreview() {
	m.previewWanted.Store(false)
	m.previewRun.stop()
}

// ensurePreviewHook installs the hook the first time a bare-modifier hotkey is registered, and again
// if it died.
func (m *Manager) ensurePreviewHook() {
	if m.previewWanted.Load() {
		_ = m.previewRun.start()
	}
}

// SetPreview attaches a preview to the polled hotkey registered as id. It reports false for a
// hotkey that is not a polled bare modifier.
func (m *Manager) SetPreview(id int, p Preview) bool { return m.polls.setPreview(id, p) }

type kbdHook struct {
	vkCode, scanCode, flags, time uint32
	extra                         uintptr
}

const llkhfInjected = 0x10

func previewHookProc(nCode int, wParam, lParam uintptr) uintptr {
	if nCode >= 0 {
		if t := hookSink.Load(); t != nil {
			kb := *(**kbdHook)(unsafe.Pointer(&lParam))
			switch wParam {
			case 0x100, 0x104:
				queueKey(t.mask, t.emit, kb.vkCode, true, kb.flags&llkhfInjected != 0, acceptInjected.Load())
			case 0x101, 0x105:
				queueKey(t.mask, t.emit, kb.vkCode, false, kb.flags&llkhfInjected != 0, acceptInjected.Load())
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

// installPreviewHook runs the hook on its own locked thread. The returned stop posts WM_QUIT,
// waits for the thread to unhook and exit. dead is called if the thread ends on its own.
func installPreviewHook(mask *keyMask) func(emit func(previewEvent), dead func()) (func(), error) {
	return func(emit func(previewEvent), dead func()) (func(), error) {
		type ready struct {
			tid uint32
			err error
		}
		rc := make(chan ready, 1)
		exited := make(chan struct{})
		var stopping atomic.Bool
		go func() {
			runtime.LockOSThread()
			defer close(exited)
			var msg [48]byte
			procPeekMessageHook.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 0) // creates the thread's queue
			t := &hookTarget{mask: mask, emit: emit}
			hookSink.Store(t)
			h, _, err := procSetWindowsHookEx.Call(13, previewHookCB, 0, 0)
			if h == 0 {
				hookSink.CompareAndSwap(t, nil)
				rc <- ready{err: err}
				return
			}
			rc <- ready{tid: currentThreadID()}
			for {
				r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
				if int32(r) <= 0 {
					break
				}
			}
			procUnhookWindowsHookEx.Call(h)
			hookSink.CompareAndSwap(t, nil)
			if !stopping.Load() {
				dead()
			}
		}()
		res := <-rc
		if res.err != nil {
			<-exited
			return nil, res.err
		}
		return func() {
			stopping.Store(true)
			procPostThreadMessage.Call(uintptr(res.tid), 0x0012, 0, 0)
			<-exited
		}, nil
	}
}
