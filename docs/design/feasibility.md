# Modern UI redesign — feasibility (walk vs WebView2 vs Fyne/Gio)

Base: `370b48f` (v1.0.28). Proposal only. **[V]** = verified in this repo / local module cache; **[A]** = assumed from general knowledge of external libraries, not verified (no network).

## Codebase facts that drive the comparison

- UI is lxn/walk `v0.0.0-20210112…` + lxn/win `…20210218…` (`go.mod:6-7`), Go 1.24.5 (`go.mod:3`). Declarative builders, ~62 KB in `ui/` (`wc -c`). [V]
- The "script tree" is **not a TreeView**: it is a `TableView` with `CheckBoxes: true`, `AlternatingRowBG` (`ui/main_window.go:198-204`). Groups are fake: header rows inserted per group (`main_window.go:374-386`, `isHeader`), children get a text prefix `├─ / └─` (`main_window.go:91,399-401`); bold headers via `StyleCell` + one `walk.NewFont` (`main_window.go:79-81,258`). [V]
- Control counts in `ui/*.go` (`Type{` literals): PushButton 34, Label 21, Composite 18, LineEdit 13, GroupBox 8, CheckBox 4, RadioButton 4, NumberEdit 2, TextEdit 1, ListBox 1 (`script_edit_dialog.go:283`). `walk.MsgBox` is used for confirms/errors (`main_window.go:533`, `script_edit_dialog.go:330,712-734`). Only one brush: white `SetBackground` on hotkey edits (`script_edit_dialog.go:364-368`). [V]
- Overlay is a raw Win32 window (`RegisterClass` + `WS_EX_TOPMOST|TOOLWINDOW|LAYERED|TRANSPARENT`, `ui/overlay.go:54-70`), GDI-painted, brush created and deleted per paint (`overlay.go:136-139`). It does not use walk or any toolkit. [V]
- Tray = `walk.NewNotifyIcon` (`ui/tray.go:23`). [V]
- Threading: `mainWindow.Run()` blocks the main goroutine (`main.go:202`); updater uses `mainWindow.Synchronize` (`main.go:102`). Hotkeys (`hotkey/manager.go:19,150,181`) and the mouse/keyboard low-level hooks (`executor/input.go:329-349,380-411`) each run on their **own `LockOSThread` goroutines** with their own message loops — independent of the UI toolkit. [V]
- Manifest: comctl32 v6, PerMonitorV2 DPI (`main.manifest`). Build: `rsrc` + `garble -literals build -trimpath -ldflags="-H windowsgui -s -w"` (`release.sh:28,33`) — **no cgo**. Current artefact: `supcua-v2.exe` 16,000,512 B, zip 8,143,552 B (`supcua-v2.zip`). [V]
- walk handles `NM_CUSTOMDRAW` for TableView internally (`walk/tableview.go:2059,2172,2502`) and exposes only `CellStyler` (font/colour/background per cell). No dark-mode / `DwmSet*` / `SetWindowTheme` anywhere in walk or `ui/`. [V]
- No webview2/wails/fyne/gio in the local module cache; all claims on them are [A].

## (a) Restyle within walk

- Feasible: Segoe UI Variable via `walk.NewFont`; dark title bar via `DwmSetWindowAttribute(20)`; `SetWindowTheme("DarkMode_Explorer")` on TableView/ListBox/edits/scrollbars; `CellStyler` for row colours, group headers, hotkey text; dark `SetBackground` on windows/composites; `walk.MsgBox` → custom dialog. [A for API behaviour on Win10/11; walk hooks are V]
- **Cannot go dark without owner-draw / subclass:** GroupBox (frame + caption stay system colours), CheckBox/RadioButton labels (comctl v6 ignores `WM_CTLCOLORSTATIC` text colour with themes on), push buttons (need `BS_OWNERDRAW` or custom widget), `MsgBox`, native edit borders, NumberEdit spin buttons. Walk 2021 has no hook for `WM_CTLCOLOR*`/`WM_DRAWITEM` per widget, so each needs a custom `walk.CustomWidget` or `win.SetWindowLongPtr` subclass with `syscall.NewCallback` (callbacks are never freed → leak-by-design unless pooled).
- Not reachable: rounded cards, toggle switches, hotkey badges as real controls — only as `CustomWidget` paint code (GDI+/GDI; no anti-aliased rounded rects without extra work). Mica: `DWMWA_SYSTEMBACKDROP_TYPE` is Win11-only [A].
- Closeness to mockups: ~50–60%. Effort: main window 3–4 d, script edit dialog (29 KB, 8 GroupBoxes) 6–8 d, action/raw/hotkey/license/update dialogs 4–5 d, theme infra + toggle + persistence 2–3 d ⇒ **~15–20 pd**.
- Leak risk: **highest of the three in handle terms** — every custom brush/font/pen is a GDI handle that must be deleted (the overlay already does it manually, `overlay.go:139`); theme switching must recreate and dispose them; subclass callbacks accumulate.

## (b) WebView2 (Wails v2/v3 or go-webview2)

