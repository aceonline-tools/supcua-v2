# supcua-v2

A Windows desktop automation tool for creating and running macro scripts with hotkey triggers, image recognition, and an always-on-top overlay HUD.

## Features

- **Script automation** — build scripts from mouse, keyboard, delay, loop, and image-recognition actions
- **Global hotkeys** — toggle scripts on/off with customizable key bindings
- **Image recognition** — wait for images to appear on screen or branch conditionally
- **Script queue** — multiple hotkey triggers queue up and execute sequentially
- **Overlay HUD** — always-on-top status display showing current run state
- **System tray** — minimize to tray, toggle global active state, control overlay
- **Auto-update** — checks MinIO endpoint for new versions and self-updates
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
- [garble](https://github.com/burrowers/garble) — `go install mvdan.cc/garble@latest`
- [rsrc](https://github.com/akavel/rsrc) — `go install github.com/akavel/rsrc@latest`
- [GitHub CLI](https://cli.github.com/) — for creating releases
- AWS CLI — for MinIO uploads

### Local build

```bash
rsrc -ico app.ico -manifest main.manifest -o rsrc.syso
GOROOT=$(go1.25.0 env GOROOT) PATH="$(go1.25.0 env GOROOT)/bin:$PATH" \
  GOGARBLE="github.com/yanmad27/supcua-v2" \
  garble -literals build -trimpath \
  -ldflags="-H windowsgui -s -w -X github.com/yanmad27/supcua-v2/version.Version=dev" \
  -o supcua-v2.exe .
```

### Release

```bash
bash release.sh         # auto-increments patch version
bash release.sh 1.2.3   # explicit version
```

The script will build, tag, push to GitHub, upload to MinIO, and open the release page.

> **Note:** garble is scoped to `github.com/yanmad27/supcua-v2` only. The `walk` UI library is excluded from obfuscation because it uses reflection internally.

## Updater migration (MinIO → GitHub Releases)

From the first release containing this change, the in-app updater reads the latest GitHub Release of `aceonline-tools/supcua-v2` (`GET /repos/aceonline-tools/supcua-v2/releases/latest`, unauthenticated) and downloads its `supcua-v2.exe` asset. Drafts, pre-releases and tags that are not plain `vX.Y.Z` are never offered. A dev build (`0.0.0`, `dev`) is offered any release; a suffix on the running version (`1.0.29-rc.1`) is ignored for comparison.

Clients **≤ v1.0.28** still read MinIO `latest.txt`. So the **first release containing this change must also be uploaded to MinIO once, manually**, by a maintainer outside CI (needs the `aws` CLI with MinIO credentials). From the repo root, with the release's `supcua-v2.exe` in the current directory and `VERSION` set to the version without `v` (e.g. `1.0.29`):

```bash
MINIO_ENDPOINT="https://dev.hrm.vangtatmining.com.vn/minio"
MINIO_BUCKET="bucket-supcua-v2"
EXE="supcua-v2.exe"
VERSION="1.0.29"

aws --endpoint-url "$MINIO_ENDPOINT" --region us-east-1 configure set default.s3.multipart_threshold 200MB
echo "$VERSION" > latest.txt
aws --endpoint-url "$MINIO_ENDPOINT" --region us-east-1 s3 cp "$EXE" "s3://$MINIO_BUCKET/$EXE"
aws --endpoint-url "$MINIO_ENDPOINT" --region us-east-1 s3 cp latest.txt "s3://$MINIO_BUCKET/latest.txt"
rm latest.txt
```

Upload the exe first, then `latest.txt`, so old clients never see a version whose binary is missing. Do not upload to MinIO for any later release. Keep the bridge exe and `latest.txt` hosted until support for clients ≤ v1.0.28 is intentionally ended.
