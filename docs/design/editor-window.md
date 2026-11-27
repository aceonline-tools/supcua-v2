# Editor window — design contract (v1.6.0)

Status: **proposed contract (E1a.1)**. Frozen once the Lead accepts it. Later changes need a REOPEN to the Lead.
Base: `origin/main` a507e2f. Spike evidence: `origin/spike/editor-window-cost` (throwaway, not merged).

## 1. Scope and non-goals

New Script / Edit open the editor in a separate native, modal, resizable window owned by the main window.
Not in scope: `ui/webview/serve*` (a concurrent PR renames `app.local` → `app.invalid`; the editor URL below uses the *current* serve host constant, never a literal), a multi-editor mode, any change to the save/conflict semantics of `bridge/editor.go`.

Spike findings treated as facts: a second controller from the **same environment** works; it stays white without the `WM_SHOWWINDOW` Reassert (`presentSurface`); no new `NewCallback`/`NewComProc` sites (reuse `hostProc` through the same class plus package-level vtables); about +35 MB private / +95 MB WS while open; `EnableWindow` modality works.

## 2. Architecture

### 2.1 Process host and Windows (replaces `current atomic.Pointer[UI]`)

```
host (process singleton, UI thread owned)
  env          *edge.Environment        // ONE, shared
  windows      map[win.HWND]*Window     // UI-thread only; replaces `current`
  disp         *rpc.Dispatcher          // ONE, shared, serial
Window { role main|editor; gen uint64; hwnd; browser; navGate; lockdown; resFilter; bootstrapJS;
         owner *Window; state; closedFlag atomic; sink rpc.Sink }
```

- `hostProc` looks up `windows[hwnd]` (a lock-free read on the UI thread, since all mutation is on the UI thread). Unknown hwnd → `DefWindowProc`. Same class, same proc, per-role behaviour switched on `Window.role`.
- Per-window: `navGate` (allowlist is per role: the editor page may navigate only to its own `index.html?window=editor&gen=…` origin), `ProcessFailed` handler, `Lockdown` (devtools/context-menu/popup denial), `WebResourceRequested` filter, bootstrap script (injects `window.__role` and `window.__gen` before page scripts so the page cannot be asked to guess).
- `WM_SHOWWINDOW(wp!=0)` → `presentSurface("shown")` for **both** windows.
- `u.cfg.OnActivate`, `OnClose`, min-size, `reportBounds` become per-role callbacks on `Window`. `Quarantine`, `Reload`, `ProbeNavigationLock`, `Stats` stay main-only API on `host`; counters are per window and summed in `Stats()`.

### 2.2 Non-blocking embed

No nested `EmbedTimeout` message pump inside RPC jobs. `editor.openWindow` (a worker job) only **posts** `createEditorWindow(gen, scriptID|nil)` to the UI thread and returns `{status:"opening", gen}` immediately. On the UI thread: create hidden owned window → start async `CreateCoreWebView2Controller` against the shared env with a completion callback and a 10 s startup timer (`WM_TIMER` on the editor hwnd). Completion callback finishes setup (navGate, lockdown, filters, bootstrap, `Navigate`) on the UI thread. Timer expiry or `E_FAIL` → startup failure rollback (§4.4). The existing main-window blocking `EmbedTimeout` path is unchanged.

### 2.3 One shared serial Dispatcher with request-local reply sinks

`rpc.Dispatcher` keeps one worker (the bridge assumes serial execution) and loses the single reply fixed at `Start`.

```go
type Reply func([]byte)                    // must not block
func (d *Dispatcher) Start()               // no reply arg
func (d *Dispatcher) Submit(raw string, reply Reply)
```

