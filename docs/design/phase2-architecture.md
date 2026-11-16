# Phase 2 architecture — WebView2 UI (spike result + DRAFT backend contract)

Base: `feat/modern-ui @ 328926a` (PR #1 clone/auto-active merged). PR #3 mechanisms are cited as `@3007ac4`. Status: **§9 contract ACCEPTED (with the amendments in §9.0); the rest is the spike result.** Spike code: `spike/` (own module). Windows runtime behaviour is **unproven locally** (no Windows here); it is covered by `spike/SMOKE.md` and `spike/ci/webview2-spike.yml`.

Legend: **[V]** verified here (build, run, or source read); **[D]** documentation only; **[A]** assumed, not verified; **[CI]** to be proven by the Windows runner.

## 1. Decision

**Option A, amended:** WebView2 via `github.com/jchv/go-webview2` (`pkg/edge` + `webviewloader`, *not* its top-level `webview.go`), with lxn/walk kept **only** as message pump, `NotifyIcon` tray and `Synchronize`. Two differences from the Lead's prior:

1. **The WebView2 lives in a raw top-level host window, not in a walk Form/child.** walk's pump calls `IsDialogMessage(fb.hWnd, msg)` for every message (`walk@c389da54/mainloop_default.go:35`), which handles Tab/Enter/Esc/arrows for any window under the form and would eat them before the page. A top-level window outside the form is not "the dialog or its child", so `IsDialogMessage` returns FALSE for it. [V source; behaviour CI/smoke step 7]
2. walk's `MainWindow` is created **hidden** and exists only as message owner for the `NotifyIcon` and `Synchronize` (`window.go:2079`). All visible UI is HTML.

The spike's "reopen" clause is **not triggered**: embedding does not force dropping walk. If walk is later dropped, the cost is ~40 lines of own pump + ~150 lines of `Shell_NotifyIcon`/`TrackPopupMenu` + 1 callback (tray window proc): ≈1–2 person-days. [A estimate]

### Criteria

| Criterion | A: jchv edge + walk pump (chosen) | B: Wails v2 | C: Wails v3 (alpha) |
|---|---|---|---|
| Pure-Go `CGO_ENABLED=0` | **[V]** `go vet` + `go build` windows/amd64, `go version -m` shows `CGO_ENABLED=0`; no `import "C"` in jchv or go-winloader. WebView2Loader.dll is loaded from memory via go-winloader (`webviewloader/module.go:139`) | [A] v2 uses a pure-Go go-webview2 fork on Windows; not built here | [A] not verified (docs 403) |
| `garble -literals` incl. working JS↔Go | **[V] build** `garble -literals build` exe OK. **[V] dispatcher parity**: `spike/cmd/rpcprobe` built plain vs garble on the host, 7 output lines byte-identical (including a deliberately untagged DTO and unicode). Windows exe JS→Go **[CI]** | **[D]** obfuscated builds replace name-based bindings by IDs; the app must use the generated `wailsjs` bindings, calling by name breaks (v2.wails.io/docs/guides/obfuscated). Works, but ties the frontend to generated files and a flag the build must keep in sync | [A] |
| Tray | **[V compile]** walk `NotifyIcon` (existing `ui/tray.go`, unchanged semantics) **[CI/smoke 5,9]** | **None in v2** (Lead's brief; [D]) → would need a second library + second loop | [A] has a tray; alpha, API churn; docs unreachable (403) |
| Overlay HWND on the UI thread | **[V compile + CI assert]** created in `newApp` on the locked main thread; self-test asserts `GetWindowThreadProcessId` equality, layered+topmost, ≥50 repaints | Wails owns the loop inside `wails.Run`; overlay would need creation inside its startup hook on the loop's thread — undocumented territory | [A] same concern |
| STA / pump rules | **[V source]** walk calls `OleInitialize` (STA) when the first window group is created (`windowgroup.go:109`); `edge.Chromium.Embed` runs a *bounded nested pump* until the controller exists (`chromium.go:95-110`), so it must be called on the UI thread **outside** any WebView2 callback | Wails owns it | Wails owns it |
| Fixed `NewCallback` count | **[V static]** see §3 (≤ ~110, constant) | Wails fork's `pkg/edge` alone registers 380 callbacks at init (`wailsapp/go-webview2@v1.0.23`, `NewComProc(` ×380) — fixed but fat, [V grep] | [A] |
| Embedded frontend | **[V]** `go:embed` + `NavigateToString` works in spike; **limit ~2 MB [A]** → production uses `AddWebResourceRequestedFilter` + `WebResourceRequestedCallback` (both exposed by jchv `chromium.go`) serving `embed.FS` from `https://app.local/` — **unproven**, first Phase-2 task | built in | built in |
| Runtime-missing detection | **[V compile, CI]** `webviewloader.GetInstalledVersion()` (`GetAvailableCoreWebView2BrowserVersionString`; ERROR_FILE_NOT_FOUND ⇒ `""`), plus `Embed` failing fast on a bad folder; native `MessageBox` Yes/No opens the Evergreen bootstrapper `https://go.microsoft.com/fwlink/p/?LinkId=2124703` | Wails shows its own prompt; not controllable | [A] |

**Rejected, with reasons.** B: no tray, owns the loop (conflicts with the hotkey/overlay/tray structure that must stay exactly as today), obfuscation coupled to generated bindings. C: alpha API, tray/loop semantics unverified (docs 403), same loop-ownership issue; revisit only if it stabilises. B and C were assessed from docs/module source, **not built**.

**Binding gaps in jchv `pkg/edge`** (workaround needed in Phase 2, owner = host peer): no `Controller.Close`, no `PostWebMessageAsJson` (the library echoes every message back with `PostWebMessageAsString`, `chromium.go:233-245` — harmless, the page must not listen to `chrome.webview` `message`), no virtual-host mapping, no `NewWindowRequested`/navigation lock. Options: small local patch via `replace` + `vendor`/fork (preferred), or add raw vtable calls in our glue. The Wails fork has more API at +352 init callbacks and is a drop-in fallback for `pkg/edge`.

## 2. Spike evidence

Pins used: Go **1.25.0** (`~/sdk/go1.25.0`, `GOTOOLCHAIN=local`; host default Go 1.26.5 is incompatible with garble 0.15), garble **v0.15.0**, rsrc **v0.10.2**, `github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808`, `go-winloader v0.0.0-20250406163304-c1995be93bd1`, `lxn/walk v0.0.0-20210112085537-c389da54e794`, `lxn/win v0.0.0-20210218163916-a377121e959e`, `x/sys v0.30.0` (same walk/win pins as root `go.mod:6-7`). Tools were installed into `$TMPDIR/supcua-tools/bin`.

| Build | Command (from `spike/build.sh`) | Result |
|---|---|---|
| plain | `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w"` | OK, `out/plain/spike.exe` **4,497,920 B**, PE32+ GUI x86-64 |
| garble | `GOGARBLE=spike garble -literals build -trimpath -ldflags "-H windowsgui -s -w"` | OK, `out/garble/spike.exe` **4,858,368 B**, PE32+ GUI x86-64 |

Both embed `rsrc.syso` (icon + the repo's comctl6/PerMonitorV2 manifest). Root `go.mod/go.sum` untouched. Garble findings: (a) `encoding/json` errors leak the **obfuscated Go type name** (`…into Go value of type uZhOV4B.IStscs`) — the dispatcher now returns fixed messages (`rpc/rpc.go:65,92`), after which output is identical; (b) the embedded `index.html` is **not** obfuscated (it is data) — garble does not protect the JS.

Self-test (`-selftest`, `selftest_windows.go`) covers: page+JS→Go, Go→JS events, tray create/remove, overlay UI-thread/style/50 repaints, 50 in-page dialog cycles with GDI/USER/handle deltas (tolerance 5/5/15), own-NewCallback constancy, hard watchdog (default 90 s → exit 2), JSON to stdout and `<exe>.selftest.json`. `-selftest-noruntime` points `WEBVIEW2_BROWSER_EXECUTABLE_FOLDER` at an empty dir, asserts detection = missing, prints the message instead of a MessageBox, and asserts `Embed` fails fast. All **[CI]**; none executed locally.

## 3. NewCallback budget (Go cap 2000, never freed)

| Source | Sites | Count | Per-open? |
|---|---|---|---|
| spike glue | `host_windows.go:58`, `overlay_windows.go:58` (via counting wrapper `callbacks_windows.go:14`) | 2 | no; asserted `[2,2]` before/after 50 cycles |
| jchv `pkg/edge` COM vtables | `NewComProc(` in package-level `var …Fn = …Vtbl{}` only (awk: 28/28 in `var` decls), `comproc.go:12` | 28 at init | no |
| jchv `webview.go:291` | `CreateWithOptions` | 0 (we do not use it) | would be per-window |
| walk | 76 inside `func init`, 1 at `window.go:557` guarded by `atomic.CompareAndSwapUint32` (`window.go:549`) | ≤77 | no |
| lxn/win `SetWinEventHook` (`user32.go:3348`) | function taking a callback | 0 (unused) | caller-controlled |
| **spike total** | | **≤ 107** | constant |

Production repo today (`grep NewCallback`, HEAD): `ui/overlay.go:61` (1, per overlay create), `ui/main_window.go:297` (1, hide subclass — **disappears**), `ui/script_edit_dialog.go:529` (**1 per editor open — leak**, fixed by PR #3 `registerEditHook` once-only, `script_edit_dialog.go:588-615 @3007ac4`), `executor/input.go:335,386` (per lock; PR #3 `hookCallbackCache` makes it once, `executor/hooklock.go:21-34 @3007ac4`). **Rule for Phase 2:** every `NewCallback` is behind `sync.Once` or package init; hooks/classes registered once; reviewers grep for it; the self-test counter guards the glue.

## 4. Threading and message-loop model

```
T_UI  main goroutine, runtime.LockOSThread in init(): STA (walk OleInitialize), walk pump
      owns: hidden walk MainWindow, NotifyIcon, host HWND + WebView2 controller, overlay HWND
T_HOTKEY   hotkey/manager.go:181 (LockOSThread + own pump)   unchanged
T_EXEHOOK  executor/input.go:329,380 (LockOSThread + pump)    unchanged; callbacks once-cached (PR #3)
T_CAPTURE  NEW, only while an editor capture is active: dedicated locked thread + pump (same
           runHook pattern as executor/hooklock.go) for the WH_KEYBOARD_LL hotkey/key capture
G_RPC      ONE worker goroutine executing every JS call (serialized mutations)
G_SAVE     ScriptStore single save worker, QueueSave/FlushSaves (storage/script_store.go:178,200 @3007ac4)
G_AUTO     autoactive watcher, 500 ms poll (autoactive/watcher.go:14); applies via Synchronize + generation check (main.go:208)
G_UPD      one-shot update check/download
```

Rules (each is a review item):

1. **Only T_UI touches COM/HWNDs**: `Eval`, `Navigate`, `Resize`, tray, overlay, host. Everything else reaches it with `mw.Synchronize(f)` (non-blocking post). `callUI` (post + wait with timeout) is allowed from G_RPC only, never from T_UI (`main_windows.go:callUI`).
2. **No nested modal loop in a WebView2 callback.** JS messages arrive in `MessageCallback` *inside* a COM event on T_UI; the callback only does `Dispatcher.Submit` (non-blocking enqueue, `rpc.go:89`, queue full ⇒ `BUSY`). No `MessageBox`, no walk `Dialog.Run`, no `Embed`, no `Sleep`. The native `MessageBox` is allowed only before windows exist (runtime missing, fatal startup) — it is the one place the pump is intentionally blocked.
3. **Dialogs become in-page modals.** Confirmations (delete, hotkey conflict, update) are DOM modals driven by promise-returning API calls.
4. **Replies/events**: worker → `Synchronize(Eval("window.__resolve(<json>)"))`. Payload is always `json.Marshal` output (escapes `<`, `>`, `&`, U+2028/9); never string-concat user data. Replace with `PostWebMessageAsJson` once the binding gap is closed.
5. **Hooks never run on T_UI.** `WH_KEYBOARD_LL` must answer within `LowLevelHooksTimeout`; T_UI can stall inside WebView2/GC. Capture uses T_CAPTURE.
6. `Embed` is called once, after `walk.NewMainWindow()` (STA init) and before `mw.Run()`; its nested pump dispatches walk messages harmlessly (the synchronize queue only drains in walk's `mainLoop`).
7. **Shutdown (UI-thread, ordered)**: emit `app.closing` → stop captures → `SetEditDialogOpen(false)` → `autoActive.Stop()` (main.go:259) → persist window/overlay pos via ConfigService → `scriptStore.FlushSaves(2s)` (main.go flush @3007ac4) → `hotkeyManager.UnregisterAll` (`hotkey/manager.go:166`) → dispose tray → destroy overlay → destroy host → `mw.Close()` (ends walk loop). Window **X exits**, **minimize hides to tray** — identical to `ui/main_window.go:299,308`; the spike's `hostProc` implements both (`host_windows.go:42-51`).
8. Hidden window memory: optional `TrySuspend` on hide **[A]**, measure in Phase 2.

## 5. One status dispatcher

Today `app.Controller.OnStatusChange` is a single callback (`app/controller.go:35,73,172,196`) set once in `main.go:151` and fanned out by hand to overlay/tray/main window. It runs on hotkey/executor goroutines. Replace with `status.Hub` (`spike/status/hub.go`, to move to `app/status`):

- `Publish(Snapshot)` called by Controller **outside** its mutex; equal snapshots are dropped; `Subscribe` replays the last value.
- Subscribers (all non-blocking, each marshals to T_UI itself): **frontend** (`Eval __emit('status', …)`), **tray** (tooltip + checkable items), **overlay** (`Refresh`), **hotkey reconcile** (`scriptHotkeySet.Reconcile`, `script_hotkeys.go:43 @3007ac4`, replaces the `prevActive` logic of main.go:149-165).
- `Snapshot` = `{globalActive, runningScriptId, runningScriptName, editDialogOpen, queueLength}`; running *name* replaces today's tray tooltip that prints the ID (`ui/tray.go:159-170`).
- Keep `OnStatusChange` temporarily as an adapter that calls `Publish`, so `app` tests keep compiling.

## 6. Ownership of shared files (one moving owner each)

| Artifact | Path (proposed) | Owner | Rule |
|---|---|---|---|
| DTOs + method/event names | `api/` (Go, json-tagged, no logic) | **Backend peer**; change = contract change → Lead | every field has an explicit `json` tag (garble) |
| Generated TS bindings | `frontend/src/api/generated.ts` from `go run ./cmd/gen-ts` | Backend peer generates; **nobody hand-edits** | CI diff-check |
| Frontend adapter | `frontend/src/api/client.ts` (`call()`, event bus, **mock adapter** for browser-only dev/tests) | **Frontend peer** | only file touching `window.chrome.webview` |
| Host glue | `ui/webview/` (host window, tray, overlay, runtime check, dispatcher registration, resource serving) | **Host peer** | only package importing walk/win/edge |
| `go.mod` / `go.sum` | root | **Lead-designated single owner** (dependency peer) | others send `DEPENDENCY_REQUEST`; adds jchv/go-webview2, go-winloader, bumps x/sys |
| `release.sh`, `scripts/build.sh` | root | Release peer | see §8 |
| `model/` fields (`theme`, `collapsed_groups`) | `model/config.go` | Backend peer, additive only | `omitempty`-safe defaults |

## 7. Frontend toolchain

**Vite + TypeScript + Svelte 5.** Rationale: compile-time reactivity (no VDOM runtime, ≈30 KB), good fit for form-heavy editor and drag-reorder lists, CSS variables for dark/light/system. Alternatives (React/Preact) acceptable if the team prefers; the contract is framework-neutral.
- Project-local npm only: `frontend/package.json` + committed `package-lock.json`, `engines.node` pinned, `npm ci --ignore-scripts`, `npx` resolves locally; no global installs. Tests: `vitest`, lint `eslint`, `svelte-check`.
- `vite build` → `frontend/dist` (gitignored except `.gitkeep`) → `//go:embed all:dist` in `ui/webview/assets.go`. Strict CSP meta (`default-src 'self'`), no remote fonts (Segoe UI Variable system font), DevTools/context menu off in release.
- Dev loop without Windows: `vite dev` with the mock adapter; Windows only for host work. Type contract drift is caught by the generated `generated.ts` diff-check.
- Not exercised in this spike (the spike page is dependency-free HTML); Vite/Svelte versions are chosen in Phase 2 [A].

## 8. `release.sh` build-only entry point

`release.sh` builds at lines ~28-33 then **tags, pushes, creates a GitHub release and uploads to MinIO** (`release.sh:35-51`). It must never run here. Proposal (owner: release peer):

1. Extract the build into `scripts/build.sh` (`frontend: npm ci && npm run build` → `rsrc -ico app.ico -manifest main.manifest -o rsrc.syso` → `garble -literals build -trimpath -ldflags="-H windowsgui -s -w -X …version.Version=$VERSION" -o "$OUT" .` with the repo's pins Go 1.25.0 / garble v0.15.0 / rsrc v0.10.2); prints `sha256`. Both `release.sh` and CI call it, so flags cannot drift (CI already mirrors them in `.github/actions/release-build` on `origin/ci/github-actions`).
2. `release.sh --build-only [--version X] [--out DIR]`: parsed **first**, before the `git` version/tag logic (`release.sh:~10-22`); sets `VERSION=${X:-dev}`; calls `scripts/build.sh`; `exit 0` before line 35. Refuse `--build-only` combined with any publish flag.
3. A test: `release.sh --build-only` run with `git`, `gh`, `aws` shadowed by failing stubs in `PATH` must exit 0 (proves no publish call). `spike/build.sh` is the template (no tag/push/upload paths).

## 9. API contract — ACCEPTED

### 9.0 Amendments and bridge interface

- `StateDTO` gains required `mouse_locked` / `keyboard_locked` (executor lock state). Run mode is `once|infinite`, hotkey mode `toggle|separate`. The host window uses the native frame.
- **D1** decided: autosave-on-close parity. `editor.close` returns `{saved, rev, reason?}` (UI shows "Saved" / "Not saved: <reason>"); an optional `script` param is the draft to autosave, otherwise the last draft whose `editor.save` failed. **D2** decided: `key_press_duration_ms` is restart-only (no `executor/` change).
- Collapsed groups go through `scripts.setGroupCollapsed` (config `collapsed_groups`); theme is config `theme`; both additive and `omitempty`-safe.
- Package `bridge/` (platform-neutral, no walk/win imports): `type Emitter interface{ Emit(event string, payload any) }`, `type UIThread interface{ Post(func()) }`, `type Handler = func(json.RawMessage) (any, error)`, `New(Deps, Emitter) *Bridge`, `(*Bridge).Methods() map[string]Handler`, `(*Bridge).Shutdown(timeout) error` (UI thread: `app.closing` → stop captures → `SetEditDialogOpen(false)` → `autoActive.Stop` → persist overlay position → `FlushSaves`). `Deps` holds `Controller`, `Store`, `Config`, `Hotkeys`, `AutoActive`, `Locks`, `Overlay`, `UI`, `Capture` (platform hook behind `CaptureBackend`) and `Host` (hide/show/exit, clipboard, boot info, hotkey rebind, update). Errors are `*bridge.Error{code, message, field?, conflicts?}`. Host-facing helpers: `NotifyBlur`, `NotifyStateChanged`, `NotifyUpdateAvailable`, `NavGoto`, `Reload`. Handlers run only on the host's single RPC worker; the status Hub (`app/status`, `Controller.Subscribe`) may call the state subscriber from any goroutine.


Transport (`spike/rpc/rpc.go`): JS → `chrome.webview.postMessage(JSON.stringify({id, method, params}))`; Go → `window.__resolve({id, ok, result|error})`; events → `window.__emit(name, payload)`. Methods are looked up by **string key in a map**, params decoded into explicit json-tagged structs — no reflection by name, so garble-safe [V]. One worker serializes all calls; queue 64, overflow ⇒ `BUSY`.

**Errors** `{code, message}` (messages are fixed strings, never Go error text/type names): `BAD_REQUEST`, `UNKNOWN_METHOD`, `BUSY`, `INTERNAL`, `NOT_FOUND`, `NAME_EMPTY`, `NAME_TAKEN`, `HOTKEY_CONFLICT{conflicts[]}`, `HOTKEY_REGISTER_FAILED`, `EDITOR_BUSY`, `EDITOR_CLOSED`, `SAVE_FAILED`, `INVALID_SETTING{field}`, `UPDATE_FAILED`, `NOT_LICENSED`.

**Shutdown and session additions.** `BeginShutdown(deadline, then)` (UI thread, non-blocking) rejects new mutating RPCs except `editor.save`, `editor.close` and the new `app.closingReady` (no params), emits `app.closing {deadline_ms}`, waits off the UI thread for `app.closingReady` or the deadline, then posts `then` to the UI thread; `then` calls the synchronous `Shutdown(timeout)`, which fences in-flight RPC work (later mutations get `BUSY`), clears editor-open, stops auto-active, persists the overlay position and flushes saves. `ResetSession(reason)` (any goroutine, idempotent; host on ProcessFailed/navigation) ends captures, drops the editor session and draft and re-enables execution. Params are strict: objects only, unknown fields/null rejected, required keys must be present; `editor.close.autosave` defaults to true. Script hotkeys must use the `hotkey.Manager` key vocabulary (modifiers de-duplicated and ordered). A save that does not flush in time is `SAVE_FAILED`.

### 9.1 Methods

| Method | Params → Result | Replaces / source |
|---|---|---|
| `app.getBootState` | → `{mode:"main"\|"notLicensed", version, macs[], runtimeVersion}` | license gate `main.go:51-59`; `ui/license_dialog.go:11-36` |
| `app.ready` | → `{}` (page loaded; Go then pushes initial `state`) | new |
| `state.get` | → `StateDTO` (below) | `ui/main_window.go:56-68` active sync |
| `app.setGlobalActive` | `{active}` → `StateDTO` | "Global Active" checkbox `main_window.go:221-229`; tray Toggle (`tray.go:70-76`) |
| `app.exit` | → `{}` (runs §4.7 ordered shutdown) | tray Exit `tray.go:109-114` |
| `window.hide` / `window.show` | → `{}` | minimize/tray `main_window.go:299`, `tray.go:119-125` |
| `window.reportBounds` | `{x,y,w,h}` (debounced by frontend/host; host also saves on close) | `main_window.go:274-313` pos restore/save |
| `scripts.list` | → `{rev, scripts:ScriptSummary[], groups:GroupDTO[]}` (sorted as `main_window.go:359-372`: ungrouped first, then group, then name, case-insensitive) | `refreshScriptList :352` |
| `scripts.get` | `{id}` → `Script` (full model) | `onEditScript :496` |
| `scripts.setEnabled` | `{id, enabled}` → `{rev}`; one `QueueSave(changed)` (last-wins) | `scriptTableModel.SetChecked :139-177` |
| `scripts.setGroupEnabled` | `{group, enabled}` → `{rev, changed:n}`; **one** `QueueSave(changed...)`, never a goroutine per script | anti-pattern v1.0.28 `main_window.go:146-151` |
| `scripts.setGroupCollapsed` | `{group, collapsed}` → `{}` persisted in config `collapsed_groups` (new) | new (mockups) |
| `scripts.clone` | `{id}` → `ScriptSummary` (`model.CloneScript`, `store.NameOrFileTaken`) | `onCloneScript main_window.go:530`; `model/clone.go` |
| `scripts.delete` | `{id}` → `{rev}` (confirm is an in-page modal; store cancels queued saves, `b46a5db`) | `onDeleteScript :551` |
| `editor.open` | `{id\|null}` → `{sessionId, script, catalog}`; sets `SetEditDialogOpen(true)` (stops the running script); second open ⇒ `EDITOR_BUSY` | `onNewScript/onEditScript :465-523`; `app/controller.go:85-93` |
| `editor.save` | `{sessionId, script, overrideConflicts?}` → `{script, rev}` or `NAME_EMPTY`/`NAME_TAKEN`/`HOTKEY_CONFLICT{conflicts:[{scriptId,name,field,hotkey}]}`; with `overrideConflicts` the conflicting hotkeys are cleared in other scripts and **all changed scripts go in one serialized `QueueSave`/`Save` pass** | `onSave script_edit_dialog.go:708-750`; `hotkey.ConflictChecker` |
| `editor.close` | `{sessionId, autosave}` → `{saved, rev}`; **always** releases session, stops captures, `SetEditDialogOpen(false)`. Default `autosave:true` = parity: closing saves when name is non-empty and unique (`autoSave :752-760`); there is no Cancel today — **decision D1** below | `Closing → autoSave`; `main_window.go:481-487` |
| `editor.describeActions` | `{actions}` → `string[]` (labels single-sourced in Go: `describeAction :891-948`, incl. loop/if_image child counts) | action list rendering |
| `actions.catalog` | → `[{type, group:mouse\|keyboard\|wait\|loop\|lock\|image, params:[{name,kind,min,max,default}]}]` (types from `model/action.go:4-27`; param sets from `action_dialog.go:24-100`: mouse_move x,y · key_* key · delay ms · loop count (0=∞) · wait_for_image timeout_ms,use_found_position · if_image use_found_position; the rest parameterless) | action buttons + `ShowActionParamDialog` |
| `editor.toJSON` / `editor.fromJSON` | `{script}`→`{text}` / `{text}`→`{script}` or `BAD_REQUEST` | raw editor — **dead code today** (`ui/raw_editor.go`, never constructed); optional, include only if the UI wants an "Advanced JSON" tab |
| `capture.hotkey.start` | `{sessionId, field:"toggle"\|"start"\|"stop"}` → `{}` | `attachHotkeyCaptures :506-600` |
| `capture.key.start` | `{sessionId}` → `{}` (single key for the action Key field; Tab captured, not focus-moving) | `keyEdit` capture `:550-560` |
| `capture.stop` | `{sessionId}` → `{}` | `Closing` hook removal |
| `capture.mouse.start` / `capture.mouse.stop` | `{sessionId}` → `{}` | `registerPickHotkey :420-457` |
| `clipboard.write` | `{text}` → `{}` (native clipboard; `navigator.clipboard` is not reliable on the app origin [A]) | license "Copy" `license_dialog.go:30-32` |
| `settings.get` | → `SettingsDTO` | new Settings page |
| `settings.update` | `{patch}` → `{applied:string[], restartRequired:string[], state}`; atomic per call, rollback on failure | new; see 9.4 |
| `overlay.set` | `{enabled?, visible?, draggable?}` → `OverlayDTO` (UI-thread ops via `callUI`) | tray items `tray.go:85-104`, overlay hotkey `main.go:174-181` |
| `autoactive.set` | `{enabled?, processName?}` → `{enabled, processName}` | tray `AddAutoActive*` `tray.go:119-145`, `main.go:219-245`, dialog `ui/auto_active_dialog.go` |
| `update.apply` / `update.skip` | `{}` → `{}`; apply runs on a worker (today the download blocks the UI thread inside `Synchronize`, `main.go:103-110`), progress via events | `ui/update_dialog.go:14-40` |
| `scripts.retrySave` | `{id}` → `{}` | save-failure recovery |

### 9.2 Events (Go → JS)

`state` (the Hub snapshot, §5) · `scripts.changed {rev}` (any external/bulk change; frontend refetches `scripts.list`) · `settings.changed {fields[]}` · `capture.hotkey {field, hotkey:Hotkey\|null, display}` · `capture.key {key}` · `capture.mouse {x,y}` · `capture.ended {reason: stopped\|blur\|timeout\|editorClosed}` · `update.available {tag}` · `update.progress {pct}` · `update.failed {message}` · `save.failed {scriptId, name, message}` (from `ScriptStore.SetOnSaveError`) · `nav.goto {route}` (tray "Auto Active Process…" → Settings) · `app.closing`.

### 9.3 DTOs (every field tagged; sketch — the table below is authoritative for the types `frontend/src/api/types.ts` defines)

| DTO | JSON fields (snake_case unless noted) |
|---|---|
| `ScriptSummary` | `id, name, group, run_mode, hotkey_mode, hotkey_label, enabled, running` |
| `GroupDTO` | `name, count, all_enabled, collapsed` |
| `ScriptList` | `rev, scripts[], groups[]` |
| `SettingsDTO` | `theme, global_hotkey, capture_hotkey, overlay_hotkey` (display strings, e.g. `Ctrl + O`), `overlay_enabled, key_press_duration_ms, scripts_dir, auto_active_enabled, auto_active_process_name, collapsed_groups[]` |
| `SettingsUpdateResult` | `applied[], restartRequired[], state` (camelCase) |
| `EditorSession` / `HotkeyConflict` / `BootState` | camelCase: `sessionId, script, catalog` / `scriptId, name, field, hotkey` / `mode, version, macs, runtimeVersion` |
| `ActionSpec` / `ParamSpec` | `type, group, params[]` / `name, kind, min?, max?, default?` (param `name` = Action JSON tag) |

Fixtures in `frontend/src/api/fixtures/*.json` are decoded strictly into the Go DTOs by `bridge/bridge_test.go`.

```go
type StateDTO struct {
  GlobalActive bool `json:"global_active"`; RunningID, RunningName string `json:"running_id","running_name"`
  EditorOpen bool `json:"editor_open"`; Overlay OverlayDTO `json:"overlay"`
  Theme string `json:"theme"` // light|dark|system
  AutoActive struct{ Enabled bool `json:"enabled"`; ProcessName string `json:"process_name"` } `json:"auto_active"`
}
type ScriptSummary struct { ID, Name, Group, RunMode, HotkeyMode, HotkeyLabel string; Enabled, Running bool } // label = formatScriptHotkeys
type GroupDTO struct { Name string; Count int; AllEnabled, Collapsed bool }
type OverlayDTO struct { Enabled, Visible, Draggable bool; X, Y int }
```
`Script`/`Action`/`Hotkey`/`Region` are the existing model types (`model/script.go:12-22`, `action.go:46-62`, `hotkey.go:5`) — already json-tagged; the wire format of script files does **not** change.

### 9.4 Settings: immediate vs restart

| Setting (config field) | Applies | Mechanism / reason |
|---|---|---|
| `theme` light\|dark\|system (new `theme`) | immediate | CSS variables + `prefers-color-scheme`; persisted via ConfigService |
| groups collapsed (new `collapsed_groups`) | immediate | persisted |
| `global_hotkey` | immediate | unregister old id + `Register` (today registered once, `main.go:165-168`); on failure restore old and return `HOTKEY_REGISTER_FAILED` |
| `overlay_hotkey`, `overlay_enabled` | immediate | re-register (`main.go:174-181`); create/destroy overlay on T_UI (window class and its callback registered once) |
| overlay visible / draggable / position | immediate | `overlay.go:156-185`; position saved debounced + on exit (`main.go:~255-262`) |
| `capture_hotkey` (mouse P key) | immediate | capture manager reads it atomically (today a startup constant, `main.go:87`) |
| `auto_active_enabled`, `auto_active_process_name` | immediate | `Watcher.Start/Stop/SetProcessName` (`main.go:219-245`) |
| `key_press_duration_ms` | **restart** in v1 (executor receives it at construction, `main.go:80` → `executor/executor.go:49`); becomes immediate if Backend adds an atomic `Executor.SetKeyPressDuration` — flagged **D2** |
| `scripts_dir` | **restart** (store bound at `main.go:71-74`, hotkeys derived from it); validated (exists/creatable, writable) before saving, `INVALID_SETTING` otherwise; a later `scripts.reload` could lift this |
| `main_window_pos`, `edit_dialog_pos` | internal; `edit_dialog_pos` obsolete (in-page modal) but kept in the file |

### 9.5 Capture lifetime and bounds

- State is one struct in a `CaptureManager`: `{sessionId, mode: none|hotkey|key|mouse, field, deadline}` — **no per-dialog goroutines, no snapshots**. One reusable mouse-poll ticker (50 ms, as `:420`) runs only while `mouse` capture is on.
- Keyboard capture installs the hook **once per process** through the once-only shared callback (`registerEditHook`, keyed by HWND, `script_edit_dialog.go:588-623 @3007ac4`), re-pointed at the **host HWND**; foreground check = host window is foreground (parity `:535`); Alt is swallowed (parity `:540`); runs on T_CAPTURE.
- Cancellation: `capture.stop`, `editor.close`, host loses activation (WM_ACTIVATE inactive), 60 s idle deadline, app exit. Each ends with `capture.ended{reason}` and removes the hook handle.
- Mouse capture ignores the key when a text input has focus: the frontend drops `capture.mouse` events while an `<input>`/`<textarea>` is focused (parity `:443-446`).
- Editor-open execution suppression: `editor.open` → `SetEditDialogOpen(true)` (also stops the running script); every close path → `false`; if the page crashes/unloads (`NavigationCompleted`/`ProcessFailed`/`app.closing`) the host forces `false` so scripts never stay blocked.

### 9.6 Storage rules (binding for all peers)

1. All mutations go through G_RPC (serialized) and the `ScriptStore`; there is exactly **one writer** — the store's save worker (`QueueSave`, last state per ID wins, writes never overlap, `@3007ac4`). No goroutine per script; no `go store.Save(s)` (v1.0.28 `main_window.go:146-151`).
2. Save only **changed** scripts (diff by ID/`reflect.DeepEqual` on the model, or explicit changed list), sequentially, in one `QueueSave(changed...)` call; hotkey-conflict overrides join the same call. `Delete` cancels queued saves (`b46a5db`).
3. Config goes through one `ConfigService.Update(func(*AppConfig))` under a mutex (today three call sites do load-modify-save: `main.go:211-217`, window close `main_window.go:308-316`, edit dialog pos `script_edit_dialog.go:~340`).
4. No retention of closed-editor state: the session holds the draft only until `editor.close`; Go keeps no copy of the previous script list per dialog; frontend drops the draft on close.
5. Save failures: synchronous API calls return `SAVE_FAILED`; background `QueueSave` failures surface via `save.failed` (store gets an `OnSaveError` hook, Backend peer) with `scripts.retrySave`; the UI keeps the unsaved toggle visibly "unsaved" until acknowledged.
6. Script hotkeys: `scriptHotkeySet.SetScripts/Reconcile` under its lock (`script_hotkeys.go @3007ac4`); the RPC layer calls `SetScripts(list)` after each mutation — never register from the UI.
7. Exit: `FlushSaves(2s)` is mandatory before process exit.

### 9.7 Decisions needed from Lead/Human

- **D1** (decided, §9.0) Editor semantics: keep autosave-on-close (parity) or add explicit Cancel (changes behaviour; recommend parity + a visible "Saved"/"Not saved: name taken" indicator).
- **D2** (decided: restart-only) Make `key_press_duration_ms` live (small executor change) or restart-only.
- **D3** Adopt a local patch/fork of jchv `pkg/edge` (Close, PostWebMessageAsJson, resource serving) vs the Wails fork (+352 init callbacks).

## 10. Unproven / residual risks

1. Nothing was executed on Windows. Highest-value checks: smoke 7 (keyboard delivery with a raw host window), self-test resource flatness, tray in `-selftest`, `-selftest-noruntime` detection (the env override is honoured by our own folder check, `runtime_windows.go:20-25`, **not proven** to be honoured by `GetAvailableCoreWebView2BrowserVersionString`; `Embed` failure step proves the loader side).
2. Production resource serving (`WebResourceRequested` + `embed.FS`) is unbuilt; fallback is `NavigateToString` (~2 MB cap [A]).
3. `Embed` nested pump runs once at startup; a runtime that never answers would hang there — the self-test watchdog covers CI, production needs a startup timeout/`BLOCKED` UI (Phase 2).
4. Wails v2/v3 were judged from docs/source only; v3 docs were unreachable (HTTP 403).
5. WebView2 footprint (~100-300 MB multi-process, feasibility.md) and Win10-without-runtime UX are product risks, not engineering ones; installer link flow is specified in §1.
6. garble: only the dispatcher/DTO layer is proven obfuscation-safe (native probe); the Windows garble exe is proven to build, not to run [CI].
7. Mouse coordinates and the overlay under mixed-DPI (manifest PerMonitorV2 retained): untested.
