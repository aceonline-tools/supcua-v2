//go:build windows

package executor

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32DLL         = syscall.NewLazyDLL("user32.dll")
	procSetCursorPos  = user32DLL.NewProc("SetCursorPos")
	procMouseEvent    = user32DLL.NewProc("mouse_event")
	procKeybdEvent    = user32DLL.NewProc("keybd_event")
	procMapVirtualKey = user32DLL.NewProc("MapVirtualKeyW")
	procVkKeyScan     = user32DLL.NewProc("VkKeyScanW")
)

// Mouse event flags
const (
	mouseEventFlagLeftDown  = 0x0002
	mouseEventFlagLeftUp    = 0x0004
	mouseEventFlagRightDown = 0x0008
	mouseEventFlagRightUp   = 0x0010
)

// Keyboard event flags
const (
	keyEventFlagExtendedKey = 0x0001
	keyEventFlagKeyUp       = 0x0002
)

// Virtual key codes for modifier and special keys
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

// InputSimulator provides methods to simulate mouse and keyboard input via Win32 API.
type InputSimulator struct{}

// NewInputSimulator creates an InputSimulator.
func NewInputSimulator() *InputSimulator {
	return &InputSimulator{}
}

// MoveMouse sets the cursor to absolute screen coordinates.
func (inputSimulator *InputSimulator) MoveMouse(positionX, positionY int) error {
	returnValue, _, _ := procSetCursorPos.Call(uintptr(positionX), uintptr(positionY))
	if returnValue == 0 {
		return fmt.Errorf("SetCursorPos failed for (%d, %d)", positionX, positionY)
	}
	return nil
}

// MouseLeftDown presses the left mouse button.
func (inputSimulator *InputSimulator) MouseLeftDown() {
	procMouseEvent.Call(mouseEventFlagLeftDown, 0, 0, 0)
}

// MouseLeftUp releases the left mouse button.
func (inputSimulator *InputSimulator) MouseLeftUp() {
	procMouseEvent.Call(mouseEventFlagLeftUp, 0, 0, 0)
}

// MouseRightDown presses the right mouse button.
func (inputSimulator *InputSimulator) MouseRightDown() {
	procMouseEvent.Call(mouseEventFlagRightDown, 0, 0, 0)
}

// MouseRightUp releases the right mouse button.
func (inputSimulator *InputSimulator) MouseRightUp() {
	procMouseEvent.Call(mouseEventFlagRightUp, 0, 0, 0)
}

// resolveVirtualKeyCode converts a key name string to a Win32 virtual key code.
func resolveVirtualKeyCode(keyName string) (uint16, error) {
	if virtualKeyCode, exists := virtualKeyCodeMap[keyName]; exists {
		return virtualKeyCode, nil
	}
	if len(keyName) == 1 {
		charRune := []rune(keyName)[0]
		returnValue, _, _ := procVkKeyScan.Call(uintptr(charRune))
		virtualKeyCode := uint16(returnValue & 0xFF)
		if virtualKeyCode == 0xFF {
			return 0, fmt.Errorf("unknown key: %s", keyName)
		}
		return virtualKeyCode, nil
	}
	return 0, fmt.Errorf("unknown key: %s", keyName)
}

// KeyDown presses a key (does not release it).
func (inputSimulator *InputSimulator) KeyDown(keyName string) error {
	virtualKeyCode, err := resolveVirtualKeyCode(keyName)
	if err != nil {
		return err
	}
	scanCode := mapVirtualKeyToScanCode(virtualKeyCode)
	procKeybdEvent.Call(uintptr(virtualKeyCode), uintptr(scanCode), 0, 0)
	return nil
}

// KeyUp releases a previously pressed key.
func (inputSimulator *InputSimulator) KeyUp(keyName string) error {
	virtualKeyCode, err := resolveVirtualKeyCode(keyName)
	if err != nil {
		return err
	}
	scanCode := mapVirtualKeyToScanCode(virtualKeyCode)
	procKeybdEvent.Call(uintptr(virtualKeyCode), uintptr(scanCode), keyEventFlagKeyUp, 0)
	return nil
}

func mapVirtualKeyToScanCode(virtualKeyCode uint16) uint16 {
	returnValue, _, _ := procMapVirtualKey.Call(uintptr(virtualKeyCode), 0)
	return uint16(returnValue)
}

// Ensure unsafe is used (needed for potential future use, satisfying import)
var _ = unsafe.Sizeof(0)
