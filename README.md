# supcua-v2

A Windows desktop automation tool for creating and running macro scripts with hotkey triggers, image recognition, and an always-on-top overlay HUD.

## Features

- **Script automation** — build scripts from mouse, keyboard, delay, loop, and image-recognition actions
- **Global hotkeys** — toggle scripts on/off with customizable key bindings
- **Image recognition** — wait for images to appear on screen or branch conditionally
- **Script queue** — multiple hotkey triggers queue up and execute sequentially
- **Overlay HUD** — always-on-top status display showing current run state
- **System tray** — minimize to tray, toggle global active state, control overlay
- **Auto-update** — checks the latest GitHub Release for new versions and self-updates
- **Single instance** — prevents multiple copies from running simultaneously
- **License check** — MAC address-based machine authorization

## Usage

### Main Window

| Column | Description |
|--------|-------------|
| Name | Script name |
| Hotkey | Assigned trigger key |
| Mode | Once / Infinite |

- **Checkbox** next to each script enables/disables it
- **Global Active** checkbox is the master on/off switch (also bound to a hotkey, default: `RCtrl`)
- Close the window hides it to the tray; use tray → **Exit** to quit

### Overlay

A small 136×32 HUD shows current status:

| Status | Color | Meaning |
|--------|-------|---------|
| `DISABLE` | Red | Global active is off |
| `IDLE` | Gray | Active, no script running |
| `RUN: name` | Green | Script is executing |

The overlay can be dragged to any position via tray → **Overlay Drag Mode**.

### Scripts

Scripts are stored as JSON files in the `scripts/` directory. Each script contains:

- **Run mode**: `once` (single execution) or `infinite` (loops until stopped)
- **Hotkey mode**: `toggle` (one key starts/stops) or `separate` (dedicated start/stop keys)
- **Actions**: ordered list of steps

#### Action Types

| Category | Actions |
|----------|---------|
| Mouse | move, left/right click, double-click, button down/up, store position, move to stored |
| Keyboard | press, key down, key up |
| Timing | delay (ms) |
| Control | loop / loop_end with repeat count |
| Image | wait_for_image, if_image (with tolerance and optional mouse move to found position) |

## Configuration (`config.json`)

```json
{
  "global_hotkey": {"modifiers": [], "key": "rctrl"},
  "capture_hotkey": {"modifiers": [], "key": "p"},
  "overlay_enabled": true,
  "overlay_position": {"x": 649, "y": 1010},
  "scripts_dir": "./scripts"
}
```

| Key | Default | Description |
|-----|---------|-------------|
| `global_hotkey` | `rctrl` | Master toggle hotkey |
| `capture_hotkey` | `p` | Key used to capture hotkeys in the UI |
| `overlay_enabled` | `true` | Show/hide the HUD |
| `scripts_dir` | `./scripts` | Path to script JSON files |

## Build & Release

### Prerequisites

- Go 1.25.x (required for garble compatibility)
- Node.js 26.x and npm (builds the `frontend/` UI embedded in the exe)
- Microsoft Edge WebView2 Runtime to run the app (preinstalled on Windows 11)
- [garble](https://github.com/burrowers/garble) — `go install mvdan.cc/garble@latest`
- [rsrc](https://github.com/akavel/rsrc) — `go install github.com/akavel/rsrc@latest`

### Local build

```bash
scripts/build.sh --version dev          # frontend + rsrc + garble -literals -> ./supcua-v2.exe
scripts/build.sh --plain --out /tmp/x   # plain go build, no garble
```

`scripts/build.sh` copies `frontend/dist` into `ui/webview/assets/dist` for the build only and restores the committed placeholder afterwards.

### Release

Releases are made by release-please: merge Conventional Commits into `main`, merge the release PR it opens, and CI tags `vX.Y.Z`, creates the GitHub Release and attaches `supcua-v2.exe`. If the build or upload failed, re-run Actions → release-please → Run workflow with `tag` set to the existing tag. See [CONTRIBUTING.md](CONTRIBUTING.md).

`bash release.sh [version]` only builds `supcua-v2.exe` locally.

> **Note:** garble is scoped to `github.com/yanmad27/supcua-v2` only. The `walk` UI library is excluded from obfuscation because it uses reflection internally.

## Updating from v1.0.28 or older

The in-app updater reads the latest GitHub Release of `aceonline-tools/supcua-v2` (asset `supcua-v2.exe`). Clients on v1.0.28 or older do not auto-update to the new version: download `supcua-v2.exe` once from https://github.com/aceonline-tools/supcua-v2/releases/latest, and from then on updates come from GitHub Releases. Drafts, pre-releases and tags that are not plain `vX.Y.Z` are never offered.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for commit conventions, CI, and the release process.
