//go:build windows

package executor

import (
	"fmt"
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32DLL              = windows.NewLazySystemDLL("user32.dll")
	procSetCursorPos       = user32DLL.NewProc("SetCursorPos")
	procGetCursorPos       = user32DLL.NewProc("GetCursorPos")
	procSendInput          = user32DLL.NewProc("SendInput")
	procMapVirtualKey      = user32DLL.NewProc("MapVirtualKeyW")
	procVkKeyScan          = user32DLL.NewProc("VkKeyScanW")
	procGetForegroundWindow = user32DLL.NewProc("GetForegroundWindow")
	procGetWindowTextW     = user32DLL.NewProc("GetWindowTextW")
)

// INPUT type constants
const (
	inputTypeMouse    uint32 = 0
	inputTypeKeyboard uint32 = 1
)

// MOUSEINPUT dwFlags
const (
	mouseEventFlagLeftDown  uint32 = 0x0002
	mouseEventFlagLeftUp    uint32 = 0x0004
	mouseEventFlagRightDown uint32 = 0x0008
	mouseEventFlagRightUp   uint32 = 0x0010
)

// KEYBDINPUT dwFlags
const (
	keyEventFlagExtendedKey uint32 = 0x0001
	keyEventFlagKeyUp       uint32 = 0x0002
)

// extendedVKCodes are keys that require KEYEVENTF_EXTENDEDKEY to work correctly.
var extendedVKCodes = map[uint16]bool{
	0xA2: true, // VK_LCONTROL
	0xA3: true, // VK_RCONTROL
	0xA4: true, // VK_LMENU (LAlt)
	0xA5: true, // VK_RMENU (RAlt)
	0x2D: true, // VK_INSERT
	0x2E: true, // VK_DELETE
	0x24: true, // VK_HOME
	0x23: true, // VK_END
	0x21: true, // VK_PRIOR (PageUp)
	0x22: true, // VK_NEXT (PageDown)
	0x25: true, // VK_LEFT
	0x26: true, // VK_UP
	0x27: true, // VK_RIGHT
	0x28: true, // VK_DOWN
	0x5B: true, // VK_LWIN
	0x5C: true, // VK_RWIN
}

// These mirror the Win32 INPUT structure layout exactly on 64-bit Windows.
// INPUT = 40 bytes: type(4) + pad(4) + union(32)
// MOUSEINPUT union layout: dx(4)+dy(4)+mouseData(4)+dwFlags(4)+time(4)+pad(4)+extra(8)
// KEYBDINPUT union layout: wVk(2)+wScan(2)+dwFlags(4)+time(4)+pad(4)+extra(8)+pad(8)

type mouseINPUT struct {
	typ       uint32
	_         uint32 // padding
	dx        int32
	dy        int32
	mouseData uint32
	dwFlags   uint32
	time      uint32
	_2        uint32 // padding before extraInfo
	extraInfo uint64
}

type keyboardINPUT struct {
	typ       uint32
	_         uint32 // padding
	wVk       uint16
	wScan     uint16
	dwFlags   uint32
	time      uint32
	_2        uint32 // padding before extraInfo
	extraInfo uint64
	_3        uint32 // pad to 40 bytes total
}

func sendMouseInput(dwFlags uint32) {
	inp := mouseINPUT{typ: inputTypeMouse, dwFlags: dwFlags}
	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
	if ret == 0 {
		log.Printf("SendInput mouse failed: %v", err)
	}
}

func sendKeyInput(vk uint16, sc uint16, flags uint32) {
	inp := keyboardINPUT{typ: inputTypeKeyboard, wVk: vk, wScan: sc, dwFlags: flags}
	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
	if ret == 0 {
		log.Printf("SendInput key failed vk=%d flags=%d: %v", vk, flags, err)
	}
}

