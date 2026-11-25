//go:build windows

package hotkey

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	procGetAsyncKeyState = user32DLL.NewProc("GetAsyncKeyState")
	procGetCurrentThreadID = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId")
)

const (
	modifierAlt   = 0x0001
	modifierCtrl  = 0x0002
	modifierShift = 0x0004
	modifierWin   = 0x0008

	wmHotkey = 0x0312
	pmRemove = 0x0001
)

// bareModifierVKCodes are keys that RegisterHotKey cannot use as the hotkey key
// because Windows consumes them as modifiers before hotkey processing.
var bareModifierVKCodes = map[uint32]bool{
	0xA0: true, // VK_LSHIFT
	0xA1: true, // VK_RSHIFT
	0xA2: true, // VK_LCONTROL
	0xA3: true, // VK_RCONTROL
	0xA4: true, // VK_LMENU (LAlt)
	0xA5: true, // VK_RMENU (RAlt)
	0x5B: true, // VK_LWIN
	0x5C: true, // VK_RWIN
}

// HotkeyCallback is invoked when a registered hotkey is pressed.
type HotkeyCallback func(hotkeyID int)

type unregRequest struct {
	id    int
	reply chan error
}

type pollKeyEntry struct {
	id       int
	vkCode   uint32
	callback HotkeyCallback
	wasDown  bool
}

// Manager handles global hotkey registration and dispatching on Windows.
// All Win32 calls happen on a single locked OS thread.
type Manager struct {
	mu           sync.RWMutex
	callbacks    map[int]HotkeyCallback
	nextID       int
	registerCh   chan regRequest
	unregisterCh chan unregRequest

	pollMu   sync.Mutex
	pollKeys []pollKeyEntry

	listenerTID atomic.Uint32
}

// registerThreadHook, when set (tests only), receives the OS thread id RegisterHotKey ran on.
var registerThreadHook func(tid uint32)

func currentThreadID() uint32 {
	id, _, _ := procGetCurrentThreadID.Call()
	return uint32(id)
}

// ListenerThreadID is the OS thread that owns every registration and receives WM_HOTKEY (0
// until ListenForHotkeys has started).
func (m *Manager) ListenerThreadID() uint32 { return m.listenerTID.Load() }

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

// KeyNameToVirtualKeyCode converts a key name string to a Win32 virtual key code.
func KeyNameToVirtualKeyCode(keyName string) uint32 {
	return convertKeyNameToVirtualKeyCode(keyName)
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
		"lctrl": 0xA2, "rctrl": 0xA3,
		"lshift": 0xA0, "rshift": 0xA1,
		"lalt": 0xA4, "ralt": 0xA5,
		"lwin": 0x5B, "rwin": 0x5C,
		";": 0xBA, "=": 0xBB, ",": 0xBC, "-": 0xBD, ".": 0xBE, "/": 0xBF,
		"`": 0xC0, "[": 0xDB, `\`: 0xDC, "]": 0xDD, "'": 0xDE,
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
	return registerBounded(m.registerCh, hotkey, callback, RegisterTimeout, func(id int) { _ = m.Unregister(id) })
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
	m.listenerTID.Store(currentThreadID())

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
		// Process pending registrations/unregistrations
		for {
			select {
			case req := <-m.registerCh:
				id := m.nextID
				m.nextID++
				modFlags := convertModifiersToFlags(req.hotkey.Modifiers)
				vk := convertKeyNameToVirtualKeyCode(req.hotkey.Key)

				// Bare modifier keys can't use RegisterHotKey — use GetAsyncKeyState polling.
				if modFlags == 0 && bareModifierVKCodes[vk] {
					m.pollMu.Lock()
					m.pollKeys = append(m.pollKeys, pollKeyEntry{id: id, vkCode: vk, callback: req.callback})
					m.pollMu.Unlock()
					req.reply <- regReply{id, nil}
				} else {
					if registerThreadHook != nil {
						registerThreadHook(currentThreadID())
					}
					ret, _, err := procRegisterHotKey.Call(0, uintptr(id), uintptr(modFlags), uintptr(vk))
					if ret == 0 {
						req.reply <- regReply{0, fmt.Errorf("RegisterHotKey failed for %+v: %v", req.hotkey, err)}
					} else {
						m.mu.Lock()
						m.callbacks[id] = req.callback
						m.mu.Unlock()
						req.reply <- regReply{id, nil}
					}
				}

			case req := <-m.unregisterCh:
				// Check poll keys first
				m.pollMu.Lock()
				found := false
				for i, pk := range m.pollKeys {
					if pk.id == req.id {
						m.pollKeys = append(m.pollKeys[:i], m.pollKeys[i+1:]...)
						found = true
						break
					}
				}
				m.pollMu.Unlock()
				if found {
					req.reply <- nil
					continue
				}
				req.reply <- removeRegistration(&m.mu, m.callbacks, req.id, func(id int) error {
					ret, _, err := procUnregisterHotKey.Call(0, uintptr(id))
					if ret == 0 {
						return err
					}
					return nil
				})
			default:
				goto doneChannels
			}
		}
	doneChannels:

		// Check for WM_HOTKEY messages (non-blocking)
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
				log.Printf("hotkey fired: id=%d", pressedID)
				go cb(pressedID)
			}
		}

		// Poll bare modifier keys via GetAsyncKeyState, fire on key-up.
		m.pollMu.Lock()
		for i := range m.pollKeys {
			pk := &m.pollKeys[i]
			state, _, _ := procGetAsyncKeyState.Call(uintptr(pk.vkCode))
			isDown := uint16(state)&0x8000 != 0
			if pk.wasDown && !isDown {
				cb := pk.callback
				id := pk.id
				log.Printf("hotkey fired: id=%d", id)
				go cb(id)
			}
			pk.wasDown = isDown
		}
		m.pollMu.Unlock()

		time.Sleep(10 * time.Millisecond)
	}
}