- `reply` is carried in the queue `item` and used on **every** path: malformed JSON (BAD_REQUEST), BUSY, handler error, success. Today's `d.reply` uses are removed.
- Each Window builds `reply` as `func(b []byte){ if !w.closedFlag.Load() { w.postFrame(b) } }` (`postFrame` posts to the UI thread; it re-checks `windows[hwnd].gen == w.gen` there). A reply for a closed or superseded window is dropped silently; the handler's side effects are not undone.
- Closing the editor never calls `Dispatcher.Close()`. Only app shutdown does.
- Queue limit remains global (`DefaultQueue`). BUSY is answered to the submitting window only.
- Per-window caller identity: `Submit` also gets `Caller{Role, Gen}` (struct attached to `item`) and handlers that need it (`editor.*`, `capture.*`, `app.closingReady`) receive it via `rpc.CallerFrom(ctx)`; the handler signature gains `ctx context.Context` only for those registered through `RegisterCtx`, so existing handlers are untouched.

### 2.4 Events routing

`bridge.emit(event, payload)` gains a target: `emitTo(Target, …)` with `Target ∈ {Main, Editor, Both}`. The host fans out per window sink; an event for an absent window is dropped.

| Event | Target | Notes |
|---|---|---|
| `state`, `scripts.changed`, `settings.changed`, `save.failed`, `update.*`, `nav.goto` | `state`, `scripts.changed`, `settings.changed`, `save.failed`: **Both**; `update.*`, `nav.goto`: **Main** | `save.failed` is needed by the editor toast/indicator |
| `editor.reset` | Editor (plus Main for the toast text) | carries `gen` |
| `editor.closeRequested` (new) | Editor | §3 |
| `editor.windowFailed` (new) | Main | startup failure toast |
| `editor.windowClosed` (new) | Main | after destroy, `{gen, saved, forced, reason?}`; main may refresh |
| `capture.hotkey`, `capture.key`, `capture.mouse`, `capture.ended` | Editor | session-bound |
| `app.closing` | **Both** | readiness aggregation §7 |

## 3. RPC contract (exact)

All types are the existing wire conventions (`{id, method, params}` → `{id, result|error}`; error codes in `bridge/errors.go`). New codes: `EDITOR_WINDOW_BUSY` (`window not ready`), `STALE_GEN`, `DRAFT_TOO_LARGE`, `DRAFT_STALE` (sequence not newer). `Role` is host-derived and cannot be forged by the page.

| Method | Caller | Params | Result |
|---|---|---|---|
| `editor.openWindow` | main | `{id: string\|null}` | `{status: "opening"\|"focused", gen: number}`. Existing editor window (any state ≥ `Creating`) → focus it, return `focused` (no new session, even if `id` differs; main shows the toast "Editor already open"). Rejected during shutdown (`SHUTTING_DOWN`). |
| `editor.windowReady` | editor | `{gen}` | `{}` — page loaded, session open is next. Informational (arms the 10 s load guard release). |
| `editor.measured` | editor | `{gen, token, cssW, cssH, dpr}` ; `cssW,cssH` integers CSS px, `1 ≤ v ≤ 8192` | `{action: "show"\|"remeasure", cssW?: number}` — the host answers via the **reply** so a late call is naturally dropped. `remeasure` carries the clamped CSS width to lay out at. |
| `editor.open` (existing) | editor | `{id: string\|null}` — **now sent by the editor page** using `gen`-bound query data, not by main | `EditorSession` unchanged, plus `gen`. Rejects `STALE_GEN`. Session creation = `SetEditDialogOpen(true)` as today. |
| `editor.checkpoint` (new) | editor | `{sessionId, gen, seq: number, script: Script}` | `{seq}` — see §6 |
| `editor.save` (existing) | editor | unchanged `+ gen` | unchanged; rejected after cleanup with `EDITOR_CLOSED` |
| `editor.requestClose` (new) | editor | `{sessionId, gen, outcome: "saved"\|"discarded"\|"failed", draftSeq?: number}` | `{}` — page tells host its guard is satisfied; host then runs close §4.3. Replaces the frontend's call to `editor.close` for the window path. |
| `editor.close` (existing) | editor | unchanged | unchanged semantics (releases session, ends capture, re-enables execution); used internally by the host for watchdog autosave and by shutdown. Idempotent: second call → `EDITOR_CLOSED`, harmless. |
| `editor.windowAction` (new) | editor | `{gen, action: "focusMain"}` — reserved, not implemented in v1.6.0 | — |
| `app.closingReady` (existing) | main or editor | `{gen?}` | `{}` — records readiness for the caller's role (§7). |

