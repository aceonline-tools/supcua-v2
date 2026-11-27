package ui

import (
	"reflect"
	"testing"

	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/ui/webview"
)

func TestKeyMods(t *testing.T) {
	if got := keyMods(false, false, false, false); got == nil || len(got) != 0 {
		t.Fatalf("no modifiers must be an empty, non-nil slice: %#v", got)
	}
	if got := keyMods(true, true, true, true); !reflect.DeepEqual(got, []string{"ctrl", "alt", "shift", "win"}) {
		t.Fatalf("canonical order: %v", got)
	}
	if got := keyMods(false, true, false, false); !reflect.DeepEqual(got, []string{"shift"}) {
		t.Fatalf("got %v", got)
	}
}

func TestKeyMods_WinChord(t *testing.T) {
	if got := keyMods(true, false, false, true); !reflect.DeepEqual(got, []string{"ctrl", "win"}) {
		t.Fatalf("got %v", got)
	}
}

type recSink struct {
	keys []string
	mods [][]string
}

func (r *recSink) Hotkey(model.Hotkey) {}
func (r *recSink) Key(k string)        { r.KeyWithMods(k, nil) }
func (r *recSink) KeyWithMods(k string, m []string) {
	r.keys = append(r.keys, k)
	r.mods = append(r.mods, m)
}
func (r *recSink) Mouse(int, int) {}

var _ bridge.CaptureSink = (*recSink)(nil)

func TestKeyCaptureHandlerForwardsTrackedState(t *testing.T) {
	s := &recSink{}
	h := keyCaptureHandler(s, func(vk uint32) string { return string(rune(vk)) })
	// async flags say "nothing held" (stale, as when the hook swallowed Ctrl); the tracked state says Ctrl+Win.
	if !h(webview.KeyEvent{VK: 'A', Down: true, Held: webview.Held{Ctrl: true, Win: true}}) {
		t.Fatal("key-down is swallowed")
	}
	if h(webview.KeyEvent{VK: 'A', Down: false}) {
		t.Fatal("key-up passes through")
	}
	h(webview.KeyEvent{VK: 'B', Down: true})
	if len(s.keys) != 2 || !reflect.DeepEqual(s.mods[0], []string{"ctrl", "win"}) {
		t.Fatalf("tracked state not forwarded: %v %v", s.keys, s.mods)
	}
	if s.mods[1] == nil || len(s.mods[1]) != 0 {
		t.Fatalf("no modifiers must be an empty non-nil slice: %#v", s.mods[1])
	}
}

type hkSink struct{ got []model.Hotkey }

func (s *hkSink) Hotkey(hk model.Hotkey)       { s.got = append(s.got, hk) }
func (s *hkSink) Key(string)                   {}
func (s *hkSink) KeyWithMods(string, []string) {}
func (s *hkSink) Mouse(int, int)               {}

func nameOf(vk uint32) string {
	switch vk {
	case 0x77:
		return "f8"
	case 0x1B:
		return "escape"
	}
	return ""
}

func ev(vk uint32, down bool, h webview.Held) webview.KeyEvent {
	return webview.KeyEvent{VK: vk, Down: down, Ctrl: h.Ctrl, Shift: h.Shift, Alt: h.Alt, Held: h}
}

func TestSettingsHandlerSingleModifiersKeepTheirSide(t *testing.T) {
	for _, c := range []struct {
		vk   uint32
		want string
	}{{0xA3, "rctrl"}, {0xA2, "lctrl"}, {0xA0, "lshift"}, {0xA1, "rshift"}, {0xA4, "lalt"}, {0xA5, "ralt"}} {
		s := &hkSink{}
		h := settingsHotkeyHandler(s, nameOf)
		h(ev(c.vk, true, webview.Held{Ctrl: true, Shift: true, Alt: true}))
		h(ev(c.vk, true, webview.Held{Ctrl: true, Shift: true, Alt: true})) // auto-repeat
		h(ev(c.vk, false, webview.Held{}))
		if len(s.got) != 1 || s.got[0].Key != c.want || len(s.got[0].Modifiers) != 0 {
			t.Errorf("vk %#x: got %+v, want single %q", c.vk, s.got, c.want)
		}
	}
}

func TestSettingsHandlerComboDoesNotAlsoFireItsModifiers(t *testing.T) {
	s := &hkSink{}
	h := settingsHotkeyHandler(s, nameOf)
	h(ev(0xA2, true, webview.Held{Ctrl: true}))
	h(ev(0xA4, true, webview.Held{Ctrl: true, Alt: true}))
	if !h(ev(0x77, true, webview.Held{Ctrl: true, Alt: true})) {
		t.Error("the combo's key must be swallowed")
	}
	h(ev(0x77, false, webview.Held{Ctrl: true, Alt: true}))
	h(ev(0xA4, false, webview.Held{Ctrl: true}))
	h(ev(0xA2, false, webview.Held{}))
	if len(s.got) != 1 || s.got[0].Key != "f8" || !reflect.DeepEqual(s.got[0].Modifiers, []string{"ctrl", "alt"}) {
		t.Fatalf("got %+v, want only Ctrl+Alt+F8", s.got)
	}
}

func TestSettingsHandlerTwoModifiersAndKeyThenModifierAreNotTaps(t *testing.T) {
	s := &hkSink{}
	h := settingsHotkeyHandler(s, nameOf)
	h(ev(0xA2, true, webview.Held{Ctrl: true}))
	h(ev(0xA0, true, webview.Held{Ctrl: true, Shift: true}))
	h(ev(0xA0, false, webview.Held{Ctrl: true}))
	h(ev(0xA2, false, webview.Held{}))
	h(ev(0x77, true, webview.Held{}))
	h(ev(0xA3, true, webview.Held{Ctrl: true}))
	h(ev(0xA3, false, webview.Held{}))
	if len(s.got) != 1 || s.got[0].Key != "f8" {
		t.Fatalf("got %+v, want only the F8", s.got)
	}
}

func TestSettingsHandlerEscapeIsReportedBareAndModifiersPassThrough(t *testing.T) {
	s := &hkSink{}
	h := settingsHotkeyHandler(s, nameOf)
	if h(ev(0xA3, true, webview.Held{Ctrl: true})) || h(ev(0xA3, false, webview.Held{})) {
		t.Error("modifier events must not be swallowed by the handler")
	}
	h(ev(0x1B, true, webview.Held{}))
	if len(s.got) != 2 || s.got[1].Key != "escape" || len(s.got[1].Modifiers) != 0 {
		t.Fatalf("got %+v", s.got)
	}
}
