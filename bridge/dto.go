package bridge

import "github.com/yanmad27/supcua-v2/model"

// DTOs mirror frontend/src/api/types.ts (canonical). Every field carries an explicit json tag.

type OverlayDTO struct {
	Enabled   bool `json:"enabled"`
	Visible   bool `json:"visible"`
	Draggable bool `json:"draggable"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
}

type AutoActiveDTO struct {
	Enabled     bool   `json:"enabled"`
	ProcessName string `json:"process_name"`
}

type StateDTO struct {
	GlobalActive   bool          `json:"global_active"`
	RunningID      string        `json:"running_id"`
	RunningName    string        `json:"running_name"`
	EditorOpen     bool          `json:"editor_open"`
	Overlay        OverlayDTO    `json:"overlay"`
	Theme          string        `json:"theme"`
	AutoActive     AutoActiveDTO `json:"auto_active"`
	MouseLocked    bool          `json:"mouse_locked"`
	KeyboardLocked bool          `json:"keyboard_locked"`
}

type ScriptSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	RunMode     string `json:"run_mode"`
	HotkeyMode  string `json:"hotkey_mode"`
	HotkeyLabel string `json:"hotkey_label"`
	Enabled     bool   `json:"enabled"`
	Running     bool   `json:"running"`
}

type GroupDTO struct {
	Name       string `json:"name"`
	Count      int    `json:"count"`
	AllEnabled bool   `json:"all_enabled"`
	Collapsed  bool   `json:"collapsed"`
}

type ScriptList struct {
	Rev     uint64          `json:"rev"`
	Scripts []ScriptSummary `json:"scripts"`
	Groups  []GroupDTO      `json:"groups"`
}

type BootState struct {
	Mode           string   `json:"mode"`
	Version        string   `json:"version"`
	Macs           []string `json:"macs"`
	RuntimeVersion string   `json:"runtimeVersion"`
}

type SettingsDTO struct {
	Theme                 string   `json:"theme"`
	GlobalHotkey          string   `json:"global_hotkey"`
	CaptureHotkey         string   `json:"capture_hotkey"`
	OverlayHotkey         string   `json:"overlay_hotkey"`
	OverlayEnabled        bool     `json:"overlay_enabled"`
	KeyPressDurationMs    int      `json:"key_press_duration_ms"`
	ScriptsDir            string   `json:"scripts_dir"`
	AutoActiveEnabled     bool     `json:"auto_active_enabled"`
	AutoActiveProcessName string   `json:"auto_active_process_name"`
	CollapsedGroups       []string `json:"collapsed_groups"`
}

type SettingsPatch struct {
	Theme                 *string `json:"theme"`
	GlobalHotkey          *string `json:"global_hotkey"`
	CaptureHotkey         *string `json:"capture_hotkey"`
	OverlayHotkey         *string `json:"overlay_hotkey"`
	OverlayEnabled        *bool   `json:"overlay_enabled"`
	KeyPressDurationMs    *int    `json:"key_press_duration_ms"`
	ScriptsDir            *string `json:"scripts_dir"`
	AutoActiveEnabled     *bool   `json:"auto_active_enabled"`
	AutoActiveProcessName *string `json:"auto_active_process_name"`
}

type SettingsUpdateResult struct {
	Applied         []string `json:"applied"`
	RestartRequired []string `json:"restartRequired"`
	State           StateDTO `json:"state"`
}

type HotkeyConflict struct {
	ScriptID string       `json:"scriptId"`
	Name     string       `json:"name"`
	Field    string       `json:"field"`
	Hotkey   model.Hotkey `json:"hotkey"`
}

type ParamSpec struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Min     *int   `json:"min,omitempty"`
	Max     *int   `json:"max,omitempty"`
	Default any    `json:"default,omitempty"`
}

type ActionSpec struct {
	Type   string      `json:"type"`
	Group  string      `json:"group"`
	Params []ParamSpec `json:"params"`
}

type EditorSession struct {
	SessionID string       `json:"sessionId"`
	Script    model.Script `json:"script"`
	Catalog   []ActionSpec `json:"catalog"`
}

// Events are emitted through Emitter with these names (contract §9.2).
const (
	EvState           = "state"
	EvScriptsChanged  = "scripts.changed"
	EvSettingsChanged = "settings.changed"
	EvSaveFailed      = "save.failed"
	EvCaptureHotkey   = "capture.hotkey"
	EvCaptureKey      = "capture.key"
	EvCaptureMouse    = "capture.mouse"
	EvCaptureEnded    = "capture.ended"
	EvUpdateAvailable = "update.available"
	EvUpdateProgress  = "update.progress"
	EvUpdateFailed    = "update.failed"
	EvNavGoto         = "nav.goto"
	EvAppClosing      = "app.closing"
	EvEditorReset     = "editor.reset"
)

type empty struct{}

// Typed event payloads: every event the bridge emits has an explicit, json-tagged type.
type scriptsChangedEvent struct {
	Rev uint64 `json:"rev"`
}

type settingsChangedEvent struct {
	Fields []string `json:"fields"`
}

type saveFailedEvent struct {
	ScriptID string `json:"scriptId"`
	Name     string `json:"name"`
	Message  string `json:"message"`
}

type navGotoEvent struct {
	Route string `json:"route"`
}

type appClosingEvent struct {
	DeadlineMs int64 `json:"deadline_ms"`
}

type updateAvailableEvent struct {
	Tag string `json:"tag"`
}

type updateProgressEvent struct {
	Pct int `json:"pct"`
}

type updateFailedEvent struct {
	Message string `json:"message"`
}

type editorResetEvent struct {
	Reason    string `json:"reason"`
	SessionID string `json:"session_id"`
}
