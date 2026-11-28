//go:build windows

package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/model"
)

func (s *SelfTest) hkstep(name string, ok bool, detail string) bool {
	return s.Res.Step("settings hotkey capture: "+name, ok, detail)
}

// settingsHotkeySuite records the Settings hotkeys with real input (SendInput) in the real main
// page: the main window must be the foreground window (the capture is gated on it), and a step
// whose input was not sent FAILS, never skips.
func (s *SelfTest) settingsHotkeySuite() {
	main := s.UI.HWND()
	win.SetForegroundWindow(win.HWND(main))
	if !waitFor(3*time.Second, func() bool { return win.GetForegroundWindow() == win.HWND(main) }) {
		s.hkstep("main window is the foreground window", false,
			fmt.Sprintf("UNVERIFIED: Windows did not grant the main window the foreground (foreground=%#x); SendInput was not sent", win.GetForegroundWindow()))
		return
	}
	if _, err := s.drive("hk-nav", `(function(){var b=document.querySelector('button.nav[aria-label="Settings"]');if(!b)return false;b.click();return true})()`, 5*time.Second); err != nil {
		s.hkstep("open Settings", false, err.Error())
		return
	}
	str := func(tag, expr string) string {
		r, err := s.drive(tag, expr, 4*time.Second)
		var out string
		if err != nil || json.Unmarshal(r.V, &out) != nil {
			return ""
		}
		return out
	}
	field := func(label string) string {
		return strings.TrimSpace(str("hk-field", `(function(){var b=document.querySelector('button[aria-label="Capture `+label+`"]');if(!b)return "";return b.closest('.hk-rec').querySelector('.hk-rec-value').textContent})()`))
	}
	saved := func(key string) string {
		return str("hk-saved", `(async function(){return (await __st.call('settings.get')).`+key+`})()`)
	}
	recording := func(label string) bool {
		return str("hk-rec", `(function(){var b=document.querySelector('button[aria-label="Capture `+label+`"]');return b&&b.getAttribute('aria-pressed')==='true'?'yes':'no'})()`) == "yes"
	}
	begin := func(label string) bool {
		if _, err := s.drive("hk-click", `(function(){var b=document.querySelector('button[aria-label="Capture `+label+`"]');if(!b)return false;b.click();return true})()`, 5*time.Second); err != nil {
			return false
		}
		if !waitFor(4*time.Second, func() bool { return recording(label) }) {
			return false
		}
		time.Sleep(400 * time.Millisecond) // the hook thread installs asynchronously
		return true
	}
	record := func(label, want string, hk model.Hotkey) (string, bool) {
		if !begin(label) {
			s.hkstep(want+": start recording "+label, false, "the Capture button did not enter the recording state")
			return "", false
		}
		if err := SendHotkey(hk); err != nil {
			s.hkstep(want+": SendInput", false, "input was not sent: "+err.Error())
			return "", false
		}
		var got string
		waitFor(5*time.Second, func() bool { got = field(label); return got == want })
		return got, true
	}

	if got, sent := record("Global hotkey", "Ctrl + Alt + F8", model.Hotkey{Modifiers: []string{"ctrl", "alt"}, Key: "f8"}); sent {
		cfg := saved("global_hotkey")
		s.hkstep(`Ctrl+Alt+F8 via SendInput is recorded as "Ctrl + Alt + F8"`, got == "Ctrl + Alt + F8" && cfg == "Ctrl + Alt + F8",
			fmt.Sprintf("field=%q saved global_hotkey=%q", got, cfg))
	}
	// Global is no longer RCtrl, so RCtrl is a valid, distinct value for the Capture hotkey (default "P").
	if got, sent := record("Capture hotkey", "RCtrl", model.Hotkey{Key: "rctrl"}); sent {
		cfg := saved("capture_hotkey")
		s.hkstep(`RCtrl tapped alone via SendInput is recorded as "RCtrl" (with its side)`, got == "RCtrl" && cfg == "RCtrl",
			fmt.Sprintf("field=%q saved capture_hotkey=%q", got, cfg))
	}
	if begin("Overlay hotkey") {
		before := saved("overlay_hotkey")
		if err := SendHotkey(model.Hotkey{Key: "escape"}); err != nil {
			s.hkstep("Esc: SendInput", false, "input was not sent: "+err.Error())
		} else {
			waitFor(4*time.Second, func() bool { return !recording("Overlay hotkey") })
			after := saved("overlay_hotkey")
			s.hkstep("Esc cancels and leaves the value unchanged", !recording("Overlay hotkey") && before != "" && before == after,
				fmt.Sprintf("overlay_hotkey %q -> %q recording=%v", before, after, recording("Overlay hotkey")))
		}
	} else {
		s.hkstep("Esc: start recording Overlay hotkey", false, "the Capture button did not enter the recording state")
	}
}
