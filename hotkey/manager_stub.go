//go:build !windows

package hotkey

import (
	"fmt"

	"github.com/yanmad27/supcua-v2/model"
)

type HotkeyCallback func(hotkeyID int)

type Registration struct {
	HotkeyID int
	Hotkey   model.Hotkey
	Label    string
}

type Manager struct{}

func NewManager() *Manager { return &Manager{} }

func (hotkeyManager *Manager) Register(hotkey model.Hotkey, callback HotkeyCallback) (int, error) {
	return 0, fmt.Errorf("hotkeys only supported on Windows")
}

func (hotkeyManager *Manager) Unregister(hotkeyID int) error {
	return fmt.Errorf("hotkeys only supported on Windows")
}

func (hotkeyManager *Manager) ListenForHotkeys() {}

func (hotkeyManager *Manager) SetSuspended(bool) {}