- Runtime: Evergreen WebView2 is preinstalled on Win11, **not guaranteed on Win10** → bootstrapper/offline installer or fallback needed [A]. Binary: Go side +~2–4 MB, embedded HTML/JS assets +0.5–3 MB; runtime itself is external (~150 MB+, separate processes `msedgewebview2.exe` ×5–6) [A].
- Closeness: ~95% (CSS gives rounded cards, toggles, badges, dark/light via `prefers-color-scheme`/CSS vars). Effort: frontend for all screens 12–18 pd (incl. tree/groups, action list drag-reorder, hotkey capture), Go bridge + state sync 4–6 pd, tray/overlay/updater glue 3–4 pd, installer/runtime check 1–2 pd ⇒ **~22–30 pd**, and a toolchain change (Node/Vite) in a `garble` build.
- Integration: IPC = JS↔Go bound functions/JSON events [A]. Hotkeys/hooks/executor stay as-is (own OS threads, `executor/input.go:329+`) — low risk. Overlay can stay the raw Win32 window (`overlay.go`). Tray: Wails v2 has no tray, so keep `walk.NotifyIcon` or a raw `Shell_NotifyIcon`; mixing walk's message loop with WebView2's requires a single STA UI thread — **this is the main architectural risk** [A]. Hotkey capture dialog (`script_edit_dialog.go:410,529` low-level hook callback) must be re-plumbed to JS.
- `garble -literals` + Wails reflection binding may break [A, untested]. SendInput/lock invariants unaffected as long as WebView2 isn't hosting the hook threads.
- Leak risk: moderate — Go handles drop, but a ~100–300 MB multi-process browser footprint is the new memory profile; JS listeners/event subscriptions can leak; WebView2 controller must be closed explicitly on exit [A].

## (c) Fyne or Gio

- Fyne: needs cgo + OpenGL (needs a C toolchain, breaks the current pure-Go `garble` cross-build from macOS; `-tags software` exists [A]); Gio: pure Go on Windows (D3D11) [A]. Binary +8–15 MB [A]. Look: Fyne is Material-ish, not Fluent; Gio is a blank canvas (rounded cards/toggles easy, but every widget — table, text input, IME, hotkey field — is hand-built).
- Tray: Fyne has `desktop.App` system tray [A]; Gio has none (`getlantern/systray` or keep walk's). Overlay: click-through layered topmost window is not exposed by either — would stay raw Win32 (`overlay.go`) [A]. Two UI toolkits' event loops (+ walk tray) in one process ⇒ high risk to overlay/tray; Gio/Fyne also own their window thread, interacting with the `LockOSThread` hook threads is safe but untested.
- Closeness: Gio ~85% after heavy work, Fyne ~50%. Effort: Gio **~30–40 pd** (custom widgets), Fyne ~15–20 pd but poor fidelity. Total rewrite of all 62 KB UI + 8 stubs (`*_stub.go`).
- Leak risk: low per-handle (GPU-resource managed by lib) but unknown; Fyne/Gio goroutine + GL-context lifetimes unverified [A].

## Comparison

| Criterion | (a) walk restyle | (b) WebView2 | (c) Gio (Fyne) |
|---|---|---|---|
| Closeness to mockups | ~55% (no rounded/toggles w/o custom paint) | ~95% | Gio ~85% (Fyne ~50%) |
| Effort (person-days) | 15–20 | 22–30 | 30–40 (Fyne 15–20) |
| New deps / runtime | none (still pure Go, no cgo) | WebView2 runtime (Win10 risk), Node toolchain, Wails/go-webview2 | Gio: none; Fyne: cgo+GL toolchain |
| Binary size (now 16.0 MB exe / 8.1 MB zip) | ≈ +0 | +3–6 MB, + external ~150 MB runtime | +8–15 MB |
| Risk to hotkey/SendInput/overlay/tray | Lowest (nothing moves) | Low for hotkey/SendInput; medium for tray/message-loop | Medium–high (overlay, tray, loop) |
| Leak risk | High GDI-handle/callback discipline needed | Medium (browser processes, JS listeners) | Low–medium, unverified |

## Recommendation: phased hybrid, start with (a)-lite, decide on (b) after a spike

1. **Phase 1 (≈6–8 pd, ships value, zero runtime risk):** theme infrastructure in walk — palette struct + persisted `light|dark|system` setting + toggle in settings; dark title bar, `DarkMode_Explorer` on TableView/ListBox/edits, Segoe UI Variable, `CellStyler` for group headers/hotkey text, owner-drawn replacement for the 4–5 worst offenders (buttons, GroupBox→flat Composite+Label, CheckBox). Centralise brush/font ownership in one `Theme` object disposed on switch (leak-safe, lines up with the memory audit).
2. **Phase 2 spike (2–3 pd):** WebView2 hosted *inside* the existing walk window for one screen (script list) to prove the single-UI-thread + tray + garble story and Win10 runtime detection. Proceed to full (b) only if the mockups' rounded cards/toggles are a hard requirement.
3. Reject (c): cgo (Fyne) or whole-UI hand-build (Gio) plus a second event loop colliding with walk's tray/overlay gives the worst effort/fidelity/risk ratio.

Caveat: if the mockups are non-negotiable (cards, toggles, badges as shown), (a) will plateau at ~55–60% and (b) is the only route to parity.
