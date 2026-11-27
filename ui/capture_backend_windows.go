//go:build windows

package ui

import (
	"sync"

	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/ui/webview"
)

// CaptureBackend implements bridge.CaptureBackend: keyboard capture runs on its own locked
// hook thread (webview.StartKeyCapture, one process-wide hook callback), mouse capture is
// the 50 ms P-key poll. At most one capture is active; Stop is idempotent.
type CaptureBackend struct {
	// HWND returns the host window handle; keys are only captured while it is the foreground window.
	HWND func() uintptr

	mu        sync.Mutex
	stopKey   func()
	stopMouse func()
}

func (c *CaptureBackend) Start(mode bridge.CaptureMode, pick model.Hotkey, sink bridge.CaptureSink) error {
	c.Stop()
	c.mu.Lock()
	defer c.mu.Unlock()
	switch mode {
	case bridge.CaptureHotkey:
		stop, err := webview.StartKeyCapture(webview.ForegroundIs(c.HWND), func(ev webview.KeyEvent) bool {
			if !ev.Down || isModifierVK(ev.VK) {
				return false
			}
			name := vkCodeToName(ev.VK)
			if name == "" {
				return false
			}
			mods := []string{}
			if ev.Ctrl {
				mods = append(mods, "ctrl")
			}
			if ev.Shift {
				mods = append(mods, "shift")
			}
			if ev.Alt {
				mods = append(mods, "alt")
			}
			sink.Hotkey(model.Hotkey{Modifiers: mods, Key: name})
			return true
		})
		if err != nil {
			return err
		}
		c.stopKey = stop
	case bridge.CaptureKey:
		stop, err := webview.StartKeyCapture(webview.ForegroundIs(c.HWND), keyCaptureHandler(sink, keyToName))
		if err != nil {
			return err
		}
		c.stopKey = stop
	case bridge.CaptureMouse:
		c.stopMouse = webview.StartMousePick(hotkey.KeyNameToVirtualKeyCode(pick.Key), sink.Mouse)
	}
	return nil
}

func (c *CaptureBackend) Stop() {
	c.mu.Lock()
	stopKey, stopMouse := c.stopKey, c.stopMouse
	c.stopKey, c.stopMouse = nil, nil
	c.mu.Unlock()
	if stopKey != nil {
		stopKey()
	}
	if stopMouse != nil {
		stopMouse()
	}
}
