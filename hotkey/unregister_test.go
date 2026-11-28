package hotkey

import (
	"errors"
	"sync"
	"testing"
)

func TestRemoveRegistrationDropsCallbackEvenOnOSFailure(t *testing.T) {
	var mu sync.RWMutex
	cbs := map[int]HotkeyCallback{1: func(int, uint64) {}, 2: func(int, uint64) {}}

	if err := removeRegistration(&mu, cbs, 1, func(int) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, ok := cbs[1]; ok {
		t.Fatal("callback 1 kept after success")
	}

	err := removeRegistration(&mu, cbs, 2, func(int) error { return errors.New("boom") })
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := cbs[2]; ok {
		t.Fatal("callback 2 kept after OS failure")
	}
}
