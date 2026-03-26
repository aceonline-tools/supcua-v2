# supcua-v2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Windows 10 mouse/keyboard automation tool in Go with Walk GUI, supporting CRUD scripts with line-by-line actions, global hotkeys, HUD overlay, and image detection.

**Architecture:** Walk-based native Win32 GUI app with system tray + overlay. Scripts stored as JSON files. Win32 API (user32.dll) for input simulation and global hotkeys. Single executor goroutine with context cancellation. Image detection via screen capture and pixel matching.

**Tech Stack:** Go, Walk (github.com/lxn/walk), Win32 API via syscall, google/uuid, encoding/json

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `main.go`

**Step 1: Initialize Go module**

Run: `go mod init github.com/yanmad27/supcua-v2`
Expected: `go.mod` created

**Step 2: Install dependencies**

Run:
```bash
go get github.com/lxn/walk
go get github.com/lxn/win
go get github.com/google/uuid
```
Expected: `go.sum` created, dependencies in `go.mod`

**Step 3: Create minimal main.go**

```go
package main

import (
	"log"
	"os"
)

func main() {
	log.SetOutput(os.Stdout)
	log.Println("supcua-v2 starting...")
}
```

**Step 4: Verify it compiles**

Run: `go build ./...`
Expected: No errors

**Step 5: Commit**

```bash
git add go.mod go.sum main.go
git commit -m "scaffold project with go module and dependencies"
```

---

### Task 2: Data Models

**Files:**
- Create: `model/action.go`
- Create: `model/script.go`
- Create: `model/config.go`
- Create: `model/hotkey.go`

**Step 1: Create model/hotkey.go**

```go
package model

// Hotkey represents a keyboard shortcut with optional modifiers.
// Modifiers can be empty for single-key hotkeys (e.g., F5).
type Hotkey struct {
	Modifiers []string `json:"modifiers"`
	Key       string   `json:"key"`
}
```

**Step 2: Create model/action.go**

```go
package model

// ActionType enumerates all supported script action types.
const (
	ActionMouseMove      = "mouse_move"
	ActionMouseLeftDown  = "mouse_left_down"
	ActionMouseLeftUp    = "mouse_left_up"
	ActionMouseRightDown = "mouse_right_down"
	ActionMouseRightUp   = "mouse_right_up"
	ActionKeyDown        = "key_down"
	ActionKeyUp          = "key_up"
	ActionDelay          = "delay"
	ActionLoop           = "loop"
	ActionWaitForImage   = "wait_for_image"
	ActionIfImage        = "if_image"
)

// Region defines a rectangular area on screen for image search.
type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Action represents a single step in a script.
// Fields are used depending on the Type:
//   - mouse_move: X, Y (or Source="found" with OffsetX, OffsetY)
//   - mouse_left_down, mouse_left_up, mouse_right_down, mouse_right_up: no params
//   - key_down, key_up: Key
//   - delay: DelayMs
//   - loop: LoopCount, Actions
//   - wait_for_image: Image, SearchRegion, TimeoutMs, UseFoundPosition
//   - if_image: Image, SearchRegion, UseFoundPosition, ThenActions, ElseActions
type Action struct {
	Type             string   `json:"type"`
	X                int      `json:"x,omitempty"`
	Y                int      `json:"y,omitempty"`
	Source           string   `json:"source,omitempty"`
	OffsetX          int      `json:"offset_x,omitempty"`
	OffsetY          int      `json:"offset_y,omitempty"`
	Key              string   `json:"key,omitempty"`
	DelayMs          int      `json:"ms,omitempty"`
	LoopCount        int      `json:"count,omitempty"`
	Actions          []Action `json:"actions,omitempty"`
	Image            string   `json:"image,omitempty"`
	SearchRegion     *Region  `json:"region,omitempty"`
	TimeoutMs        int      `json:"timeout_ms,omitempty"`
	UseFoundPosition bool     `json:"use_found_position,omitempty"`
	ThenActions      []Action `json:"then,omitempty"`
	ElseActions      []Action `json:"else,omitempty"`
}
```

**Step 3: Create model/script.go**

```go
package model

const (
	RunModeOnce     = "once"
	RunModeInfinite = "infinite"

	HotkeyModeToggle   = "toggle"
	HotkeyModeSeparate = "separate"
)

// Script represents a user-created automation script.
type Script struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Enabled      bool    `json:"enabled"`
	RunMode      string  `json:"run_mode"`
	HotkeyMode   string  `json:"hotkey_mode"`
	ToggleHotkey *Hotkey `json:"toggle_hotkey,omitempty"`
	StartHotkey  *Hotkey `json:"start_hotkey,omitempty"`
	StopHotkey   *Hotkey `json:"stop_hotkey,omitempty"`
	Actions      []Action `json:"actions"`
}
```

**Step 4: Create model/config.go**

```go
package model

// AppConfig holds global application settings.
type AppConfig struct {
	GlobalHotkey   Hotkey `json:"global_hotkey"`
	CaptureHotkey  Hotkey `json:"capture_hotkey"`
	OverlayEnabled bool   `json:"overlay_enabled"`
	OverlayPosition struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"overlay_position"`
	ScriptsDir string `json:"scripts_dir"`
}

// DefaultConfig returns sensible defaults for a fresh install.
func DefaultConfig() AppConfig {
	return AppConfig{
		GlobalHotkey:   Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "f1"},
		CaptureHotkey:  Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "f2"},
		OverlayEnabled: true,
		OverlayPosition: struct {
			X int `json:"x"`
			Y int `json:"y"`
		}{X: 10, Y: 10},
		ScriptsDir: "./scripts",
	}
}
```

**Step 5: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 6: Commit**

```bash
git add model/
git commit -m "add data models for script, action, hotkey, and config"
```

---

### Task 3: Config Storage

**Files:**
- Create: `storage/config_store.go`
- Create: `storage/config_store_test.go`

**Step 1: Write failing test for config load/save**

```go
package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestSaveAndLoadConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configStore := NewConfigStore(configPath)

	originalConfig := model.DefaultConfig()
	originalConfig.OverlayEnabled = false

	err := configStore.Save(originalConfig)
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loadedConfig, err := configStore.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loadedConfig.OverlayEnabled != false {
		t.Errorf("expected overlay_enabled=false, got %v", loadedConfig.OverlayEnabled)
	}
	if loadedConfig.GlobalHotkey.Key != "f1" {
		t.Errorf("expected global hotkey key=f1, got %s", loadedConfig.GlobalHotkey.Key)
	}
}

func TestLoadConfigFileNotExist(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "missing.json")
	configStore := NewConfigStore(configPath)

	config, err := configStore.Load()
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}

	defaultConfig := model.DefaultConfig()
	if config.GlobalHotkey.Key != defaultConfig.GlobalHotkey.Key {
		t.Errorf("expected default config, got: %+v", config)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./storage/ -v`
Expected: FAIL — `NewConfigStore` not defined

**Step 3: Implement config_store.go**

```go
package storage

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/yanmad27/supcua-v2/model"
)

// ConfigStore handles loading and saving the app config JSON file.
type ConfigStore struct {
	filePath string
}

// NewConfigStore creates a ConfigStore for the given file path.
func NewConfigStore(filePath string) *ConfigStore {
	return &ConfigStore{filePath: filePath}
}

// Load reads the config from disk. Returns default config if file doesn't exist.
func (configStore *ConfigStore) Load() (model.AppConfig, error) {
	data, err := os.ReadFile(configStore.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.DefaultConfig(), nil
		}
		return model.AppConfig{}, err
	}

	var appConfig model.AppConfig
	if err := json.Unmarshal(data, &appConfig); err != nil {
		return model.AppConfig{}, err
	}
	return appConfig, nil
}

// Save writes the config to disk as formatted JSON.
func (configStore *ConfigStore) Save(appConfig model.AppConfig) error {
	data, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configStore.filePath, data, 0644)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./storage/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add storage/config_store.go storage/config_store_test.go
git commit -m "add config store with load, save, and default fallback"
```

---

### Task 4: Script Storage (CRUD)

**Files:**
- Create: `storage/script_store.go`
- Create: `storage/script_store_test.go`

**Step 1: Write failing tests**

```go
package storage

import (
	"path/filepath"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestCreateAndGetScript(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	script := model.Script{
		ID:      "test-id-1",
		Name:    "Test Script",
		Enabled: true,
		RunMode: model.RunModeOnce,
		Actions: []model.Action{
			{Type: model.ActionDelay, DelayMs: 100},
		},
	}

	err := scriptStore.Save(script)
	if err != nil {
		t.Fatalf("failed to save script: %v", err)
	}

	loaded, err := scriptStore.Get("test-id-1")
	if err != nil {
		t.Fatalf("failed to get script: %v", err)
	}
	if loaded.Name != "Test Script" {
		t.Errorf("expected name 'Test Script', got '%s'", loaded.Name)
	}
	if len(loaded.Actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(loaded.Actions))
	}
}

func TestListScripts(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	for i, name := range []string{"Alpha", "Beta"} {
		script := model.Script{
			ID:   "id-" + string(rune('a'+i)),
			Name: name,
		}
		if err := scriptStore.Save(script); err != nil {
			t.Fatalf("failed to save script %s: %v", name, err)
		}
	}

	allScripts, err := scriptStore.List()
	if err != nil {
		t.Fatalf("failed to list scripts: %v", err)
	}
	if len(allScripts) != 2 {
		t.Errorf("expected 2 scripts, got %d", len(allScripts))
	}
}

func TestDeleteScript(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	script := model.Script{ID: "del-1", Name: "Delete Me"}
	if err := scriptStore.Save(script); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	err := scriptStore.Delete("del-1")
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	_, err = scriptStore.Get("del-1")
	if err == nil {
		t.Error("expected error getting deleted script")
	}
}

func TestGetScriptNotFound(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	_, err := scriptStore.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing script")
	}
}