Everything else (`editor.describe/toJSON/fromJSON`, `capture.*Start/Stop`, `settings.*`) stays callable by the editor window; main-only methods (`scripts.*`, `settings.update`, `overlay.*`, `update.*`, …) are rejected from the editor role with `FORBIDDEN_ROLE`, and `editor.*`/`capture.*` from main with `FORBIDDEN_ROLE` except `editor.openWindow`, `app.closingReady`.

Event payloads (typed):

```ts
editor.closeRequested {gen:number, reason:"user"|"app-closing"|"owner-closing"}
editor.windowFailed   {gen:number, stage:"create"|"embed"|"timeout"|"navigate"|"measure", message:string}
editor.windowClosed   {gen:number, saved:boolean, forced:boolean, reason?:string}
editor.reset          {reason:string, sessionId:string, gen:number}     // gen added
app.closing           {deadline_ms:number, roles:("main"|"editor")[]}   // roles = who must ack
```

Frontend event validation (`frontend/src/api/validate.ts`) and `bridge/dto.go` + `bridge_test.go` round-trip tables must list all new events/methods (existing tests enforce this).

## 4. Window/session state machine

### 4.1 States (UI thread owns `Window.state`; worker owns `session`)

```
        openWindow
  None ───────────► Creating ──embed ok──► Loading ──windowReady──► Measuring ──show──► Open
                       │  fail/10s            │ 10s load guard         │ 5s measure guard   │
                       ▼                      ▼                        ▼                    │ WM_CLOSE / app.closing
                    Rollback ◄────────────────┴────────────────────────┘                    ▼
                       │                                                               CloseRequested
                       ▼                                                                    │ requestClose ok │ 10s watchdog
                     None ◄──── Destroyed ◄──── Closing ◄───────────────────────────────────┴─────────────────┘
```

- **Creating** (hidden, owned, not enabled-modal yet; `EnableWindow(main,FALSE)` is executed on entry to `Creating` *before* the window can be shown/interactive). 10 s guard.
- **Loading**: navigated to `index.html?window=editor&gen=<token>` (`token` = decimal gen, plus per-window random nonce embedded in bootstrap; query alone is not trusted). 10 s guard from `Creating` covers both.
- **Measuring**: page ran `editor.open`, rendered, reports `editor.measured`. 5 s guard (failure → fallback size §5.4, not rollback, so a stuck measurer still gives a usable window).
- **Open**: visible and interactive.
- **CloseRequested**: first `WM_CLOSE`/`app.closing`; `editor.closeRequested` emitted; watchdog started (§4.5). Repeated requests do not emit again nor reset the timer.
- **Closing**: `EnableWindow(main,TRUE)` → `DestroyWindow(editor)` → `SetForegroundWindow(main)`; session cleanup; `editor.windowClosed` to main.
- Only one editor: `openWindow` in any state except `None` → focus (hidden states: ensure it is shown when it reaches `Open`).

### 4.2 Modality order (acceptance-critical)

1. On `Creating`: `SetEditDialogOpen` stays false until the session opens, but `EnableWindow(mainHWND, FALSE)` first; then create/show the editor. Main is never interactive while the editor can be.
2. On close: `EnableWindow(mainHWND, TRUE)` **before** `DestroyWindow(editor)` (otherwise Windows activates another app). After destroy, `SetForegroundWindow(main)` (and `SetFocus` on the browser). If main is minimised/hidden to tray, restore per §8 instead.
3. All of this is one UI-thread function `endModal()`; it is idempotent (`windows[hwnd]` presence + `modalHeld` flag).

### 4.3 Close flow

`WM_CLOSE` on the editor (X, Alt-F4, system menu) → `hostProc` returns 0, **does not destroy**, sets `CloseRequested`, emits `editor.closeRequested{reason:"user"}`. The page runs the existing guard (`editor.svelte.ts close()` incl. conflict dialog) and, when the result is `closed`, calls `editor.requestClose{outcome}`. The host then (worker job) calls the internal `editor.close` with `autosave` as required (draft already saved → `false`), and posts `endModal()+Destroy` to the UI thread. If the guard leaves the editor open (conflict dialog, user cancels) the watchdog is still counting (§4.5) — see Open Question Q2.

