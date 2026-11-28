//go:build windows

package hotkey

import (
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	procSetWindowsHookEx = user32DLL.NewProc("SetWindowsHookExW")
	procCallNextHookEx   = user32DLL.NewProc("CallNextHookEx")
	procGetMessage       = user32DLL.NewProc("GetMessageW")

	previewHookStarted atomic.Bool
	previewManager     atomic.Pointer[Manager]
	previewEvents      = make(chan previewEvent, 256)
	acceptInjected     atomic.Bool
)

type previewEvent struct {
	vk   uint32
	down bool
}

// SetAcceptInjected lets the preview hook treat SendInput keys as real ones. Self-test only: a
// production hook ignores injected keys so a script's own key events never drive the overlay.
func SetAcceptInjected(on bool) { acceptInjected.Store(on) }

// StartPreview enables the event-driven key-down detection, installed with the first bare-modifier hotkey: a WH_KEYBOARD_LL hook on its own
// thread (it never swallows a key and only does non-blocking work) and a worker that feeds the
// polled hotkeys' previews. It costs nothing while no key is pressed. Call once.
func (m *Manager) StartPreview() { m.previewWanted.Store(true) }

// ensurePreviewHook installs the hook the first time a bare-modifier hotkey is registered.
func (m *Manager) ensurePreviewHook() {
	if !m.previewWanted.Load() || !previewHookStarted.CompareAndSwap(false, true) {
		return
	}
	previewManager.Store(m)
	go previewWorker()
	go runPreviewHook()
}

// SetPreview attaches a preview to the polled hotkey registered as id. It reports false for a
// hotkey that is not a polled bare modifier.
func (m *Manager) SetPreview(id int, p Preview) bool { return m.polls.setPreview(id, p) }

func previewWorker() {
	for ev := range previewEvents {
		if m := previewManager.Load(); m != nil {
			m.polls.key(ev.vk, ev.down, m.pollMute.muted(), m.stamp)
		}
	}
}

type kbdHook struct {
	vkCode, scanCode, flags, time uint32
	extra                         uintptr
}

const llkhfInjected = 0x10

func previewHookProc(nCode int, wParam, lParam uintptr) uintptr {
	if nCode >= 0 {
		kb := *(**kbdHook)(unsafe.Pointer(&lParam))
		if kb.flags&llkhfInjected == 0 || acceptInjected.Load() {
			var down bool
			switch wParam {
			case 0x100, 0x104:
				down = true
			case 0x101, 0x105:
			default:
				goto next
			}
			select {
			case previewEvents <- previewEvent{kb.vkCode, down}:
			default: // full: the poll still ends any preview whose key is up
			}
		}
	}
next:
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

func runPreviewHook() {
	runtime.LockOSThread()
	h, _, _ := procSetWindowsHookEx.Call(13, syscall.NewCallback(previewHookProc), 0, 0)
	if h == 0 {
		previewHookStarted.Store(false)
		return
	}
	var msg [48]byte
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
	}
}