func TestScriptFilePath(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	expected := filepath.Join(tempDir, "test-id.json")
	actual := scriptStore.scriptFilePath("test-id")
	if actual != expected {
		t.Errorf("expected path %s, got %s", expected, actual)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./storage/ -v -run TestCreateAndGetScript`
Expected: FAIL — `NewScriptStore` not defined

**Step 3: Implement script_store.go**

```go
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yanmad27/supcua-v2/model"
)

// ScriptStore handles CRUD operations for script JSON files in a directory.
type ScriptStore struct {
	scriptsDirectory string
}

// NewScriptStore creates a ScriptStore for the given directory.
func NewScriptStore(scriptsDirectory string) *ScriptStore {
	return &ScriptStore{scriptsDirectory: scriptsDirectory}
}

func (scriptStore *ScriptStore) scriptFilePath(scriptID string) string {
	return filepath.Join(scriptStore.scriptsDirectory, scriptID+".json")
}

// Save writes a script to disk as a JSON file named by its ID.
func (scriptStore *ScriptStore) Save(script model.Script) error {
	if err := os.MkdirAll(scriptStore.scriptsDirectory, 0755); err != nil {
		return fmt.Errorf("create scripts directory: %w", err)
	}

	data, err := json.MarshalIndent(script, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal script: %w", err)
	}

	return os.WriteFile(scriptStore.scriptFilePath(script.ID), data, 0644)
}

// Get reads a single script by ID.
func (scriptStore *ScriptStore) Get(scriptID string) (model.Script, error) {
	data, err := os.ReadFile(scriptStore.scriptFilePath(scriptID))
	if err != nil {
		return model.Script{}, fmt.Errorf("read script %s: %w", scriptID, err)
	}

	var script model.Script
	if err := json.Unmarshal(data, &script); err != nil {
		return model.Script{}, fmt.Errorf("unmarshal script %s: %w", scriptID, err)
	}
	return script, nil
}

// List reads all scripts from the directory.
func (scriptStore *ScriptStore) List() ([]model.Script, error) {
	entries, err := os.ReadDir(scriptStore.scriptsDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read scripts directory: %w", err)
	}

	var allScripts []model.Script
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		scriptID := strings.TrimSuffix(entry.Name(), ".json")
		script, err := scriptStore.Get(scriptID)
		if err != nil {
			continue
		}
		allScripts = append(allScripts, script)
	}
	return allScripts, nil
}

// Delete removes a script file by ID.
func (scriptStore *ScriptStore) Delete(scriptID string) error {
	filePath := scriptStore.scriptFilePath(scriptID)
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("delete script %s: %w", scriptID, err)
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./storage/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add storage/script_store.go storage/script_store_test.go
git commit -m "add script store with CRUD operations and tests"
```

---

### Task 5: Input Simulator (Win32)

**Files:**
- Create: `executor/input.go`

**Note:** This uses Win32 syscalls — only compiles/runs on Windows. No unit tests for hardware simulation; verified manually.

**Step 1: Create executor/input.go**

```go
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
```

**Step 2: Add build tag file for non-Windows compilation**

Create `executor/input_stub.go` so the project compiles on non-Windows for development:

```go
//go:build !windows

package executor

import "fmt"

type InputSimulator struct{}

func NewInputSimulator() *InputSimulator { return &InputSimulator{} }

func (inputSimulator *InputSimulator) MoveMouse(positionX, positionY int) error {
	return fmt.Errorf("input simulation only supported on Windows")
}
func (inputSimulator *InputSimulator) MouseLeftDown()            {}
func (inputSimulator *InputSimulator) MouseLeftUp()              {}
func (inputSimulator *InputSimulator) MouseRightDown()           {}
func (inputSimulator *InputSimulator) MouseRightUp()             {}
func (inputSimulator *InputSimulator) KeyDown(keyName string) error { return fmt.Errorf("not supported") }
func (inputSimulator *InputSimulator) KeyUp(keyName string) error   { return fmt.Errorf("not supported") }
```

Add build tag to `executor/input.go` header:
```go
//go:build windows
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors (uses stub on non-Windows)

**Step 4: Commit**

```bash
git add executor/input.go executor/input_stub.go
git commit -m "add input simulator with Win32 mouse and keyboard calls"
```

---

### Task 6: Script Executor

**Files:**
- Create: `executor/executor.go`
- Create: `executor/executor_test.go`

**Step 1: Write failing test for executor**

The executor test uses a mock input simulator to verify action dispatch without Win32 calls.

```go
package executor

import (
	"context"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

type mockInputSimulator struct {
	executedActions []string
}

func (mockSimulator *mockInputSimulator) MoveMouse(positionX, positionY int) error {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_move")
	return nil
}
func (mockSimulator *mockInputSimulator) MouseLeftDown() {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_left_down")
}
func (mockSimulator *mockInputSimulator) MouseLeftUp() {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_left_up")
}
func (mockSimulator *mockInputSimulator) MouseRightDown() {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_right_down")
}
func (mockSimulator *mockInputSimulator) MouseRightUp() {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_right_up")
}
func (mockSimulator *mockInputSimulator) KeyDown(keyName string) error {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "key_down:"+keyName)
	return nil
}
func (mockSimulator *mockInputSimulator) KeyUp(keyName string) error {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "key_up:"+keyName)
	return nil
}

func TestExecuteBasicActions(t *testing.T) {
	mockSimulator := &mockInputSimulator{}
	scriptExecutor := NewExecutor(mockSimulator, nil)

	script := model.Script{
		ID:      "test-1",
		RunMode: model.RunModeOnce,
		Actions: []model.Action{
			{Type: model.ActionMouseMove, X: 100, Y: 200},
			{Type: model.ActionMouseLeftDown},
			{Type: model.ActionMouseLeftUp},
			{Type: model.ActionKeyDown, Key: "a"},
			{Type: model.ActionKeyUp, Key: "a"},
		},
	}

	ctx := context.Background()
	err := scriptExecutor.Execute(ctx, script)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedActions := []string{"mouse_move", "mouse_left_down", "mouse_left_up", "key_down:a", "key_up:a"}
	if len(mockSimulator.executedActions) != len(expectedActions) {
		t.Fatalf("expected %d actions, got %d: %v", len(expectedActions), len(mockSimulator.executedActions), mockSimulator.executedActions)
	}
	for actionIndex, expectedAction := range expectedActions {
		if mockSimulator.executedActions[actionIndex] != expectedAction {
			t.Errorf("action %d: expected %s, got %s", actionIndex, expectedAction, mockSimulator.executedActions[actionIndex])
		}
	}
}

func TestExecuteLoop(t *testing.T) {
	mockSimulator := &mockInputSimulator{}
	scriptExecutor := NewExecutor(mockSimulator, nil)

	script := model.Script{
		ID:      "test-loop",
		RunMode: model.RunModeOnce,
		Actions: []model.Action{
			{
				Type:      model.ActionLoop,
				LoopCount: 3,
				Actions: []model.Action{
					{Type: model.ActionMouseLeftDown},
					{Type: model.ActionMouseLeftUp},
				},
			},
		},
	}

	ctx := context.Background()
	err := scriptExecutor.Execute(ctx, script)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 iterations * 2 actions = 6
	if len(mockSimulator.executedActions) != 6 {
		t.Errorf("expected 6 actions, got %d: %v", len(mockSimulator.executedActions), mockSimulator.executedActions)
	}
}

func TestExecuteCancelledContext(t *testing.T) {
	mockSimulator := &mockInputSimulator{}
	scriptExecutor := NewExecutor(mockSimulator, nil)

	script := model.Script{
		ID:      "test-cancel",
		RunMode: model.RunModeOnce,
		Actions: []model.Action{
			{Type: model.ActionDelay, DelayMs: 5000},
			{Type: model.ActionMouseLeftDown},
		},
	}

	ctx, cancelFunction := context.WithCancel(context.Background())
	cancelFunction() // cancel immediately

	err := scriptExecutor.Execute(ctx, script)
	if err == nil {
		t.Error("expected context cancelled error")
	}
}

func TestExecuteInfiniteMode(t *testing.T) {
	mockSimulator := &mockInputSimulator{}
	scriptExecutor := NewExecutor(mockSimulator, nil)

	script := model.Script{
		ID:      "test-infinite",
		RunMode: model.RunModeInfinite,
		Actions: []model.Action{
			{Type: model.ActionMouseLeftDown},
		},
	}

	ctx, cancelFunction := context.WithCancel(context.Background())

	executionDone := make(chan error, 1)
	go func() {
		executionDone <- scriptExecutor.Execute(ctx, script)
	}()

	// Let it run a few iterations then cancel
	for len(mockSimulator.executedActions) < 5 {
		// spin until enough actions
	}
	cancelFunction()

	err := <-executionDone
	if err == nil {
		t.Error("expected context cancelled error")
	}
	if len(mockSimulator.executedActions) < 5 {
		t.Errorf("expected at least 5 actions, got %d", len(mockSimulator.executedActions))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./executor/ -v -run TestExecuteBasicActions`
Expected: FAIL — `NewExecutor` not defined

**Step 3: Implement executor.go**

```go
package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// InputSimulatorInterface abstracts input simulation for testing.
type InputSimulatorInterface interface {
	MoveMouse(positionX, positionY int) error
	MouseLeftDown()
	MouseLeftUp()
	MouseRightDown()
	MouseRightUp()
	KeyDown(keyName string) error
	KeyUp(keyName string) error
}

// ImageDetectorInterface abstracts image detection for testing.
type ImageDetectorInterface interface {
	FindImage(imageBase64 string, searchRegion *model.Region) (foundX int, foundY int, found bool, err error)
}

// StatusCallback is called when the executor status changes.
type StatusCallback func(scriptID string, status string, errorMessage string)

// Executor runs script actions line by line.
type Executor struct {
	inputSimulator InputSimulatorInterface
	imageDetector  ImageDetectorInterface
	foundPositionX int
	foundPositionY int
}

// NewExecutor creates an Executor with the given input simulator and image detector.
func NewExecutor(inputSimulator InputSimulatorInterface, imageDetector ImageDetectorInterface) *Executor {
	return &Executor{
		inputSimulator: inputSimulator,
		imageDetector:  imageDetector,
	}
}

// Execute runs a script's actions. Blocks until complete, cancelled, or error.
func (scriptExecutor *Executor) Execute(ctx context.Context, script model.Script) error {
	for {
		if err := scriptExecutor.executeActions(ctx, script.Actions); err != nil {
			return err
		}
		if script.RunMode != model.RunModeInfinite {
			return nil
		}
	}
}

func (scriptExecutor *Executor) executeActions(ctx context.Context, actionList []model.Action) error {
	for _, currentAction := range actionList {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := scriptExecutor.executeOneAction(ctx, currentAction); err != nil {
			return err
		}
	}
	return nil
}

func (scriptExecutor *Executor) executeOneAction(ctx context.Context, currentAction model.Action) error {
	switch currentAction.Type {
	case model.ActionMouseMove:
		return scriptExecutor.executeMouseMove(currentAction)

	case model.ActionMouseLeftDown:
		scriptExecutor.inputSimulator.MouseLeftDown()
		return nil

	case model.ActionMouseLeftUp:
		scriptExecutor.inputSimulator.MouseLeftUp()
		return nil

	case model.ActionMouseRightDown:
		scriptExecutor.inputSimulator.MouseRightDown()
		return nil

	case model.ActionMouseRightUp:
		scriptExecutor.inputSimulator.MouseRightUp()
		return nil

	case model.ActionKeyDown:
		return scriptExecutor.inputSimulator.KeyDown(currentAction.Key)

	case model.ActionKeyUp:
		return scriptExecutor.inputSimulator.KeyUp(currentAction.Key)

	case model.ActionDelay:
		return scriptExecutor.executeDelay(ctx, currentAction)

	case model.ActionLoop:
		return scriptExecutor.executeLoop(ctx, currentAction)

	case model.ActionWaitForImage:
		return scriptExecutor.executeWaitForImage(ctx, currentAction)

	case model.ActionIfImage:
		return scriptExecutor.executeIfImage(ctx, currentAction)

	default:
		return fmt.Errorf("unknown action type: %s", currentAction.Type)
	}
}

func (scriptExecutor *Executor) executeMouseMove(currentAction model.Action) error {
	if currentAction.Source == "found" {
		targetX := scriptExecutor.foundPositionX + currentAction.OffsetX
		targetY := scriptExecutor.foundPositionY + currentAction.OffsetY
		return scriptExecutor.inputSimulator.MoveMouse(targetX, targetY)
	}
	return scriptExecutor.inputSimulator.MoveMouse(currentAction.X, currentAction.Y)
}

func (scriptExecutor *Executor) executeDelay(ctx context.Context, currentAction model.Action) error {
	delayTimer := time.NewTimer(time.Duration(currentAction.DelayMs) * time.Millisecond)
	defer delayTimer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-delayTimer.C:
		return nil
	}
}

func (scriptExecutor *Executor) executeLoop(ctx context.Context, currentAction model.Action) error {
	if currentAction.LoopCount == 0 {
		// Infinite loop — runs until context cancelled
		for {
			if err := scriptExecutor.executeActions(ctx, currentAction.Actions); err != nil {
				return err
			}
		}
	}
	for iteration := 0; iteration < currentAction.LoopCount; iteration++ {
		if err := scriptExecutor.executeActions(ctx, currentAction.Actions); err != nil {
			return err
		}
	}
	return nil
}

func (scriptExecutor *Executor) executeWaitForImage(ctx context.Context, currentAction model.Action) error {
	if scriptExecutor.imageDetector == nil {
		return fmt.Errorf("image detector not available")
	}

	pollInterval := 200 * time.Millisecond
	timeoutDuration := time.Duration(currentAction.TimeoutMs) * time.Millisecond
	deadlineTimer := time.NewTimer(timeoutDuration)
	defer deadlineTimer.Stop()

	for {
		foundX, foundY, imageFound, err := scriptExecutor.imageDetector.FindImage(currentAction.Image, currentAction.SearchRegion)
		if err != nil {
			return fmt.Errorf("image detection error: %w", err)
		}
		if imageFound {
			if currentAction.UseFoundPosition {
				scriptExecutor.foundPositionX = foundX
				scriptExecutor.foundPositionY = foundY
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadlineTimer.C:
			return fmt.Errorf("wait_for_image timed out after %dms", currentAction.TimeoutMs)
		case <-time.After(pollInterval):
			// continue polling
		}
	}
}

func (scriptExecutor *Executor) executeIfImage(ctx context.Context, currentAction model.Action) error {
	if scriptExecutor.imageDetector == nil {
		return fmt.Errorf("image detector not available")
	}

	foundX, foundY, imageFound, err := scriptExecutor.imageDetector.FindImage(currentAction.Image, currentAction.SearchRegion)
	if err != nil {
		return fmt.Errorf("image detection error: %w", err)
	}

	if imageFound {
		if currentAction.UseFoundPosition {
			scriptExecutor.foundPositionX = foundX
			scriptExecutor.foundPositionY = foundY
		}
		return scriptExecutor.executeActions(ctx, currentAction.ThenActions)
	}

	if len(currentAction.ElseActions) > 0 {
		return scriptExecutor.executeActions(ctx, currentAction.ElseActions)
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./executor/ -v -timeout 10s`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add executor/executor.go executor/executor_test.go
git commit -m "add script executor with action dispatch, loops, and cancellation"
```

---

### Task 7: Hotkey Manager

**Files:**
- Create: `hotkey/manager.go`
- Create: `hotkey/manager_stub.go`

**Note:** Win32 RegisterHotKey requires a message loop — only works on Windows. Stub for non-Windows.

**Step 1: Create hotkey/manager.go**

```go
//go:build windows

package hotkey

import (
	"fmt"
	"strings"
	"sync"
	"syscall"

	"github.com/yanmad27/supcua-v2/model"
)

var (
	user32DLL          = syscall.NewLazyDLL("user32.dll")
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
```

**Step 2: Create hotkey/manager_stub.go**

```go
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
```

**Step 3: Add missing import to manager.go**

The `unsafe` import is needed for the message struct pointer in `ListenForHotkeys`. Ensure the imports block in `hotkey/manager.go` includes:
```go
import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/yanmad27/supcua-v2/model"
)
```

**Step 4: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 5: Commit**

```bash
git add hotkey/
git commit -m "add hotkey manager with Win32 RegisterHotKey and message loop"
```

---

### Task 8: Hotkey Conflict Detection

**Files:**
- Create: `hotkey/conflict.go`
- Create: `hotkey/conflict_test.go`

**Step 1: Write failing test**

```go
package hotkey

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestFindConflictingScript(t *testing.T) {
	conflictChecker := NewConflictChecker()

	scriptAlpha := model.Script{
		ID:           "script-alpha",
		Name:         "Alpha",
		HotkeyMode:   model.HotkeyModeToggle,
		ToggleHotkey: &model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "1"},
	}
	scriptBeta := model.Script{
		ID:           "script-beta",
		Name:         "Beta",
		HotkeyMode:   model.HotkeyModeSeparate,
		StartHotkey:  &model.Hotkey{Modifiers: []string{"ctrl"}, Key: "f5"},
		StopHotkey:   &model.Hotkey{Modifiers: []string{"ctrl"}, Key: "f6"},
	}

	conflictingScript, conflictingField := conflictChecker.FindConflict(
		model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "1"},
		[]model.Script{scriptAlpha, scriptBeta},
		"script-beta", // exclude self
	)

	if conflictingScript == nil {
		t.Fatal("expected to find a conflict with script-alpha")
	}
	if conflictingScript.ID != "script-alpha" {
		t.Errorf("expected conflict with script-alpha, got %s", conflictingScript.ID)
	}
	if conflictingField != "toggle_hotkey" {
		t.Errorf("expected field toggle_hotkey, got %s", conflictingField)
	}
}

