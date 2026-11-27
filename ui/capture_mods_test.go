package ui

import (
	"reflect"
	"testing"
)

func TestKeyMods(t *testing.T) {
	if got := keyMods(false, false, false); got == nil || len(got) != 0 {
		t.Fatalf("no modifiers must be an empty, non-nil slice: %#v", got)
	}
	if got := keyMods(true, true, true); !reflect.DeepEqual(got, []string{"ctrl", "alt", "shift"}) {
		t.Fatalf("canonical order: %v", got)
	}
	if got := keyMods(false, true, false); !reflect.DeepEqual(got, []string{"shift"}) {
		t.Fatalf("got %v", got)
	}
}
