//go:build windows

package executor

import "unsafe"

// SimulatePhysicalRCtrl runs the keyboard-lock hook procedure for a physical (non-injected) RCtrl
// event, which SendInput cannot produce (it is always flagged injected). Self-test only.
func SimulatePhysicalRCtrl(down bool) {
	wm := uintptr(0x0101)
	if down {
		wm = 0x0100
	}
	kbs := kbdllhookstructExec{vkCode: 0xA3}
	keyboardHookProc(0, wm, uintptr(unsafe.Pointer(&kbs)))
}
