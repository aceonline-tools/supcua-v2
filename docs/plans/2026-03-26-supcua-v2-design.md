# supcua-v2 Design Document

## Overview

Windows 10 mouse/keyboard automation tool built in Go. Users create scripts containing mouse and keyboard actions that execute line by line, triggered by global hotkeys.

## Tech Stack

- **Language:** Go
- **GUI:** Walk (native Win32)
- **Input simulation:** user32.dll via syscall (SetCursorPos, mouse_event, keybd_event)
- **Global hotkeys:** Win32 RegisterHotKey API
- **Storage:** JSON files (one per script)

## Architecture

```
+----------------+  +------------------+
| System Tray    |  | HUD Overlay      |
| Icon/Menu      |  | (always on top)  |
+----------------+  +------------------+
+--------------------------------------+
|           Main Window                |
| +-------------+--------------------+ |
| | Script List | Script Editor      | |
| | (sidebar)   | (form + raw JSON)  | |
| +-------------+--------------------+ |
+--------------------------------------+
+--------------------------------------+
|          Hotkey Manager              |
| (register/unregister global combos)  |
+--------------------------------------+
+--------------------------------------+
|         Script Executor              |
| (runs actions, handles loops)        |
+--------------------------------------+
+--------------------------------------+
|        Input Simulator               |
| (mouse/keyboard via user32.dll)      |
+--------------------------------------+
+--------------------------------------+
|        Image Detector                |
| (screen capture + pixel matching)    |
+--------------------------------------+
+--------------------------------------+
|        Mouse Capture                 |
| (grab cursor position via hotkey)    |
+--------------------------------------+
```

## Data Model

### Script (JSON file)

```json
{
  "id": "uuid-string",
  "name": "My Script",
  "enabled": true,
  "run_mode": "once",
  "hotkey_mode": "toggle",
  "toggle_hotkey": { "modifiers": ["ctrl", "shift"], "key": "1" },
  "start_hotkey": null,
  "stop_hotkey": null,
  // modifiers can be empty for single key: { "modifiers": [], "key": "f5" }
  "actions": [
    { "type": "mouse_move", "x": 500, "y": 300 },
    { "type": "mouse_left_down" },
    { "type": "mouse_left_up" },
    { "type": "delay", "ms": 200 },
    { "type": "key_down", "key": "ctrl" },
    { "type": "key_down", "key": "c" },
    { "type": "key_up", "key": "c" },
    { "type": "key_up", "key": "ctrl" },
    {
      "type": "loop",
      "count": 5,
      "actions": [
        { "type": "mouse_move", "x": 100, "y": 100 },
        { "type": "mouse_left_down" },
        { "type": "mouse_left_up" },
        { "type": "delay", "ms": 100 }
      ]
    }
  ]
}
```

### Action Types

| Type | Parameters | Description |
|---|---|---|
| `mouse_move` | `x`, `y` | Move cursor to absolute position |
| `mouse_left_down` | — | Press left mouse button |
| `mouse_left_up` | — | Release left mouse button |
| `mouse_right_down` | — | Press right mouse button |
| `mouse_right_up` | — | Release right mouse button |
| `key_down` | `key` | Press a keyboard key |
| `key_up` | `key` | Release a keyboard key |
| `delay` | `ms` | Wait for milliseconds |
| `loop` | `count`, `actions` | Repeat nested actions (0 = infinite) |
| `wait_for_image` | `image`, `region?`, `timeout_ms`, `use_found_position` | Pause until image found on screen |
| `if_image` | `image`, `region?`, `use_found_position`, `then`, `else?` | Conditional branch based on image presence |
| `mouse_move` (found) | `source: "found"`, `offset_x?`, `offset_y?` | Move to last found image position |

### Image Detection Actions

**wait_for_image** — polls screen until image appears, then continues:
```json
{
  "type": "wait_for_image",
  "image": "base64-encoded-png",
  "region": { "x": 100, "y": 100, "width": 200, "height": 200 },
  "timeout_ms": 5000,
  "use_found_position": true
}
```

**if_image** — single check, runs `then` or `else` actions:
```json
{
  "type": "if_image",
  "image": "base64-encoded-png",
  "region": { "x": 100, "y": 100, "width": 200, "height": 200 },
  "use_found_position": true,
  "then": [ ... ],
  "else": [ ... ]
}
```

