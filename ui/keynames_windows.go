//go:build windows

package ui

import "fmt"

// vkNames maps Win32 virtual-key codes to the key names used in scripts/hotkeys (ported from
// the removed walk editor so recorded hotkeys stay byte-identical).
var vkNames = func() map[uint32]string {
	m := map[uint32]string{
		0x20: "space", 0x0D: "enter", 0x1B: "escape", 0x09: "tab",
		0x08: "backspace", 0x2E: "delete", 0x2D: "insert",
		0x24: "home", 0x23: "end", 0x21: "pageup", 0x22: "pagedown",
		0x25: "left", 0x26: "up", 0x27: "right", 0x28: "down",
		0xBA: ";", 0xBB: "=", 0xBC: ",", 0xBD: "-", 0xBE: ".", 0xBF: "/",
		0xC0: "`", 0xDB: "[", 0xDC: `\`, 0xDD: "]", 0xDE: "'",
	}
	for i := 0; i <= 9; i++ {
		m[uint32(0x30+i)] = string(rune('0' + i))
	}
	for i := 0; i < 26; i++ {
		m[uint32(0x41+i)] = string(rune('a' + i))
	}
	for i := 1; i <= 12; i++ {
		m[uint32(0x6F+i)] = fmt.Sprintf("f%d", i)
	}
	return m
}()

// vkCodeToName is the hotkey-capture name of vk, or "" when it is not a bindable key.
func vkCodeToName(vk uint32) string { return vkNames[vk] }

func isModifierVK(vk uint32) bool {
	switch vk {
	case 0x10, 0x11, 0x12, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5:
		return true
	}
	return false
}

// keyToName names the key typed into an action's Key field (parity with the removed
// keyToString): letters, digits, F1-F24, navigation keys, modifiers, else 0xNN.
func keyToName(vk uint32) string {
	switch {
	case vk >= 0x41 && vk <= 0x5A:
		return string(rune('a' + (vk - 0x41)))
	case vk >= 0x30 && vk <= 0x39:
		return string(rune('0' + (vk - 0x30)))
	case vk >= 0x70 && vk <= 0x87:
		return fmt.Sprintf("f%d", vk-0x70+1)
	}
	switch vk {
	case 0x20:
		return "space"
	case 0x0D:
		return "enter"
	case 0x1B:
		return "escape"
	case 0x09:
		return "tab"
	case 0x08:
		return "backspace"
	case 0x2E:
		return "delete"
	case 0x2D:
		return "insert"
	case 0x24:
		return "home"
	case 0x23:
		return "end"
	case 0x21:
		return "pageup"
	case 0x22:
		return "pagedown"
	case 0x26:
		return "up"
	case 0x28:
		return "down"
	case 0x25:
		return "left"
	case 0x27:
		return "right"
	case 0x11, 0xA2, 0xA3:
		return "ctrl"
	case 0x10, 0xA0, 0xA1:
		return "shift"
	case 0x12, 0xA4, 0xA5:
		return "alt"
	}
	return fmt.Sprintf("0x%02X", vk)
}
