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