**mouse_move with found position:**
```json
{ "type": "mouse_move", "source": "found" }
{ "type": "mouse_move", "source": "found", "offset_x": 10, "offset_y": 5 }
```

- `region` — optional. If null, search whole screen.
- `image` — base64-encoded PNG, captured via editor's region select tool
- `use_found_position` — if true, stores center of matched area as `found_x`/`found_y` for subsequent `mouse_move` with `source: "found"`
- `wait_for_image` timeout → error, script stops
- `if_image` `else` is optional — if omitted and image not found, continues to next action

### Run Modes

- `"once"` — execute actions once, then stop
- `"infinite"` — repeat entire script until stop hotkey or global inactive

### App Config (config.json)

```json
{
  "global_hotkey": { "modifiers": ["ctrl", "shift"], "key": "f1" },
  "capture_hotkey": { "modifiers": ["ctrl", "shift"], "key": "f2" },
  "overlay_enabled": true,
  "overlay_position": { "x": 10, "y": 10 },
  "scripts_dir": "./scripts"
}
```

## Status Model

### Global Status

- **Active** — hotkeys respond, scripts can run
- **Inactive** — all hotkeys ignored, running script cancelled

Toggled via global hotkey or tray icon/overlay.

### Script Status

- **Enabled** — hotkeys registered when global is Active
- **Disabled** — hotkeys not registered, script cannot run

### Execution Logic

```
User presses start/toggle hotkey
  -> Is global Active? No -> ignore
  -> Is script Enabled? No -> ignore
  -> Is ANY script already running? Yes -> ignore
  -> Start executor goroutine

Only one script runs at a time.
```

## Script Executor

- Each execution runs in a goroutine with `context.WithCancel`
- Between every action, check `ctx.Done()` for responsive stopping
- On error (e.g., invalid coordinates) -> stop immediately, set status to error
- Stop triggers: stop hotkey, global goes Inactive, script completes, error

## Hotkey Manager

### Registered Hotkeys

| Purpose | Action |
|---|---|
| Global toggle | Toggle Active/Inactive |
| Script start/toggle | Start script (if slot free) |
| Script stop | Stop running script |
| Mouse capture | Grab cursor position to editor |

### Lifecycle

1. App starts -> register global toggle + capture hotkey
2. Global Active -> register all Enabled scripts' hotkeys
3. Global Inactive -> unregister all script hotkeys
4. Script Enabled toggled -> register/unregister that script's hotkeys

### Conflict Handling

- When a hotkey is already used by another script, prompt user: "This hotkey is already used by [Script Name]. Override?"
- Yes -> clear old script's hotkey, assign to new script, save both
- No -> keep old assignment, new script's field unchanged

## HUD Overlay

- Small, always-on-top, semi-transparent window
- **Can be toggled on/off** via tray menu or app config
- Shows global status: Active (green) / Inactive (red)
- Shows currently running script name or "idle"
- Click-through (WS_EX_TRANSPARENT)
- No taskbar entry (WS_EX_TOOLWINDOW) — hidden from taskbar and Alt+Tab
- Draggable, position saved in config
- Right-click -> context menu (toggle status, open main window, hide overlay, exit)

## Script Editor

- **Form-based** (primary): dropdown for action type, input fields for parameters, add/reorder/delete lines
- **Raw JSON** view: direct JSON editing for power users
- **Mouse capture**: hotkey grabs current cursor position into active coordinate fields

## Project Structure

```
supcua-v2/
├── main.go
├── go.mod
├── go.sum
├── config.json              (runtime)
├── scripts/                 (runtime)
│
├── model/
│   ├── script.go
│   └── config.go
│
├── storage/
│   ├── script_store.go
│   └── config_store.go
│
├── executor/
│   ├── executor.go
│   └── input.go
│
├── hotkey/
│   ├── manager.go
│   └── conflict.go
│
├── ui/
│   ├── main_window.go
│   ├── script_list.go
│   ├── script_editor.go
│   ├── raw_editor.go
│   ├── overlay.go
│   └── tray.go
│
├── image/
│   └── detector.go          # Screen capture + pixel matching
│
└── capture/
    └── capture.go
```
