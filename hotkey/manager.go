//go:build windows

package hotkey

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/yanmad27/supcua-v2/model"
)

var (
	user32DLL            = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey   = user32DLL.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32DLL.NewProc("UnregisterHotKey")
	procGetMessage       = user32DLL.NewProc("GetMessageW")
)

const (
	modifierAlt   = 0x0001
	modifierCtrl  = 0x0002
	modifierShift = 0x0004
	modifierWin   = 0x0008

	wmHotkey = 0x0312
)

// HotkeyCallback is invoked when a registered hotkey is pressed.
type HotkeyCallback func(hotkeyID int)

// Registration tracks a single registered hotkey.
type Registration struct {
	HotkeyID int
	Hotkey   model.Hotkey
	Label    string
}

// Manager handles global hotkey registration and dispatching on Windows.
type Manager struct {
	registrationMutex sync.Mutex
	registrationMap   map[int]HotkeyCallback
	nextHotkeyID      int
	windowHandle      uintptr
}

// NewManager creates a hotkey Manager.
func NewManager() *Manager {
	return &Manager{
		registrationMap: make(map[int]HotkeyCallback),
		nextHotkeyID:    1,
	}
}

func convertModifiersToFlags(modifierNames []string) uint32 {
	var combinedFlags uint32
	for _, modifierName := range modifierNames {
		switch strings.ToLower(modifierName) {
		case "alt":
			combinedFlags |= modifierAlt
		case "ctrl":
			combinedFlags |= modifierCtrl
		case "shift":
			combinedFlags |= modifierShift
		case "win":
			combinedFlags |= modifierWin
		}
	}
	return combinedFlags
}

func convertKeyNameToVirtualKeyCode(keyName string) uint32 {
	keyNameLower := strings.ToLower(keyName)
	virtualKeyCodeMap := map[string]uint32{
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
		"space": 0x20, "enter": 0x0D, "escape": 0x1B, "tab": 0x09,
	}
	if virtualKeyCode, exists := virtualKeyCodeMap[keyNameLower]; exists {
		return virtualKeyCode
	}
	if len(keyNameLower) == 1 {
		return uint32(keyNameLower[0]) - 0x20
	}
	return 0
}

// Register registers a global hotkey and returns its ID. Returns error if already taken.
func (hotkeyManager *Manager) Register(hotkey model.Hotkey, callback HotkeyCallback) (int, error) {
	hotkeyManager.registrationMutex.Lock()
	defer hotkeyManager.registrationMutex.Unlock()

	assignedID := hotkeyManager.nextHotkeyID
	hotkeyManager.nextHotkeyID++

	modifierFlags := convertModifiersToFlags(hotkey.Modifiers)
	virtualKeyCode := convertKeyNameToVirtualKeyCode(hotkey.Key)

	returnValue, _, err := procRegisterHotKey.Call(0, uintptr(assignedID), uintptr(modifierFlags), uintptr(virtualKeyCode))
	if returnValue == 0 {
		return 0, fmt.Errorf("RegisterHotKey failed for %+v: %v", hotkey, err)
	}

	hotkeyManager.registrationMap[assignedID] = callback
	return assignedID, nil
}

// Unregister removes a registered hotkey by its ID.
func (hotkeyManager *Manager) Unregister(hotkeyID int) error {
	hotkeyManager.registrationMutex.Lock()
	defer hotkeyManager.registrationMutex.Unlock()

	returnValue, _, err := procUnregisterHotKey.Call(0, uintptr(hotkeyID))
	if returnValue == 0 {
		return fmt.Errorf("UnregisterHotKey failed for ID %d: %v", hotkeyID, err)
	}

	delete(hotkeyManager.registrationMap, hotkeyID)
	return nil
}

// ListenForHotkeys starts the Windows message loop to dispatch hotkey events.
// This blocks — run in a goroutine.
func (hotkeyManager *Manager) ListenForHotkeys() {
	type messageStruct struct {
		hwnd    uintptr
		message uint32
		wParam  uintptr
		lParam  uintptr
		time    uint32
		ptX     int32
		ptY     int32
	}

	var windowMessage messageStruct
	for {
		returnValue, _, _ := procGetMessage.Call(
			uintptr(unsafe.Pointer(&windowMessage)), 0, 0, 0,
		)
		if returnValue == 0 {
			break
		}
		if windowMessage.message == wmHotkey {
			pressedHotkeyID := int(windowMessage.wParam)
			hotkeyManager.registrationMutex.Lock()
			registeredCallback, exists := hotkeyManager.registrationMap[pressedHotkeyID]
			hotkeyManager.registrationMutex.Unlock()
			if exists {
				go registeredCallback(pressedHotkeyID)
			}
		}
	}
}
