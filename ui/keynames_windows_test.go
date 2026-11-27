//go:build windows

package ui

import "testing"

func TestKeyToNameMapsWindowsKeysToWin(t *testing.T) {
	for _, vk := range []uint32{0x5B, 0x5C} {
		if got := keyToName(vk); got != "win" {
			t.Errorf("keyToName(%#x) = %q", vk, got)
		}
	}
}
