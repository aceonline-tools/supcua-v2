//go:build windows

package hotkey

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/yanmad27/supcua-v2/model"
)

var (
	user32DLL            = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey   = user32DLL.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32DLL.NewProc("UnregisterHotKey")
	procPeekMessage      = user32DLL.NewProc("PeekMessageW")
)

const (
	modifierAlt   = 0x0001
	modifierCtrl  = 0x0002
	modifierShift = 0x0004
	modifierWin   = 0x0008

	wmHotkey = 0x0312
	pmRemove = 0x0001
)

// HotkeyCallback is invoked when a registered hotkey is pressed.
type HotkeyCallback func(hotkeyID int)

type regRequest struct {
	hotkey   model.Hotkey
	callback HotkeyCallback
	reply    chan regReply
}

type regReply struct {
	id  int
	err error
}

type unregRequest struct {
	id    int
	reply chan error
}

// Manager handles global hotkey registration and dispatching on Windows.
// All Win32 calls happen on a single locked OS thread.
type Manager struct {
	mu          sync.RWMutex
	callbacks   map[int]HotkeyCallback
	nextID      int
	registerCh  chan regRequest
	unregisterCh chan unregRequest
}

// NewManager creates a hotkey Manager.
func NewManager() *Manager {
	return &Manager{
		callbacks:    make(map[int]HotkeyCallback),
		nextID:       1,
		registerCh:   make(chan regRequest, 32),
		unregisterCh: make(chan unregRequest, 32),
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

// Register sends a registration request to the listener goroutine (same OS thread as RegisterHotKey).
func (m *Manager) Register(hotkey model.Hotkey, callback HotkeyCallback) (int, error) {
	reply := make(chan regReply, 1)
	m.registerCh <- regRequest{hotkey: hotkey, callback: callback, reply: reply}
	r := <-reply
	return r.id, r.err
}

// Unregister sends an unregistration request to the listener goroutine.
func (m *Manager) Unregister(hotkeyID int) error {
	reply := make(chan error, 1)
	m.unregisterCh <- unregRequest{id: hotkeyID, reply: reply}
	return <-reply
}

// UnregisterAll removes all registered hotkeys.
func (m *Manager) UnregisterAll() {
	m.mu.RLock()
	ids := make([]int, 0, len(m.callbacks))
	for id := range m.callbacks {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		m.Unregister(id)
	}
}

// ListenForHotkeys starts the Windows message loop on a locked OS thread.
// This blocks — run in a goroutine.
func (m *Manager) ListenForHotkeys() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	type msgStruct struct {
		hwnd    uintptr
		message uint32
		wParam  uintptr
		lParam  uintptr
		time    uint32
		ptX     int32
		ptY     int32
	}

	for {
		// Process pending registrations
		for {
			select {
			case req := <-m.registerCh:
				id := m.nextID
				m.nextID++
				modFlags := convertModifiersToFlags(req.hotkey.Modifiers)
				vk := convertKeyNameToVirtualKeyCode(req.hotkey.Key)
				ret, _, err := procRegisterHotKey.Call(0, uintptr(id), uintptr(modFlags), uintptr(vk))
				if ret == 0 {
					req.reply <- regReply{0, fmt.Errorf("RegisterHotKey failed for %+v: %v", req.hotkey, err)}
				} else {
					m.mu.Lock()
					m.callbacks[id] = req.callback
					m.mu.Unlock()
					req.reply <- regReply{id, nil}
				}
			case req := <-m.unregisterCh:
				ret, _, err := procUnregisterHotKey.Call(0, uintptr(req.id))
				if ret == 0 {
					req.reply <- fmt.Errorf("UnregisterHotKey failed for ID %d: %v", req.id, err)
				} else {
					m.mu.Lock()
					delete(m.callbacks, req.id)
					m.mu.Unlock()
					req.reply <- nil
				}
			default:
				goto doneChannels
			}
		}
	doneChannels:

		// Check for hotkey messages (non-blocking)
		var msg msgStruct
		ret, _, _ := procPeekMessage.Call(
			uintptr(unsafe.Pointer(&msg)), 0, 0, 0, pmRemove,
		)
		if ret != 0 && msg.message == wmHotkey {
			pressedID := int(msg.wParam)
			m.mu.RLock()
			cb, ok := m.callbacks[pressedID]
			m.mu.RUnlock()
			if ok {
				go cb(pressedID)
			}
		}

		time.Sleep(10 * time.Millisecond)
	}
}