### 4.4 Startup failure rollback

Any failure in `Creating/Loading/Measuring` hard failures (embed error, 10 s timeout, `ProcessFailed` before `Open`, navigation denied): `endModal()` (re-enable main), `SetEditDialogOpen(false)`, drop the session (`ResetSession("window-failed")`), destroy the window, bump the generation, emit `editor.windowFailed` to main (main shows a toast with `message`). Idempotent; a late completion callback for a rolled-back gen finds no window and releases its controller.

### 4.5 Watchdog

Starts **only** on the first close request (`CloseRequested` entry), **not at open**; 10 s; **not reset** by repeated requests, nor by `editor.checkpoint`. On expiry (UI-thread `WM_TIMER`) → `forceClose` job on the worker:

1. `recovered := bridge.RecoverDraft(sessionID)` — the latest accepted checkpoint (§6), plus the existing `sess.draft`, taking the higher `seq`.
2. If a draft exists and validates → `saveScript` (same path as normal; conflicts resolved as "keep my edit" is **not** assumed: a hotkey conflict makes the autosave fail → stored as recoverable draft, §6.4). Result `{saved, reason}`.
3. `editor.close` internals (end capture, release session), `endModal()`, destroy (browser `Close()` first, bounded 1 s, then `DestroyWindow`), `editor.windowClosed{forced:true}`.

A renderer that responds in time cancels the watchdog (`requestClose` received).

### 4.6 Generation tokens

`gen` (monotonic `uint64`) is assigned at `openWindow`, stored on `Window` and on `editorSession`. Every editor→host call carries `gen`; the host rejects `gen != current` with `STALE_GEN`. Replies and events are delivered through a sink that re-checks `windows[hwnd].gen`. Native callbacks (controller completion, `ProcessFailed`, `WM_TIMER`, `NavigationStarting`) capture `gen` and no-op if it no longer matches. A replacement editor therefore cannot be affected by the old one. Late `editor.save`/`checkpoint`/`closingReady` after cleanup → `EDITOR_CLOSED`/`STALE_GEN`, no side effect.

## 5. Size measurement handshake

### 5.1 Principle

Default size = the editor's **intrinsic natural size at compact density**, measured in the page, converted by the host. No hard-coded pixel sizes except the fallback (§5.4).

### 5.2 Page side (E2)

- Measurement mode: when `?window=editor`, before reporting, the page puts `<html data-measure>` which switches `.editor-main` from `height:100vh` + `1fr` column to **intrinsic layout** (`height:auto; width:max-content`-based grid, `min-width:0` overrides removed, actions pane `max-height: var(--actions-pref-h)` = fixed preferred bounded height, e.g. a design token of 360 px at compact density), independent of the current viewport.
- Compact density is forced during measurement (the same density the editor shows by default).
- Measure after: session loaded and rendered, `await document.fonts.ready`, two `requestAnimationFrame`s, no pending images/icons. Take `scrollWidth/scrollHeight` of the measurement root (or `getBoundingClientRect` of `.editor-main`), `Math.ceil`.
- Report `editor.measured{gen, token, cssW, cssH, dpr: devicePixelRatio}`.
- On `{action:"remeasure", cssW}` → set the measurement root width to `cssW`, wait 2 rAF, re-measure (text wraps, height grows) and report again. At most 2 remeasures; the third answer is forced `show`.
- On `{action:"show"}` → remove `data-measure` (restore viewport-driven layout: `100vh`, `1fr`), after which the page is responsive to resize. The `data-measure` ↔ viewport switch must not itself change the window size.

### 5.3 Host side (E1a.2/E1b)