func TestNoConflict(t *testing.T) {
	conflictChecker := NewConflictChecker()

	scriptAlpha := model.Script{
		ID:           "script-alpha",
		Name:         "Alpha",
		HotkeyMode:   model.HotkeyModeToggle,
		ToggleHotkey: &model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "1"},
	}

	conflictingScript, _ := conflictChecker.FindConflict(
		model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "2"},
		[]model.Script{scriptAlpha},
		"script-beta",
	)

	if conflictingScript != nil {
		t.Errorf("expected no conflict, got conflict with %s", conflictingScript.ID)
	}
}

func TestHotkeysEqual(t *testing.T) {
	conflictChecker := NewConflictChecker()

	testCases := []struct {
		name     string
		hotkeyA  model.Hotkey
		hotkeyB  model.Hotkey
		expected bool
	}{
		{"same combo", model.Hotkey{Modifiers: []string{"ctrl", "shift"}, Key: "1"}, model.Hotkey{Modifiers: []string{"shift", "ctrl"}, Key: "1"}, true},
		{"different key", model.Hotkey{Modifiers: []string{"ctrl"}, Key: "1"}, model.Hotkey{Modifiers: []string{"ctrl"}, Key: "2"}, false},
		{"different mods", model.Hotkey{Modifiers: []string{"ctrl"}, Key: "1"}, model.Hotkey{Modifiers: []string{"shift"}, Key: "1"}, false},
		{"single keys equal", model.Hotkey{Key: "f5"}, model.Hotkey{Key: "f5"}, true},
		{"single keys diff", model.Hotkey{Key: "f5"}, model.Hotkey{Key: "f6"}, false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := conflictChecker.HotkeysEqual(testCase.hotkeyA, testCase.hotkeyB)
			if result != testCase.expected {
				t.Errorf("expected %v, got %v", testCase.expected, result)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./hotkey/ -v -run TestFindConflictingScript`
Expected: FAIL — `NewConflictChecker` not defined

**Step 3: Implement conflict.go**

```go
package hotkey

import (
	"sort"
	"strings"

	"github.com/yanmad27/supcua-v2/model"
)

// ConflictChecker detects hotkey conflicts across scripts.
type ConflictChecker struct{}

// NewConflictChecker creates a ConflictChecker.
func NewConflictChecker() *ConflictChecker {
	return &ConflictChecker{}
}

// HotkeysEqual returns true if two hotkeys represent the same key combination.
// Modifier order does not matter.
func (conflictChecker *ConflictChecker) HotkeysEqual(hotkeyA, hotkeyB model.Hotkey) bool {
	if strings.ToLower(hotkeyA.Key) != strings.ToLower(hotkeyB.Key) {
		return false
	}

	normalizedModifiersA := normalizeModifiers(hotkeyA.Modifiers)
	normalizedModifiersB := normalizeModifiers(hotkeyB.Modifiers)

	if len(normalizedModifiersA) != len(normalizedModifiersB) {
		return false
	}
	for modifierIndex := range normalizedModifiersA {
		if normalizedModifiersA[modifierIndex] != normalizedModifiersB[modifierIndex] {
			return false
		}
	}
	return true
}

func normalizeModifiers(modifierList []string) []string {
	normalized := make([]string, len(modifierList))
	for index, modifierName := range modifierList {
		normalized[index] = strings.ToLower(modifierName)
	}
	sort.Strings(normalized)
	return normalized
}

// FindConflict checks if a hotkey conflicts with any script in the list.
// excludeScriptID is the script being edited (don't conflict with self).
// Returns the conflicting script and which field conflicts, or nil if no conflict.
func (conflictChecker *ConflictChecker) FindConflict(
	candidateHotkey model.Hotkey,
	allScripts []model.Script,
	excludeScriptID string,
) (*model.Script, string) {
	for scriptIndex := range allScripts {
		currentScript := &allScripts[scriptIndex]
		if currentScript.ID == excludeScriptID {
			continue
		}

		if currentScript.ToggleHotkey != nil && conflictChecker.HotkeysEqual(candidateHotkey, *currentScript.ToggleHotkey) {
			return currentScript, "toggle_hotkey"
		}
		if currentScript.StartHotkey != nil && conflictChecker.HotkeysEqual(candidateHotkey, *currentScript.StartHotkey) {
			return currentScript, "start_hotkey"
		}
		if currentScript.StopHotkey != nil && conflictChecker.HotkeysEqual(candidateHotkey, *currentScript.StopHotkey) {
			return currentScript, "stop_hotkey"
		}
	}
	return nil, ""
}

// ClearConflictingHotkey sets the conflicting hotkey field to nil on the given script.
func (conflictChecker *ConflictChecker) ClearConflictingHotkey(script *model.Script, fieldName string) {
	switch fieldName {
	case "toggle_hotkey":
		script.ToggleHotkey = nil
	case "start_hotkey":
		script.StartHotkey = nil
	case "stop_hotkey":
		script.StopHotkey = nil
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./hotkey/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add hotkey/conflict.go hotkey/conflict_test.go
git commit -m "add hotkey conflict detection with override support"
```

---

### Task 9: Image Detector

**Files:**
- Create: `image/detector.go`
- Create: `image/detector_stub.go`

**Note:** Screen capture uses Win32 GDI — Windows only. Pixel matching logic is testable cross-platform but we keep it simple for now.

**Step 1: Create image/detector.go**

```go
//go:build windows

package image

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"syscall"
	"unsafe"

	"github.com/yanmad27/supcua-v2/model"
)

var (
	user32DLL           = syscall.NewLazyDLL("user32.dll")
	gdi32DLL            = syscall.NewLazyDLL("gdi32.dll")
	procGetDC           = user32DLL.NewProc("GetDC")
	procReleaseDC       = user32DLL.NewProc("ReleaseDC")
	procGetSystemMetrics = user32DLL.NewProc("GetSystemMetrics")
	procCreateCompatibleDC    = gdi32DLL.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32DLL.NewProc("CreateCompatibleBitmap")
	procSelectObject    = gdi32DLL.NewProc("SelectObject")
	procBitBlt          = gdi32DLL.NewProc("BitBlt")
	procDeleteObject    = gdi32DLL.NewProc("DeleteObject")
	procDeleteDC        = gdi32DLL.NewProc("DeleteDC")
	procGetDIBits       = gdi32DLL.NewProc("GetDIBits")
)

const (
	systemMetricScreenWidth  = 0
	systemMetricScreenHeight = 1
	bitBltSourceCopy         = 0x00CC0020
)

// Detector captures screen regions and matches template images via pixel comparison.
type Detector struct {
	matchTolerance int // per-channel tolerance (0-255)
}

// NewDetector creates an image Detector with the given per-channel tolerance.
func NewDetector(matchTolerance int) *Detector {
	return &Detector{matchTolerance: matchTolerance}
}

// FindImage searches the screen (or a region) for the given base64-encoded PNG.
// Returns the center coordinates of the match and whether it was found.
func (imageDetector *Detector) FindImage(imageBase64 string, searchRegion *model.Region) (int, int, bool, error) {
	templateImage, err := decodeBase64PNG(imageBase64)
	if err != nil {
		return 0, 0, false, fmt.Errorf("decode template image: %w", err)
	}

	screenCapture, captureOriginX, captureOriginY, err := imageDetector.captureScreen(searchRegion)
	if err != nil {
		return 0, 0, false, fmt.Errorf("capture screen: %w", err)
	}

	templateBounds := templateImage.Bounds()
	captureBounds := screenCapture.Bounds()

	templateWidth := templateBounds.Dx()
	templateHeight := templateBounds.Dy()

	for scanY := 0; scanY <= captureBounds.Dy()-templateHeight; scanY++ {
		for scanX := 0; scanX <= captureBounds.Dx()-templateWidth; scanX++ {
			if imageDetector.pixelsMatch(screenCapture, templateImage, scanX, scanY) {
				centerX := captureOriginX + scanX + templateWidth/2
				centerY := captureOriginY + scanY + templateHeight/2
				return centerX, centerY, true, nil
			}
		}
	}

	return 0, 0, false, nil
}

func (imageDetector *Detector) pixelsMatch(screenCapture, templateImage image.Image, startX, startY int) bool {
	templateBounds := templateImage.Bounds()
	for templateY := templateBounds.Min.Y; templateY < templateBounds.Max.Y; templateY++ {
		for templateX := templateBounds.Min.X; templateX < templateBounds.Max.X; templateX++ {
			screenRed, screenGreen, screenBlue, _ := screenCapture.At(startX+templateX, startY+templateY).RGBA()
			templateRed, templateGreen, templateBlue, _ := templateImage.At(templateX, templateY).RGBA()

			// RGBA returns 16-bit values, convert to 8-bit
			if absDifference(int(screenRed>>8), int(templateRed>>8)) > imageDetector.matchTolerance ||
				absDifference(int(screenGreen>>8), int(templateGreen>>8)) > imageDetector.matchTolerance ||
				absDifference(int(screenBlue>>8), int(templateBlue>>8)) > imageDetector.matchTolerance {
				return false
			}
		}
	}
	return true
}

func absDifference(valueA, valueB int) int {
	if valueA > valueB {
		return valueA - valueB
	}
	return valueB - valueA
}

func decodeBase64PNG(encodedString string) (image.Image, error) {
	decodedBytes, err := base64.StdEncoding.DecodeString(encodedString)
	if err != nil {
		return nil, err
	}
	decodedImage, err := png.Decode(bytes.NewReader(decodedBytes))
	if err != nil {
		return nil, err
	}
	return decodedImage, nil
}

// captureScreen captures the full screen or a region using Win32 GDI.
func (imageDetector *Detector) captureScreen(searchRegion *model.Region) (image.Image, int, int, error) {
	screenDC, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screenDC)

	var captureX, captureY, captureWidth, captureHeight int
	if searchRegion != nil {
		captureX = searchRegion.X
		captureY = searchRegion.Y
		captureWidth = searchRegion.Width
		captureHeight = searchRegion.Height
	} else {
		screenWidthValue, _, _ := procGetSystemMetrics.Call(systemMetricScreenWidth)
		screenHeightValue, _, _ := procGetSystemMetrics.Call(systemMetricScreenHeight)
		captureWidth = int(screenWidthValue)
		captureHeight = int(screenHeightValue)
	}

	memoryDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	defer procDeleteDC.Call(memoryDC)

	bitmapHandle, _, _ := procCreateCompatibleBitmap.Call(screenDC, uintptr(captureWidth), uintptr(captureHeight))
	defer procDeleteObject.Call(bitmapHandle)

	procSelectObject.Call(memoryDC, bitmapHandle)
	procBitBlt.Call(memoryDC, 0, 0, uintptr(captureWidth), uintptr(captureHeight),
		screenDC, uintptr(captureX), uintptr(captureY), bitBltSourceCopy)

	// Read bitmap data
	type bitmapInfoHeader struct {
		BiSize          uint32
		BiWidth         int32
		BiHeight        int32
		BiPlanes        uint16
		BiBitCount      uint16
		BiCompression   uint32
		BiSizeImage     uint32
		BiXPelsPerMeter int32
		BiYPelsPerMeter int32
		BiClrUsed       uint32
		BiClrImportant  uint32
	}

	headerInfo := bitmapInfoHeader{
		BiSize:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:    int32(captureWidth),
		BiHeight:   -int32(captureHeight), // top-down
		BiPlanes:   1,
		BiBitCount: 32,
	}

	pixelDataSize := captureWidth * captureHeight * 4
	pixelBuffer := make([]byte, pixelDataSize)

	procGetDIBits.Call(memoryDC, bitmapHandle, 0, uintptr(captureHeight),
		uintptr(unsafe.Pointer(&pixelBuffer[0])),
		uintptr(unsafe.Pointer(&headerInfo)), 0)

	capturedImage := image.NewRGBA(image.Rect(0, 0, captureWidth, captureHeight))
	for pixelIndex := 0; pixelIndex < captureWidth*captureHeight; pixelIndex++ {
		byteOffset := pixelIndex * 4
		// GDI returns BGRA, convert to RGBA
		capturedImage.Pix[byteOffset+0] = pixelBuffer[byteOffset+2] // R
		capturedImage.Pix[byteOffset+1] = pixelBuffer[byteOffset+1] // G
		capturedImage.Pix[byteOffset+2] = pixelBuffer[byteOffset+0] // B
		capturedImage.Pix[byteOffset+3] = 255                       // A
	}

	return capturedImage, captureX, captureY, nil
}
```

**Step 2: Create image/detector_stub.go**

```go
//go:build !windows

package image

import (
	"fmt"

	"github.com/yanmad27/supcua-v2/model"
)

type Detector struct {
	matchTolerance int
}

func NewDetector(matchTolerance int) *Detector {
	return &Detector{matchTolerance: matchTolerance}
}

func (imageDetector *Detector) FindImage(imageBase64 string, searchRegion *model.Region) (int, int, bool, error) {
	return 0, 0, false, fmt.Errorf("image detection only supported on Windows")
}
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 4: Commit**

```bash
git add image/
git commit -m "add image detector with screen capture and pixel matching"
```

---

### Task 10: Mouse Capture

**Files:**
- Create: `capture/capture.go`
- Create: `capture/capture_stub.go`

**Step 1: Create capture/capture.go**

```go
//go:build windows

package capture

import (
	"syscall"
	"unsafe"
)

var (
	user32DLL        = syscall.NewLazyDLL("user32.dll")
	procGetCursorPos = user32DLL.NewProc("GetCursorPos")
)

type cursorPoint struct {
	X int32
	Y int32
}

// PositionCallback is called with the captured mouse coordinates.
type PositionCallback func(positionX, positionY int)

// GetCurrentMousePosition returns the current cursor position.
func GetCurrentMousePosition() (int, int, error) {
	var currentPoint cursorPoint
	returnValue, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&currentPoint)))
	if returnValue == 0 {
		return 0, 0, err
	}
	return int(currentPoint.X), int(currentPoint.Y), nil
}
```

**Step 2: Create capture/capture_stub.go**

```go
//go:build !windows

package capture

import "fmt"

type PositionCallback func(positionX, positionY int)

func GetCurrentMousePosition() (int, int, error) {
	return 0, 0, fmt.Errorf("mouse capture only supported on Windows")
}
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 4: Commit**

```bash
git add capture/
git commit -m "add mouse position capture via Win32 GetCursorPos"
```

---

### Task 11: Application Controller

**Files:**
- Create: `app/controller.go`
- Create: `app/controller_test.go`

This wires together the executor, hotkey manager, and storage — the "brain" of the app.

**Step 1: Write failing test**

```go
package app

import (
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestGlobalStatusToggle(t *testing.T) {
	appController := NewController(nil, nil, nil, nil)

	if appController.IsGlobalActive() {
		t.Error("expected inactive by default")
	}

	appController.ToggleGlobalStatus()

	if !appController.IsGlobalActive() {
		t.Error("expected active after toggle")
	}

	appController.ToggleGlobalStatus()

	if appController.IsGlobalActive() {
		t.Error("expected inactive after second toggle")
	}
}

func TestCanStartScript(t *testing.T) {
	appController := NewController(nil, nil, nil, nil)
	appController.ToggleGlobalStatus() // set active

	enabledScript := model.Script{ID: "s1", Enabled: true}
	disabledScript := model.Script{ID: "s2", Enabled: false}

	if !appController.CanStartScript(enabledScript) {
		t.Error("expected enabled script to be startable when global active")
	}

	if appController.CanStartScript(disabledScript) {
		t.Error("expected disabled script to NOT be startable")
	}

	appController.ToggleGlobalStatus() // set inactive

	if appController.CanStartScript(enabledScript) {
		t.Error("expected script to NOT be startable when global inactive")
	}
}

func TestOnlyOneScriptRunsAtATime(t *testing.T) {
	appController := NewController(nil, nil, nil, nil)
	appController.ToggleGlobalStatus()

	appController.SetRunningScriptID("script-1")

	scriptTwo := model.Script{ID: "script-2", Enabled: true}
	if appController.CanStartScript(scriptTwo) {
		t.Error("expected script-2 to NOT be startable while script-1 is running")
	}

	appController.ClearRunningScript()

	if !appController.CanStartScript(scriptTwo) {
		t.Error("expected script-2 to be startable after clearing")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./app/ -v`
Expected: FAIL — `NewController` not defined

**Step 3: Implement controller.go**

```go
package app

import (
	"context"
	"sync"

	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
)

// StatusChangeCallback is called when global or script status changes.
type StatusChangeCallback func()

// Controller coordinates the app's global state, executor, and hotkey manager.
type Controller struct {
	stateMutex          sync.RWMutex
	globalActive        bool
	runningScriptID     string
	cancelRunningScript context.CancelFunc

	scriptExecutor     *executor.Executor
	hotkeyManager      *hotkey.Manager
	scriptStore        *storage.ScriptStore
	configStore        *storage.ConfigStore

	OnStatusChange StatusChangeCallback
}

// NewController creates a Controller with all dependencies.
func NewController(
	scriptExecutor *executor.Executor,
	hotkeyManager *hotkey.Manager,
	scriptStore *storage.ScriptStore,
	configStore *storage.ConfigStore,
) *Controller {
	return &Controller{
		scriptExecutor: scriptExecutor,
		hotkeyManager:  hotkeyManager,
		scriptStore:    scriptStore,
		configStore:    configStore,
	}
}

// IsGlobalActive returns the current global active status.
func (appController *Controller) IsGlobalActive() bool {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.globalActive
}

// ToggleGlobalStatus flips the global active state.
// When going inactive, cancels any running script.
func (appController *Controller) ToggleGlobalStatus() {
	appController.stateMutex.Lock()
	appController.globalActive = !appController.globalActive
	nowActive := appController.globalActive
	appController.stateMutex.Unlock()

	if !nowActive {
		appController.StopRunningScript()
	}

	if appController.OnStatusChange != nil {
		appController.OnStatusChange()
	}
}

// CanStartScript returns true if the script is allowed to run right now.
func (appController *Controller) CanStartScript(script model.Script) bool {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.globalActive && script.Enabled && appController.runningScriptID == ""
}

// GetRunningScriptID returns the ID of the currently running script, or empty.
func (appController *Controller) GetRunningScriptID() string {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.runningScriptID
}

// SetRunningScriptID marks a script as currently running.
func (appController *Controller) SetRunningScriptID(scriptID string) {
	appController.stateMutex.Lock()
	defer appController.stateMutex.Unlock()
	appController.runningScriptID = scriptID
}

// ClearRunningScript marks no script as running.
func (appController *Controller) ClearRunningScript() {
	appController.stateMutex.Lock()
	defer appController.stateMutex.Unlock()
	appController.runningScriptID = ""
	appController.cancelRunningScript = nil
}

// StartScript launches the executor for a script in a goroutine.
func (appController *Controller) StartScript(script model.Script) {
	if !appController.CanStartScript(script) {
		return
	}

	executionContext, cancelFunction := context.WithCancel(context.Background())

	appController.stateMutex.Lock()
	appController.runningScriptID = script.ID
	appController.cancelRunningScript = cancelFunction
	appController.stateMutex.Unlock()

	if appController.OnStatusChange != nil {
		appController.OnStatusChange()
	}

	go func() {
		defer func() {
			appController.ClearRunningScript()
			if appController.OnStatusChange != nil {
				appController.OnStatusChange()
			}
		}()
		appController.scriptExecutor.Execute(executionContext, script)
	}()
}

// StopRunningScript cancels the currently running script.
func (appController *Controller) StopRunningScript() {
	appController.stateMutex.Lock()
	if appController.cancelRunningScript != nil {
		appController.cancelRunningScript()
	}
	appController.stateMutex.Unlock()
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./app/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add app/
git commit -m "add application controller with global state and script lifecycle"
```

---

### Task 12: System Tray

**Files:**
- Create: `ui/tray.go`
- Create: `ui/tray_stub.go`

**Note:** Walk system tray — Windows only.

**Step 1: Create ui/tray.go**

```go
//go:build windows

package ui

import (
	"github.com/lxn/walk"

	"github.com/yanmad27/supcua-v2/app"
)

// TrayIcon wraps the Walk system tray icon with context menu.
type TrayIcon struct {
	notifyIcon     *walk.NotifyIcon
	appController  *app.Controller
	mainWindow     *walk.MainWindow
	overlayWindow  *OverlayWindow
}

// NewTrayIcon creates and configures the system tray icon.
func NewTrayIcon(mainWindow *walk.MainWindow, appController *app.Controller, overlayWindow *OverlayWindow) (*TrayIcon, error) {
	notifyIcon, err := walk.NewNotifyIcon(mainWindow)
	if err != nil {
		return nil, err
	}

	trayIcon := &TrayIcon{
		notifyIcon:    notifyIcon,
		appController: appController,
		mainWindow:    mainWindow,
		overlayWindow: overlayWindow,
	}

	notifyIcon.SetToolTip("supcua-v2 - Inactive")
	notifyIcon.SetVisible(true)

	trayIcon.buildContextMenu()

	notifyIcon.MouseDown().Attach(func(mouseX, mouseY int, mouseButton walk.MouseButton) {
		if mouseButton == walk.LeftButton {
			mainWindow.SetVisible(!mainWindow.Visible())
			if mainWindow.Visible() {
				mainWindow.Activate()
			}
		}
	})

	return trayIcon, nil
}

func (trayIcon *TrayIcon) buildContextMenu() {
	contextMenu := trayIcon.notifyIcon.ContextMenu()

	toggleStatusAction := walk.NewAction()
	toggleStatusAction.SetText("Toggle Active")
	toggleStatusAction.Triggered().Attach(func() {
		trayIcon.appController.ToggleGlobalStatus()
		trayIcon.UpdateTooltip()
	})
	contextMenu.Actions().Add(toggleStatusAction)

	showWindowAction := walk.NewAction()
	showWindowAction.SetText("Show Window")
	showWindowAction.Triggered().Attach(func() {
		trayIcon.mainWindow.SetVisible(true)
		trayIcon.mainWindow.Activate()
	})
	contextMenu.Actions().Add(showWindowAction)

	if trayIcon.overlayWindow != nil {
		toggleOverlayAction := walk.NewAction()
		toggleOverlayAction.SetText("Toggle Overlay")
		toggleOverlayAction.Triggered().Attach(func() {
			trayIcon.overlayWindow.ToggleVisibility()
		})
		contextMenu.Actions().Add(toggleOverlayAction)
	}

	separatorAction := walk.NewSeparatorAction()
	contextMenu.Actions().Add(separatorAction)

	exitAction := walk.NewAction()
	exitAction.SetText("Exit")
	exitAction.Triggered().Attach(func() {
		trayIcon.Dispose()
		trayIcon.mainWindow.Close()
	})
	contextMenu.Actions().Add(exitAction)
}

// UpdateTooltip refreshes the tooltip text based on current status.
func (trayIcon *TrayIcon) UpdateTooltip() {
	statusText := "Inactive"
	if trayIcon.appController.IsGlobalActive() {
		statusText = "Active"
		runningScriptID := trayIcon.appController.GetRunningScriptID()
		if runningScriptID != "" {
			statusText += " - Running: " + runningScriptID
		}
	}
	trayIcon.notifyIcon.SetToolTip("supcua-v2 - " + statusText)
}

// Dispose cleans up the tray icon.
func (trayIcon *TrayIcon) Dispose() {
	trayIcon.notifyIcon.Dispose()
}
```

**Step 2: Create ui/tray_stub.go**

```go
//go:build !windows

package ui

type TrayIcon struct{}
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 4: Commit**

```bash
git add ui/tray.go ui/tray_stub.go
git commit -m "add system tray icon with context menu"
```

---

### Task 13: HUD Overlay

**Files:**
- Create: `ui/overlay.go`
- Create: `ui/overlay_stub.go`

**Step 1: Create ui/overlay.go**

```go
//go:build windows

package ui

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/app"
)

var (
	gdi32DLL              = syscall.NewLazyDLL("gdi32.dll")
	procCreateSolidBrush  = gdi32DLL.NewProc("CreateSolidBrush")
)

const (
	overlayWidth  = 180
	overlayHeight = 32

	colorGreenActive   = 0x0000FF00
	colorRedInactive   = 0x000000FF
	colorWhiteText     = 0x00FFFFFF
)

// OverlayWindow is the always-on-top HUD showing global status.
type OverlayWindow struct {
	windowHandle  win.HWND
	appController *app.Controller
	positionX     int
	positionY     int
	isVisible     bool
}

// NewOverlayWindow creates the HUD overlay as a layered, topmost, toolwindow.
func NewOverlayWindow(appController *app.Controller, initialX, initialY int, startVisible bool) (*OverlayWindow, error) {
	overlayWindow := &OverlayWindow{
		appController: appController,
		positionX:     initialX,
		positionY:     initialY,
		isVisible:     startVisible,
	}

	className, err := syscall.UTF16PtrFromString("supcua_overlay")
	if err != nil {
		return nil, err
	}

	windowClass := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		LpfnWndProc:   syscall.NewCallback(overlayWindow.windowProcedure),
		HInstance:      win.GetModuleHandle(nil),
		LpszClassName: className,
		HbrBackground: win.HBRUSH(win.GetStockObject(win.BLACK_BRUSH)),
	}
	win.RegisterClassEx(&windowClass)

	windowTitle, _ := syscall.UTF16PtrFromString("supcua-v2 Overlay")

	extendedStyle := uint32(win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW | win.WS_EX_LAYERED)

	overlayWindow.windowHandle = win.CreateWindowEx(
		extendedStyle,
		className,
		windowTitle,
		win.WS_POPUP,
		int32(initialX), int32(initialY),
		overlayWidth, overlayHeight,
		0, 0, windowClass.HInstance, nil,
	)

	if overlayWindow.windowHandle == 0 {
		return nil, fmt.Errorf("failed to create overlay window")
	}

	// Set 80% opacity
	win.SetLayeredWindowAttributes(overlayWindow.windowHandle, 0, 200, win.LWA_ALPHA)

	if startVisible {
		win.ShowWindow(overlayWindow.windowHandle, win.SW_SHOWNOACTIVATE)
	}

	return overlayWindow, nil
}

func (overlayWindow *OverlayWindow) windowProcedure(hwnd win.HWND, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case win.WM_PAINT:
		overlayWindow.paint()
		return 0
	case win.WM_RBUTTONUP:
		// Right-click could show context menu — handled via tray for now
		return 0
	case win.WM_NCHITTEST:
		// Allow dragging by treating entire window as title bar
		return win.HTCAPTION
	}
	return win.DefWindowProc(hwnd, message, wParam, lParam)
}

func (overlayWindow *OverlayWindow) paint() {
	var paintStruct win.PAINTSTRUCT
	deviceContext := win.BeginPaint(overlayWindow.windowHandle, &paintStruct)
	defer win.EndPaint(overlayWindow.windowHandle, &paintStruct)

	var backgroundColor uint32
	var statusText string
	if overlayWindow.appController.IsGlobalActive() {
		backgroundColor = colorGreenActive
		statusText = "ACTIVE"
		runningScriptID := overlayWindow.appController.GetRunningScriptID()
		if runningScriptID != "" {
			statusText = "RUN: " + runningScriptID
		}
	} else {
		backgroundColor = colorRedInactive
		statusText = "INACTIVE"
	}

	brushHandle, _, _ := procCreateSolidBrush.Call(uintptr(backgroundColor))
	clientRect := win.RECT{Left: 0, Top: 0, Right: overlayWidth, Bottom: overlayHeight}
	win.FillRect(deviceContext, &clientRect, win.HBRUSH(brushHandle))
	win.DeleteObject(win.HGDIOBJ(brushHandle))

	win.SetBkMode(deviceContext, win.TRANSPARENT)
	win.SetTextColor(deviceContext, win.COLORREF(colorWhiteText))

	textUTF16, _ := syscall.UTF16FromString(statusText)
	textRect := clientRect
	win.DrawTextEx(deviceContext, &textUTF16[0], int32(len(textUTF16)-1), &textRect,
		win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE, nil)
}

// Refresh triggers a repaint of the overlay.
func (overlayWindow *OverlayWindow) Refresh() {
	win.InvalidateRect(overlayWindow.windowHandle, nil, true)
}

// ToggleVisibility shows or hides the overlay.
func (overlayWindow *OverlayWindow) ToggleVisibility() {
	overlayWindow.isVisible = !overlayWindow.isVisible
	if overlayWindow.isVisible {
		win.ShowWindow(overlayWindow.windowHandle, win.SW_SHOWNOACTIVATE)
	} else {
		win.ShowWindow(overlayWindow.windowHandle, win.SW_HIDE)
	}
}

// IsVisible returns whether the overlay is currently shown.
func (overlayWindow *OverlayWindow) IsVisible() bool {
	return overlayWindow.isVisible
}

// GetPosition returns the current overlay position.
func (overlayWindow *OverlayWindow) GetPosition() (int, int) {
	return overlayWindow.positionX, overlayWindow.positionY
}

// Destroy cleans up the overlay window.
func (overlayWindow *OverlayWindow) Destroy() {
	win.DestroyWindow(overlayWindow.windowHandle)
}
```

**Step 2: Create ui/overlay_stub.go**

```go
//go:build !windows

package ui

type OverlayWindow struct {
	isVisible bool
}

func (overlayWindow *OverlayWindow) Refresh()           {}
func (overlayWindow *OverlayWindow) ToggleVisibility()   { overlayWindow.isVisible = !overlayWindow.isVisible }
func (overlayWindow *OverlayWindow) IsVisible() bool     { return overlayWindow.isVisible }
func (overlayWindow *OverlayWindow) GetPosition() (int, int) { return 0, 0 }
func (overlayWindow *OverlayWindow) Destroy()            {}
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 4: Commit**

```bash
git add ui/overlay.go ui/overlay_stub.go
git commit -m "add HUD overlay with status display and drag support"
```

---

### Task 14: Main Window Shell

**Files:**
- Create: `ui/main_window.go`
- Create: `ui/main_window_stub.go`

**Step 1: Create ui/main_window.go**

This creates the main window with a left sidebar (script list) and right panel (editor area). Detailed script list and editor widgets come in later tasks.

```go
//go:build windows

package ui

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/storage"
)

// MainWindowController manages the main application window.
type MainWindowController struct {
	mainWindow      *walk.MainWindow
	appController   *app.Controller
	scriptStore     *storage.ScriptStore
	scriptListBox   *walk.ListBox
	editorContainer *walk.Composite
}

// CreateMainWindow builds and returns the main application window (hidden initially).
func CreateMainWindow(appController *app.Controller, scriptStore *storage.ScriptStore) (*walk.MainWindow, *MainWindowController, error) {
	windowController := &MainWindowController{
		appController: appController,
		scriptStore:   scriptStore,
	}

	var mainWindowInstance *walk.MainWindow

	err := (MainWindow{
		AssignTo: &mainWindowInstance,
		Title:    "supcua-v2 - Script Automation",
		MinSize:  Size{Width: 800, Height: 500},
		Size:     Size{Width: 1000, Height: 600},
		Layout:   HBox{MarginsZero: true},
		Visible:  false,
		Children: []Widget{
			ListBox{
				AssignTo:    &windowController.scriptListBox,
				MinSize:     Size{Width: 200},
				MaxSize:     Size{Width: 250},
				OnCurrentIndexChanged: func() {
					windowController.onScriptSelected()
				},
			},
			Composite{
				AssignTo: &windowController.editorContainer,
				Layout:   VBox{},
			},
		},
		MenuItems: []MenuItem{
			Menu{
				Text: "&Script",
				Items: []MenuItem{
					Action{
						Text:        "&New Script",
						OnTriggered: func() { windowController.onNewScript() },
					},
					Action{
						Text:        "&Delete Script",
						OnTriggered: func() { windowController.onDeleteScript() },
					},
				},
			},
		},
	}).Create()

	if err != nil {
		return nil, nil, err
	}

	windowController.mainWindow = mainWindowInstance
	windowController.refreshScriptList()

	return mainWindowInstance, windowController, nil
}

func (windowController *MainWindowController) refreshScriptList() {
	allScripts, err := windowController.scriptStore.List()
	if err != nil {
		return
	}

	scriptNames := make([]string, len(allScripts))
	for index, script := range allScripts {
		statusPrefix := "[ ] "
		if script.Enabled {
			statusPrefix = "[x] "
		}
		scriptNames[index] = statusPrefix + script.Name
	}

	windowController.scriptListBox.SetModel(scriptNames)
}

func (windowController *MainWindowController) onScriptSelected() {
	// Will be implemented in script editor task
}

func (windowController *MainWindowController) onNewScript() {
	// Will be implemented in script editor task
}

func (windowController *MainWindowController) onDeleteScript() {
	// Will be implemented in script editor task
}
```

**Step 2: Create ui/main_window_stub.go**

```go
//go:build !windows

package ui

type MainWindowController struct{}
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 4: Commit**

```bash
git add ui/main_window.go ui/main_window_stub.go
git commit -m "add main window shell with script list sidebar and editor area"
```

---

### Task 15: Script Editor (Form-Based)

**Files:**
- Create: `ui/script_editor.go`
- Create: `ui/script_editor_stub.go`

**Step 1: Create ui/script_editor.go**

```go
//go:build windows

package ui

import (
	"github.com/google/uuid"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
)

// ScriptEditor provides the form-based script editing UI.
type ScriptEditor struct {
	container          *walk.Composite
	scriptStore        *storage.ScriptStore
	currentScript      *model.Script
	refreshListCallback func()

	// Form fields
	nameEdit           *walk.LineEdit
	enabledCheckbox    *walk.CheckBox
	runModeComboBox    *walk.ComboBox
	hotkeyModeComboBox *walk.ComboBox
	actionListBox      *walk.ListBox
}

// NewScriptEditor creates the form editor inside the given parent container.
func NewScriptEditor(parentContainer *walk.Composite, scriptStore *storage.ScriptStore, refreshListCallback func()) (*ScriptEditor, error) {
	scriptEditor := &ScriptEditor{
		scriptStore:        scriptStore,
		refreshListCallback: refreshListCallback,
	}

	var editorComposite *walk.Composite

	builder := NewBuilder(parentContainer)

	err := (Composite{
		AssignTo: &editorComposite,
		Layout:   VBox{},
		Children: []Widget{
			GroupBox{
				Title:  "Script Properties",
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "Name:"},
					LineEdit{AssignTo: &scriptEditor.nameEdit},

					Label{Text: "Enabled:"},
					CheckBox{AssignTo: &scriptEditor.enabledCheckbox},

					Label{Text: "Run Mode:"},
					ComboBox{
						AssignTo: &scriptEditor.runModeComboBox,
						Model:    []string{"once", "infinite"},
						Editable: false,
					},

					Label{Text: "Hotkey Mode:"},
					ComboBox{
						AssignTo: &scriptEditor.hotkeyModeComboBox,
						Model:    []string{"toggle", "separate"},
						Editable: false,
					},
				},
			},
			GroupBox{
				Title:  "Actions",
				Layout: VBox{},
				Children: []Widget{
					ListBox{
						AssignTo: &scriptEditor.actionListBox,
						MinSize:  Size{Height: 200},
					},
					Composite{
						Layout: HBox{},
						Children: []Widget{
							PushButton{
								Text: "Add Action",
								OnClicked: func() { scriptEditor.onAddAction() },
							},
							PushButton{
								Text: "Edit Action",
								OnClicked: func() { scriptEditor.onEditAction() },
							},
							PushButton{
								Text: "Delete Action",
								OnClicked: func() { scriptEditor.onDeleteAction() },
							},
							PushButton{
								Text: "Move Up",
								OnClicked: func() { scriptEditor.onMoveActionUp() },
							},
							PushButton{
								Text: "Move Down",
								OnClicked: func() { scriptEditor.onMoveActionDown() },
							},
						},
					},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text: "Save",
						OnClicked: func() { scriptEditor.onSave() },
					},
				},
			},
		},
	}).Create(builder)

	if err != nil {
		return nil, err
	}

	scriptEditor.container = editorComposite
	return scriptEditor, nil
}

// LoadScript populates the editor with the given script's data.
func (scriptEditor *ScriptEditor) LoadScript(script *model.Script) {
	scriptEditor.currentScript = script
	scriptEditor.nameEdit.SetText(script.Name)
	scriptEditor.enabledCheckbox.SetChecked(script.Enabled)

	runModeIndex := 0
	if script.RunMode == model.RunModeInfinite {
		runModeIndex = 1
	}
	scriptEditor.runModeComboBox.SetCurrentIndex(runModeIndex)

	hotkeyModeIndex := 0
	if script.HotkeyMode == model.HotkeyModeSeparate {
		hotkeyModeIndex = 1
	}
	scriptEditor.hotkeyModeComboBox.SetCurrentIndex(hotkeyModeIndex)

	scriptEditor.refreshActionList()
}

// NewEmptyScript creates a new script with defaults and loads it.
func (scriptEditor *ScriptEditor) NewEmptyScript() {
	newScript := &model.Script{
		ID:         uuid.New().String(),
		Name:       "New Script",
		Enabled:    false,
		RunMode:    model.RunModeOnce,
		HotkeyMode: model.HotkeyModeToggle,
		Actions:    []model.Action{},
	}
	scriptEditor.LoadScript(newScript)
}

func (scriptEditor *ScriptEditor) refreshActionList() {
	if scriptEditor.currentScript == nil {
		return
	}
	actionDescriptions := make([]string, len(scriptEditor.currentScript.Actions))
	for index, action := range scriptEditor.currentScript.Actions {
		actionDescriptions[index] = describeAction(action)
	}
	scriptEditor.actionListBox.SetModel(actionDescriptions)
}

func describeAction(action model.Action) string {
	switch action.Type {
	case model.ActionMouseMove:
		if action.Source == "found" {
			return "Mouse Move → found position"
		}
		return fmt.Sprintf("Mouse Move → (%d, %d)", action.X, action.Y)
	case model.ActionMouseLeftDown:
		return "Mouse Left Down"
	case model.ActionMouseLeftUp:
		return "Mouse Left Up"
	case model.ActionMouseRightDown:
		return "Mouse Right Down"
	case model.ActionMouseRightUp:
		return "Mouse Right Up"
	case model.ActionKeyDown:
		return "Key Down: " + action.Key
	case model.ActionKeyUp:
		return "Key Up: " + action.Key
	case model.ActionDelay:
		return fmt.Sprintf("Delay: %dms", action.DelayMs)
	case model.ActionLoop:
		countText := "infinite"
		if action.LoopCount > 0 {
			countText = fmt.Sprintf("%d times", action.LoopCount)
		}
		return fmt.Sprintf("Loop (%s, %d actions)", countText, len(action.Actions))
	case model.ActionWaitForImage:
		return fmt.Sprintf("Wait For Image (timeout: %dms)", action.TimeoutMs)
	case model.ActionIfImage:
		return fmt.Sprintf("If Image (then: %d, else: %d)", len(action.ThenActions), len(action.ElseActions))
	default:
		return action.Type
	}
}

func (scriptEditor *ScriptEditor) onSave() {
	if scriptEditor.currentScript == nil {
		return
	}

	scriptEditor.currentScript.Name = scriptEditor.nameEdit.Text()
	scriptEditor.currentScript.Enabled = scriptEditor.enabledCheckbox.Checked()

	if scriptEditor.runModeComboBox.CurrentIndex() == 0 {
		scriptEditor.currentScript.RunMode = model.RunModeOnce
	} else {
		scriptEditor.currentScript.RunMode = model.RunModeInfinite
	}

	if scriptEditor.hotkeyModeComboBox.CurrentIndex() == 0 {
		scriptEditor.currentScript.HotkeyMode = model.HotkeyModeToggle
	} else {
		scriptEditor.currentScript.HotkeyMode = model.HotkeyModeSeparate
	}

	scriptEditor.scriptStore.Save(*scriptEditor.currentScript)
	if scriptEditor.refreshListCallback != nil {
		scriptEditor.refreshListCallback()
	}
}

func (scriptEditor *ScriptEditor) onAddAction()      { /* Dialog in later task */ }
func (scriptEditor *ScriptEditor) onEditAction()      { /* Dialog in later task */ }
func (scriptEditor *ScriptEditor) onDeleteAction() {
	if scriptEditor.currentScript == nil {
		return
	}
	selectedIndex := scriptEditor.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(scriptEditor.currentScript.Actions) {
		return
	}
	scriptEditor.currentScript.Actions = append(
		scriptEditor.currentScript.Actions[:selectedIndex],
		scriptEditor.currentScript.Actions[selectedIndex+1:]...,
	)
	scriptEditor.refreshActionList()
}

func (scriptEditor *ScriptEditor) onMoveActionUp() {
	if scriptEditor.currentScript == nil {
		return
	}
	selectedIndex := scriptEditor.actionListBox.CurrentIndex()
	if selectedIndex <= 0 {
		return
	}
	actions := scriptEditor.currentScript.Actions
	actions[selectedIndex], actions[selectedIndex-1] = actions[selectedIndex-1], actions[selectedIndex]
	scriptEditor.refreshActionList()
	scriptEditor.actionListBox.SetCurrentIndex(selectedIndex - 1)
}

func (scriptEditor *ScriptEditor) onMoveActionDown() {
	if scriptEditor.currentScript == nil {
		return
	}
	selectedIndex := scriptEditor.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(scriptEditor.currentScript.Actions)-1 {
		return
	}
	actions := scriptEditor.currentScript.Actions
	actions[selectedIndex], actions[selectedIndex+1] = actions[selectedIndex+1], actions[selectedIndex]
	scriptEditor.refreshActionList()
	scriptEditor.actionListBox.SetCurrentIndex(selectedIndex + 1)
}
```

**Step 2: Create ui/script_editor_stub.go**

```go
//go:build !windows

package ui

type ScriptEditor struct{}
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors (note: `fmt` import needed in script_editor.go for Sprintf calls — ensure it's in the imports)

**Step 4: Commit**

```bash
git add ui/script_editor.go ui/script_editor_stub.go
git commit -m "add form-based script editor with action list management"
```

---

### Task 16: Raw JSON Editor

**Files:**
- Create: `ui/raw_editor.go`
- Create: `ui/raw_editor_stub.go`

**Step 1: Create ui/raw_editor.go**

```go
//go:build windows

package ui

import (
	"encoding/json"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/model"
)

// RawJSONEditor provides a text area for direct JSON editing of a script.
type RawJSONEditor struct {
	textEdit      *walk.TextEdit
	currentScript *model.Script
}

// NewRawJSONEditor creates the raw JSON editor inside the given parent.
func NewRawJSONEditor(parentContainer *walk.Composite) (*RawJSONEditor, error) {
	rawEditor := &RawJSONEditor{}

	builder := NewBuilder(parentContainer)

	err := (TextEdit{
		AssignTo: &rawEditor.textEdit,
		VScroll:  true,
		HScroll:  true,
	}).Create(builder)

	if err != nil {
		return nil, err
	}

	return rawEditor, nil
}

// LoadScript serializes the script to formatted JSON and displays it.
func (rawEditor *RawJSONEditor) LoadScript(script *model.Script) {
	rawEditor.currentScript = script
	jsonBytes, err := json.MarshalIndent(script, "", "  ")
	if err != nil {
		rawEditor.textEdit.SetText("Error: " + err.Error())
		return
	}
	rawEditor.textEdit.SetText(string(jsonBytes))
}

// GetEditedScript parses the current JSON text back into a Script.
func (rawEditor *RawJSONEditor) GetEditedScript() (*model.Script, error) {
	var editedScript model.Script
	err := json.Unmarshal([]byte(rawEditor.textEdit.Text()), &editedScript)
	if err != nil {
		return nil, err
	}
	return &editedScript, nil
}
```

**Step 2: Create ui/raw_editor_stub.go**

```go
//go:build !windows

package ui

type RawJSONEditor struct{}
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 4: Commit**

```bash
git add ui/raw_editor.go ui/raw_editor_stub.go
git commit -m "add raw JSON editor for power user script editing"
```

---

### Task 17: Wire Everything in main.go

**Files:**
- Modify: `main.go`

**Step 1: Create the Windows app manifest**

Walk requires an application manifest for visual styles. Create `main.manifest`:

```xml
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
</assembly>
```

**Step 2: Create rsrc.syso (on Windows)**

Run: `go get github.com/akavel/rsrc && rsrc -manifest main.manifest -o rsrc.syso`

For non-Windows development, this step is skipped — the app compiles but won't run without Walk.

**Step 3: Update main.go**

```go
//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/hotkey"
	imagedetector "github.com/yanmad27/supcua-v2/image"
	"github.com/yanmad27/supcua-v2/storage"
	"github.com/yanmad27/supcua-v2/ui"
)

func main() {
	log.SetOutput(os.Stdout)
	log.Println("supcua-v2 starting...")

	executablePath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path: %v", err)
	}
	applicationDirectory := filepath.Dir(executablePath)

	// Storage
	configStore := storage.NewConfigStore(filepath.Join(applicationDirectory, "config.json"))
	appConfig, err := configStore.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	scriptsDirectory := appConfig.ScriptsDir
	if !filepath.IsAbs(scriptsDirectory) {
		scriptsDirectory = filepath.Join(applicationDirectory, scriptsDirectory)
	}
	scriptStore := storage.NewScriptStore(scriptsDirectory)

	// Core components
	inputSimulator := executor.NewInputSimulator()
	imageDetectorInstance := imagedetector.NewDetector(10)
	scriptExecutor := executor.NewExecutor(inputSimulator, imageDetectorInstance)
	hotkeyManager := hotkey.NewManager()

	// Application controller
	appController := app.NewController(scriptExecutor, hotkeyManager, scriptStore, configStore)

	// UI
	mainWindow, _, err := ui.CreateMainWindow(appController, scriptStore)
	if err != nil {
		log.Fatalf("failed to create main window: %v", err)
	}

	// Overlay
	var overlayWindow *ui.OverlayWindow
	if appConfig.OverlayEnabled {
		overlayWindow, err = ui.NewOverlayWindow(
			appController,
			appConfig.OverlayPosition.X,
			appConfig.OverlayPosition.Y,
			true,
		)
		if err != nil {
			log.Printf("warning: failed to create overlay: %v", err)
		}
	}

	// System tray
	trayIcon, err := ui.NewTrayIcon(mainWindow, appController, overlayWindow)
	if err != nil {
		log.Fatalf("failed to create tray icon: %v", err)
	}
	defer trayIcon.Dispose()

	// Status change callback — refresh overlay and tray
	appController.OnStatusChange = func() {
		if overlayWindow != nil {
			overlayWindow.Refresh()
		}
		trayIcon.UpdateTooltip()
	}

	// Register global hotkey
	_, err = hotkeyManager.Register(appConfig.GlobalHotkey, func(hotkeyID int) {
		appController.ToggleGlobalStatus()
	})
	if err != nil {
		log.Printf("warning: failed to register global hotkey: %v", err)
	}

	// Start hotkey listener in background
	go hotkeyManager.ListenForHotkeys()

	// Register script hotkeys
	registerScriptHotkeys(appController, hotkeyManager, scriptStore)

	// Run the Walk message loop (blocks until window closed)
	mainWindow.Run()

	// Cleanup
	if overlayWindow != nil {
		overlayWindow.Destroy()
	}
	log.Println("supcua-v2 exiting")
}

func registerScriptHotkeys(
	appController *app.Controller,
	hotkeyManager *hotkey.Manager,
	scriptStore *storage.ScriptStore,
) {
	allScripts, err := scriptStore.List()
	if err != nil {
		log.Printf("warning: failed to list scripts for hotkey registration: %v", err)
		return
	}

	for _, script := range allScripts {
		if !script.Enabled {
			continue
		}
		currentScript := script // capture for closure

		switch script.HotkeyMode {
		case "toggle":
			if script.ToggleHotkey != nil {
				hotkeyManager.Register(*script.ToggleHotkey, func(hotkeyID int) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScript()
					} else {
						appController.StartScript(currentScript)
					}
				})
			}
		case "separate":
			if script.StartHotkey != nil {
				hotkeyManager.Register(*script.StartHotkey, func(hotkeyID int) {
					appController.StartScript(currentScript)
				})
			}
			if script.StopHotkey != nil {
				hotkeyManager.Register(*script.StopHotkey, func(hotkeyID int) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScript()
					}
				})
			}
		}
	}
}
```

**Step 4: Create main_stub.go for non-Windows**

```go
//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("supcua-v2 requires Windows 10. This binary was built for a non-Windows platform.")
}
```

**Step 5: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 6: Commit**

```bash
git add main.go main_stub.go main.manifest
git commit -m "wire all components together in main entry point"
```

---

### Task 18: Action Editor Dialog

**Files:**
- Create: `ui/action_dialog.go`
- Create: `ui/action_dialog_stub.go`

**Step 1: Create ui/action_dialog.go**

```go
//go:build windows

package ui

import (
	"strconv"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/model"
)

// ShowActionDialog shows a dialog to create or edit an action.
// Returns the action and true if OK was clicked, or zero-value and false if cancelled.
func ShowActionDialog(ownerWindow walk.Form, existingAction *model.Action) (model.Action, bool) {
	var dialogWindow *walk.Dialog
	var actionTypeComboBox *walk.ComboBox
	var parameterXEdit *walk.LineEdit
	var parameterYEdit *walk.LineEdit
	var keyNameEdit *walk.LineEdit
	var delayMsEdit *walk.LineEdit
	var loopCountEdit *walk.LineEdit
	var acceptedResult bool

	actionTypeNames := []string{
		"mouse_move", "mouse_left_down", "mouse_left_up",
		"mouse_right_down", "mouse_right_up",
		"key_down", "key_up", "delay", "loop",
		"wait_for_image", "if_image",
	}

	initialType := 0
	initialX := ""
	initialY := ""
	initialKey := ""
	initialDelay := ""
	initialLoopCount := ""

	if existingAction != nil {
		for typeIndex, typeName := range actionTypeNames {
			if typeName == existingAction.Type {
				initialType = typeIndex
				break
			}
		}
		initialX = strconv.Itoa(existingAction.X)
		initialY = strconv.Itoa(existingAction.Y)
		initialKey = existingAction.Key
		initialDelay = strconv.Itoa(existingAction.DelayMs)
		initialLoopCount = strconv.Itoa(existingAction.LoopCount)
	}

	dialogDefinition := Dialog{
		AssignTo: &dialogWindow,
		Title:    "Action Editor",
		MinSize:  Size{Width: 350, Height: 250},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "Type:"},
					ComboBox{
						AssignTo:     &actionTypeComboBox,
						Model:        actionTypeNames,
						CurrentIndex: initialType,
						Editable:     false,
					},
					Label{Text: "X:"},
					LineEdit{AssignTo: &parameterXEdit, Text: initialX},
					Label{Text: "Y:"},
					LineEdit{AssignTo: &parameterYEdit, Text: initialY},
					Label{Text: "Key:"},
					LineEdit{AssignTo: &keyNameEdit, Text: initialKey},
					Label{Text: "Delay (ms):"},
					LineEdit{AssignTo: &delayMsEdit, Text: initialDelay},
					Label{Text: "Loop Count (0=inf):"},
					LineEdit{AssignTo: &loopCountEdit, Text: initialLoopCount},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text: "OK",
						OnClicked: func() {
							acceptedResult = true
							dialogWindow.Accept()
						},
					},
					PushButton{
						Text: "Cancel",
						OnClicked: func() {
							dialogWindow.Cancel()
						},
					},
				},
			},
		},
	}

	dialogDefinition.Run(ownerWindow)

	if !acceptedResult {
		return model.Action{}, false
	}

	selectedType := actionTypeNames[actionTypeComboBox.CurrentIndex()]
	parsedX, _ := strconv.Atoi(parameterXEdit.Text())
	parsedY, _ := strconv.Atoi(parameterYEdit.Text())
	parsedDelay, _ := strconv.Atoi(delayMsEdit.Text())
	parsedLoopCount, _ := strconv.Atoi(loopCountEdit.Text())

	resultAction := model.Action{
		Type:      selectedType,
		X:         parsedX,
		Y:         parsedY,
		Key:       keyNameEdit.Text(),
		DelayMs:   parsedDelay,
		LoopCount: parsedLoopCount,
	}

	// Preserve nested actions from existing action if editing a loop/if_image
	if existingAction != nil {
		resultAction.Actions = existingAction.Actions
		resultAction.ThenActions = existingAction.ThenActions
		resultAction.ElseActions = existingAction.ElseActions
		resultAction.Image = existingAction.Image
		resultAction.SearchRegion = existingAction.SearchRegion
		resultAction.UseFoundPosition = existingAction.UseFoundPosition
		resultAction.TimeoutMs = existingAction.TimeoutMs
	}

	return resultAction, true
}
```

**Step 2: Create ui/action_dialog_stub.go**

```go
//go:build !windows

package ui

type actionDialogStub struct{}
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 4: Commit**

```bash
git add ui/action_dialog.go ui/action_dialog_stub.go
git commit -m "add action editor dialog for creating and editing script actions"
```

---

### Task 19: Hotkey Input Widget

**Files:**
- Create: `ui/hotkey_input.go`
- Create: `ui/hotkey_input_stub.go`

This widget lets users press a key combo to set a hotkey (like a "press any key" field).

**Step 1: Create ui/hotkey_input.go**

```go
//go:build windows

package ui

import (
	"fmt"
	"strings"

	"github.com/lxn/walk"

	"github.com/yanmad27/supcua-v2/model"
)

// HotkeyInputWidget displays the current hotkey and captures a new one on click.
type HotkeyInputWidget struct {
	displayLabel  *walk.LineEdit
	capturedHotkey *model.Hotkey
	isCapturing   bool
}

// FormatHotkey returns a human-readable string for a hotkey.
func FormatHotkey(hotkeyValue *model.Hotkey) string {
	if hotkeyValue == nil {
		return "(none)"
	}
	parts := make([]string, 0, len(hotkeyValue.Modifiers)+1)
	for _, modifierName := range hotkeyValue.Modifiers {
		parts = append(parts, strings.Title(modifierName))
	}
	parts = append(parts, strings.ToUpper(hotkeyValue.Key))
	return strings.Join(parts, " + ")
}

// GetCapturedHotkey returns the last captured hotkey.
func (hotkeyInput *HotkeyInputWidget) GetCapturedHotkey() *model.Hotkey {
	return hotkeyInput.capturedHotkey
}

// SetHotkey sets the displayed hotkey.
func (hotkeyInput *HotkeyInputWidget) SetHotkey(hotkeyValue *model.Hotkey) {
	hotkeyInput.capturedHotkey = hotkeyValue
	if hotkeyInput.displayLabel != nil {
		hotkeyInput.displayLabel.SetText(FormatHotkey(hotkeyValue))
	}
}
```

**Step 2: Create ui/hotkey_input_stub.go**

```go
//go:build !windows

package ui

type HotkeyInputWidget struct{}
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 4: Commit**

```bash
git add ui/hotkey_input.go ui/hotkey_input_stub.go
git commit -m "add hotkey input widget for capturing key combinations"
```

---

### Task 20: End-to-End Manual Test on Windows

**Files:** None (manual testing)

**Step 1: Build the Windows executable**

Run (on Windows or cross-compile):
```bash
GOOS=windows GOARCH=amd64 go build -o supcua-v2.exe .
```

**Step 2: Test checklist**

Run the app on Windows 10 and verify:

1. [ ] App starts, tray icon appears
2. [ ] Overlay shows "INACTIVE" in red
3. [ ] Global hotkey (Ctrl+Shift+F1) toggles overlay to "ACTIVE" (green)
4. [ ] Main window opens on tray left-click
5. [ ] Can create a new script via menu
6. [ ] Can add actions to script (mouse move, key down/up, delay)
7. [ ] Can save script — JSON file appears in scripts/ directory
8. [ ] Can assign toggle hotkey to script
9. [ ] Hotkey starts script — mouse moves as configured
10. [ ] Stop hotkey stops script
11. [ ] Global toggle to Inactive stops running script
12. [ ] Overlay toggles on/off via tray menu
13. [ ] Overlay is draggable
14. [ ] Script list shows enabled/disabled status
15. [ ] Raw JSON view shows script JSON
16. [ ] Capture hotkey grabs mouse position

**Step 3: Fix any issues found during testing**

**Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix issues found during manual testing"
```

---

## Summary

| Task | Component | Type |
|------|-----------|------|
| 1 | Project scaffolding | Setup |
| 2 | Data models | Core |
| 3 | Config storage | Core + Tests |
| 4 | Script storage (CRUD) | Core + Tests |
| 5 | Input simulator (Win32) | Platform |
| 6 | Script executor | Core + Tests |
| 7 | Hotkey manager | Platform |
| 8 | Hotkey conflict detection | Core + Tests |
| 9 | Image detector | Platform |
| 10 | Mouse capture | Platform |
| 11 | Application controller | Core + Tests |
| 12 | System tray | UI |
| 13 | HUD overlay | UI |
| 14 | Main window shell | UI |
| 15 | Script editor (form) | UI |
| 16 | Raw JSON editor | UI |
| 17 | Wire main.go | Integration |
| 18 | Action editor dialog | UI |
| 19 | Hotkey input widget | UI |
| 20 | End-to-end manual test | Testing |