// Virtual key codes
var virtualKeyCodeMap = map[string]uint16{
	"ctrl": 0x11, "lctrl": 0xA2, "rctrl": 0xA3,
	"shift": 0x10, "lshift": 0xA0, "rshift": 0xA1,
	"alt": 0x12, "lalt": 0xA4, "ralt": 0xA5,
	"win": 0x5B,
	"enter": 0x0D, "return": 0x0D,
	"tab": 0x09, "escape": 0x1B, "esc": 0x1B,
	"space": 0x20, "backspace": 0x08, "delete": 0x2E,
	"insert": 0x2D, "home": 0x24, "end": 0x23,
	"pageup": 0x21, "pagedown": 0x22,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73,
	"f5": 0x74, "f6": 0x75, "f7": 0x76, "f8": 0x77,
	"f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
	"0": 0x30, "1": 0x31, "2": 0x32, "3": 0x33, "4": 0x34,
	"5": 0x35, "6": 0x36, "7": 0x37, "8": 0x38, "9": 0x39,
	"a": 0x41, "b": 0x42, "c": 0x43, "d": 0x44, "e": 0x45,
	"f": 0x46, "g": 0x47, "h": 0x48, "i": 0x49, "j": 0x4A,
	"k": 0x4B, "l": 0x4C, "m": 0x4D, "n": 0x4E, "o": 0x4F,
	"p": 0x50, "q": 0x51, "r": 0x52, "s": 0x53, "t": 0x54,
	"u": 0x55, "v": 0x56, "w": 0x57, "x": 0x58, "y": 0x59,
	"z": 0x5A,
}

// InputSimulator provides methods to simulate mouse and keyboard input.
type InputSimulator struct{}

func NewInputSimulator() *InputSimulator { return &InputSimulator{} }

func (s *InputSimulator) GetCursorPos() (int, int) {
	var pt struct{ x, y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return int(pt.x), int(pt.y)
}

func (s *InputSimulator) MoveMouse(x, y int) error {
	ret, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if ret == 0 {
		return fmt.Errorf("SetCursorPos failed for (%d, %d)", x, y)
	}
	return nil
}

func (s *InputSimulator) MouseLeftDown()  { sendMouseInput(mouseEventFlagLeftDown) }
func (s *InputSimulator) MouseLeftUp()    { sendMouseInput(mouseEventFlagLeftUp) }
func (s *InputSimulator) MouseRightDown() { sendMouseInput(mouseEventFlagRightDown) }
func (s *InputSimulator) MouseRightUp()   { sendMouseInput(mouseEventFlagRightUp) }

func resolveVirtualKeyCode(keyName string) (uint16, error) {
	if vk, ok := virtualKeyCodeMap[keyName]; ok {
		return vk, nil
	}
	if len(keyName) == 1 {
		r := []rune(keyName)[0]
		ret, _, _ := procVkKeyScan.Call(uintptr(r))
		vk := uint16(ret & 0xFF)
		if vk == 0xFF {
			return 0, fmt.Errorf("unknown key: %s", keyName)
		}
		return vk, nil
	}
	return 0, fmt.Errorf("unknown key: %s", keyName)
}

func mapVirtualKeyToScanCode(vk uint16) uint16 {
	ret, _, _ := procMapVirtualKey.Call(uintptr(vk), 0)
	return uint16(ret)
}

func logForegroundWindow() {
	hwnd, _, _ := procGetForegroundWindow.Call()
	buf := make([]uint16, 256)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
	title := windows.UTF16ToString(buf)
	log.Printf("foreground window: hwnd=0x%X title=%q", hwnd, title)
}

func extendedFlags(vk uint16, base uint32) uint32 {
	if extendedVKCodes[vk] {
		return base | keyEventFlagExtendedKey
	}
	return base
}

func (s *InputSimulator) KeyDown(keyName string) error {
	vk, err := resolveVirtualKeyCode(keyName)
	if err != nil {
		return err
	}
	sc := mapVirtualKeyToScanCode(vk)
	logForegroundWindow()
	log.Printf("KeyDown vk=0x%02X sc=0x%02X name=%s", vk, sc, keyName)
	sendKeyInput(vk, sc, extendedFlags(vk, 0))
	return nil
}

func (s *InputSimulator) KeyUp(keyName string) error {
	vk, err := resolveVirtualKeyCode(keyName)
	if err != nil {
		return err
	}
	sc := mapVirtualKeyToScanCode(vk)
	sendKeyInput(vk, sc, extendedFlags(vk, keyEventFlagKeyUp))
	return nil
}