```
host: shown? no (hidden)
page → host   editor.measured{cssW,cssH,dpr}
host:  scale = GetDpiForWindow(editor)/96              (real DPI, not dpr alone; mismatch logged)
       clientPx = ceil(css*scale)
       frame    = AdjustWindowRectExForDpi(WS_OVERLAPPEDWINDOW… , dpi) → outer = client + frame
       wa       = work area of monitor chosen by (remembered bounds ▸ owner's monitor)
       outer    = clamp(outer, min=scaledMin, max=wa)         // tiny work area: fit wa, min yields
       centre on owner unless remembered position
       if outerW was clamped narrower than natural → reply {action:"remeasure", cssW:(outerW-frameW)/scale}
       else → SetWindowPos(hidden) ; reply {action:"show"} ; ShowWindow(SW_SHOW) ; presentSurface ; activate
```

```mermaid
sequenceDiagram
  participant M as main page
  participant W as worker (bridge)
  participant H as host (UI thread)
  participant E as editor page
  M->>W: editor.openWindow{id}
  W->>H: post createEditor(gen)  ; reply {opening,gen}
  H->>H: EnableWindow(main,FALSE); create hidden owned HWND; async controller
  H->>E: navigate index.html?window=editor&gen
  E->>W: editor.windowReady ; editor.open{id,gen}
  W-->>E: EditorSession (SetEditDialogOpen(true))
  E->>E: render; fonts.ready; 2×rAF; measure (intrinsic)
  E->>W: editor.measured{cssW,cssH,dpr}
  W->>H: size job
  H->>H: dpi→px, +frame, clamp(work area)
  alt width clamped
    H-->>E: {remeasure,cssW}
    E->>W: editor.measured (≤2 rounds)
  end
  H-->>E: {show}
  H->>H: SetWindowPos; ShowWindow; presentSurface; activate
```

Reply for `measured` is produced by the UI-thread job (`Submit`'s worker job posts to the UI thread and hands the result back through an `Enqueue`d completion); the page awaits the RPC promise.

### 5.4 Fallbacks and remembered bounds

- Remembered valid bounds (§9) take precedence on later opens: the window opens at remembered size/position (work-area-corrected), still waits for the page to report `measured` (so the page can drop `data-measure`) but ignores the size. Only `show` is sent after `measured` or the 5 s guard.
- Measure guard timeout or `measured` out of range (`<1` or `>8192`, or after 2 remeasures): fallback = `cssW=960, cssH=720` (documented constant, clamped like any size) and a log line; not a rollback.
- Width-only remeasure is enough because the content reflows vertically; a height clamp keeps the vertical scroller.

## 6. Draft checkpoints

### 6.1 Frontend sending (E2)

While a session is open and `dirty`: debounce 500 ms trailing, max-wait 2 s, send `editor.checkpoint{sessionId, gen, seq, script}`; `seq` per session, strictly increasing, one in flight (latest wins). Not sent when the script equals the last saved revision. Cleared after successful `editor.save`.

### 6.2 Backend (E1b)

- Bounded: `script` JSON ≤ 1 MiB (`DRAFT_TOO_LARGE`, no replacement of the previous good checkpoint); only the **latest** is kept (`seq` must be greater, else `DRAFT_STALE`, a no-op ack). Holds in `editorSession.checkpoint{seq, script, at}` guarded by `b.mu`; `scriptID` must equal the session's.
- Versioned: stored with `schemaVersion` of `model.Script`; validated with the same `validate` the save path uses, but **invalid drafts are kept too** (flag `valid=false`) so recovery can present them.
- `RecoverDraft(sessionID)` returns the highest-`seq` of {checkpoint, last failed save draft}.

### 6.3 Recovery and persistence of invalid drafts

- Valid draft + forced close → autosaved through `saveScript` (same result type).
- Invalid or conflicting draft + forced close → **not** written to `scripts/`; persisted to `<state-dir>/recovered/<scriptID>-<unixms>.json` (atomic write, 0600, max 5 files per script, oldest pruned), and `editor.windowClosed.reason` = `"draft kept in recovered/…"`; main shows a toast with the file name. The scripts dir and the existing script's revision are never overwritten by an invalid draft.
- A checkpoint-persisted file is **not** auto-reapplied at next open (no silent state change); the toast tells the user where it is. (Q4.)

### 6.4 Idempotence and rejection

Cleanup (`ResetSession`, `editor.close`, forceClose, rollback) are all idempotent and drop the checkpoint with the session. `checkpoint/save/requestClose/closingReady` carrying a stale `gen` or an absent `sessionId` return `STALE_GEN`/`EDITOR_CLOSED` and change nothing.

## 7. Shutdown sequence

`main.go` shutdown calls `BeginShutdown(1500ms, then)` as today. Change:

```
UI thread: BeginShutdown
  roles := {main} ∪ {editor if a Window exists or session exists}
  emit app.closing{deadline_ms:1500, roles} to Both
  if editor present and not CloseRequested: state→CloseRequested with reason "app-closing" and watchdog = min(10s, deadline)
goroutine waits for ALL of: main.closingReady, editor.closingReady (if editor ∈ roles)  — or deadline
  main's ack alone does NOT release; an ack from a role not in `roles` is ignored.
on release: quiesce(); then host job: if editor still present → forceClose (§4.5 steps 1–3, bounded 1 s by remaining budget); then `then()` → Shutdown/queue close/Dispatcher.Close
```

- Deadline semantics: a single **1.5 s** budget for the aggregate wait (unchanged number), after which shutdown proceeds with forced editor close using the **checkpoint** (it cannot wait for the 10 s watchdog; the watchdog is capped to the remaining shutdown budget). `quiesceGrace` (3 s) remains for handlers.
- Editor authoritative: when a session exists the editor's flush (`editor.save`/`editor.close` then `closingReady`) must come before shutdown continues; `editor.save`, `editor.close`, `editor.requestClose`, `editor.checkpoint`, `app.closingReady` stay in `shutdownAllowed`.
- Shutdown during **creation** (`Creating/Loading/Measuring`): treated as startup failure rollback with no toast; no ack is awaited from a window that never loaded.
- Shutdown during **save**: the in-flight save completes (`b.sessMu`), then close; **capture**: `capture.End(EndEditorClosed)` then Stop.
- `Dispatcher.Close()` only after the editor is destroyed.

## 8. Capture, hotkeys, tray, minimise

- **Capture**: `CaptureBackend.HWND` becomes a func `ActiveModalHWND()` (editor hwnd when present and `Open`, else main). `ForegroundIs` is evaluated per event so it follows the active modal. Capture sessions are session-bound; `editor.close`/teardown/rollback call `capture.End` (already) and `CaptureBackend.Stop()`.
- **Hotkeys**: `SetEditDialogOpen(true)` (session open) blocks script hotkeys exactly as today. The emergency stop and overlay toggle remain registered and functional (verified with real input in E1c).
- **Tray Show**: restores main AND, if the editor exists, `ShowWindow(SW_RESTORE)` + activates the editor (it, not main, receives focus because main is disabled).
- **Minimise**: today `SC_MINIMIZE` on main hides to tray. For the editor, `SC_MINIMIZE` minimises normally (owned windows minimise with their owner; they have no taskbar button). The main `SC_MINIMIZE` handler runs only for the main hwnd and, when an editor exists, hides **both** (editor `SW_HIDE`; remembered state `hiddenWithOwner`) and tray Show restores both. Minimising the editor alone never invokes main's hide-to-tray. A hidden-to-tray main with a pending editor `closeRequested` (watchdog) is unaffected: watchdog is timer-driven.
- `WM_ACTIVATE` for the editor does not call main's `OnActivate`; `OnActivate(false)` for main while the editor is open must not end capture (the editor owns capture blur).

## 9. Config schema

```go
type EditorWindow struct {
    X, Y, W, H int  `json:"x","y","w","h"`
    Maximized   bool `json:"maximized,omitempty"`
    Version     int  `json:"version"`   // EditorWindowLayoutVersion = 1
}
// AppConfig: EditorWindow EditorWindow `json:"editor_window,omitempty"`   (pointer-free; zero W/H = unset)
```

**Decision: add `EditorWindow`, do not reuse or migrate `EditDialogPos`.** `EditDialogPos` has no reader or writer in the code (grep: only the field), so there is no data to migrate, and it carries no version, no DPI context, and no maximised flag. Keep it parsed (forward/backward compat for old config files) but mark it deprecated in the doc comment; never written. Per-monitor DPI safety: stored in **physical px** for the work area it was saved under, plus the monitor DPI is not stored — on load the saved rect is accepted only if `W,H ≥ scaledMin` and it intersects a current monitor's work area by ≥ 120×60 px (`onSomeMonitor`); otherwise it is dropped and the measurement path runs. `Version` bump semantics mirror `WindowLayout`: when the editor layout changes materially, increment to invalidate remembered size (position kept). Saved on `WM_EXITSIZEMOVE` and at close (debounced write via existing settings path, no new locking model).

## 10. DPI, monitors, tiny work areas

- `WM_DPICHANGED` on the editor: apply the suggested rect (as main does), then `NotifyParentWindowPositionChanged` + `browser.Resize()`; page reflows by viewport, no remeasure (remembered/measured size already adapts). The measurement result is only valid for the DPI it was measured at; if DPI changes during `Measuring` the host asks for `remeasure` once.
- Disconnected monitor / off-screen: on open and on `WM_DISPLAYCHANGE`, if the editor rect doesn't intersect any work area by the threshold, centre on the owner's monitor (clamped).
- Tiny work area (smaller than natural, e.g. 1024×576 at 150%): outer size = work area (minus a 0 margin), content reflows with scroll in the actions pane; `WM_GETMINMAXINFO` min = `min(scaledMin, work area)`; never larger than the work area, never zero.

## 11. Ownership split, files, milestones

| Milestone | Owner scope | Files (provisional) |
|---|---|---|
| **E1a.2** host/Window routing + shared dispatcher | host/Window split, hwnd→Window map, per-window gates, non-blocking embed, `Submit(raw, reply)`, events routing, role/caller, `WM_SHOWWINDOW` for both; no editor UX yet | `ui/webview/ui_windows.go`, new `ui/webview/host_windows.go`, `ui/webview/window_windows.go`, `ui/webview/rpc/rpc.go` (+ tests), `bridge/bridge.go` (`emitTo`, role check), `bridge/dto.go`, `main.go` (wiring only) |
| **E1b** session/watchdog/shutdown/checkpoints | state machine, modality, `editor.openWindow/requestClose/measured/checkpoint`, watchdog, recovery files, shutdown aggregation, size/DPI math, config | `bridge/editor.go`, new `bridge/editor_window.go`, `bridge/checkpoint.go`, `bridge/bridge.go` (shutdown), `app/controller.go` (read only unless needed), `model/config.go`, `ui/capture_backend_windows.go`, `main.go`, tray handler file |
| **E1c** native Windows selftest evidence | `selftest` mode + CI workflow driving the real app | `main.go` (selftest flag only if existing), `.github/workflows/*selftest*`, `tools/selftest/**` |
| **E2** frontend editor mode | `?window=editor` routing, intrinsic measurement, checkpoint sending, close protocol, `editor.closeRequested` handling | `frontend/src/lib/editor.svelte.ts`, `frontend/src/lib/store.svelte.ts`, `frontend/src/components/EditorScreen.svelte`, `frontend/src/api/*.ts`, tests |
| **E3** simulator | second window in dev sim and Vercel build | `tools/sim/**`, `frontend/src/api/mock.ts`, `vercel` config |

Ordering: E1a.2 → E1b → (E2 ∥ E1c) → E3. `ui/webview/serve*` untouched throughout. Contract changes between milestones go through the Lead.

## 12. Acceptance tests

Unit/headless (CI Linux) marked **U**; Windows native marked **W** (E1c evidence).

**E1a.2**: U dispatcher: per-request sink on success, handler error, BAD_REQUEST, BUSY; closed sink drops; two sinks interleaved keep order; `Close()` unaffected by a window close. U role-gating matrix (`FORBIDDEN_ROLE`). U `emitTo` routing table (§2.4). W hwnd→Window routing: both windows render (non-white), per-window navGate/lockdown denied nav counted per window, `WM_SHOWWINDOW` Reassert for both.

**E1b**: U state machine transitions incl. illegal ones; U `STALE_GEN` on every editor method; U watchdog only starts on close, not reset by repeats, expires at 10 s (fake clock); U checkpoint bounds/order/`DRAFT_TOO_LARGE`/`DRAFT_STALE`; U recovery of valid/invalid/conflicting drafts; U shutdown aggregation (main ack alone does not end, editor ack ends, deadline ends, editor absent ignored); U startup failure rollback leaves `EditDialogOpen=false`, no session, main enabled flag restored; U idempotent cleanup (double close, late save/ack). W: main disabled **before** the editor is interactive (`IsWindowEnabled(main)==false` at editor first-paint callback) and enabled **before** destroy (record order); focus returns to main; repeated New/Edit focuses the existing editor (no second hwnd); startup failure (inject embed failure and a 10 s hang) → toast, main enabled, hotkeys re-enabled; tray Show restores both, editor minimise does not hide main, main minimise hides both; mixed DPI (move editor to a 150% monitor → `WM_DPICHANGED`, size rule holds); tiny work area (virtual 1024×576@150%) ≤ work area; shutdown during creation, during save, during capture; renderer failure (`ProcessFailed`) while Open → session reset, window closed, main enabled, toast; forced close via unresponsive-renderer test (page stalls `closeRequested`, watchdog fires at ≈10 s, checkpoint recovered **on disk** — assert saved script revision or recovered file content); reopen after forced close (new gen, new window, works); capture targeting (`capture.key` fires only when the editor is foreground; main-foreground ignored; ends on teardown); emergency and overlay hotkeys work with real input (SendInput) while a script hotkey is blocked (script does not run, count unchanged).

**E2**: U (vitest) intrinsic measurement independent of viewport (resize jsdom viewport, same result) with bounded actions height; reports after fonts/render; remeasure width flow ≤2 rounds; `data-measure` removed on show; checkpoint debounce/max-wait/seq/one-in-flight/clear-on-save; `closeRequested` runs guard then `requestClose`, conflict leaves editor open; no `100vh` dependence in measurement mode; layout reflows at min and tiny sizes (Playwright screenshots).

**E3**: dev `npm run sim` and Vercel show the editor as a second window with drag/resize; same `openWindow` handshake against the mock; close → returns to main.

## 13. Windows CI evidence (E1c)

Per scenario, JSON + screenshots uploaded as workflow artifacts: editor content/client/outer bounds (px), DPI (`GetDpiForWindow`) for editor and main, monitor work area, `devicePixelRatio`, the reported `cssW/cssH`, the clamp result and remeasure rounds, `IsWindowEnabled(main)` timeline (before show, during, after re-enable, before destroy), `GetForegroundWindow` after close, resize/reflow results (three sizes: natural, min, work-area; no horizontal clipping, no overlap), close/reopen outcomes (normal, X, Alt-F4, forced via watchdog), checkpoint recovery file/revision hash, WS/private memory with the editor open vs closed, and screenshots of main and editor in each scenario, plus real-input log for the hotkey scenario. Failures fail the job; the evidence file is the pass criterion.

## 14. Open questions (need Lead/Human decision)

- **Q1** Fallback size when the measure guard times out: `960×720 CSS px` acceptable (not "no guessing" in the happy path, only as a failure fallback)?
- **Q2** If the close guard leaves the editor open (conflict dialog awaiting the user) the 10 s watchdog would force-close under an active dialog. Proposed: the watchdog is cancelled by an explicit `editor.guardActive` signal from the page (a bounded 60 s ceiling), because "hung renderer" ≠ "user deciding". Otherwise the user's conflict decision is raced. Requirement text says unreset; confirm.
- **Q3** Same-script reopen while editor exists but for a **different** `id`: focus only (proposed) vs. offer a switch.
- **Q4** Invalid draft after a forced close is kept as a file only (not re-offered at next open). Is a "Recover draft" UI in scope (probably later)?
- **Q5** `editor.open` is now sent by the editor page (not main) — the old main-window editor route remains until E2 removes it; is a feature flag needed for a single release?
