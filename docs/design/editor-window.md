# Editor window — design contract (v1.6.0)

Status: **contract rev 2 (E1a.1)**, revised after review of ff96774. Frozen once the Lead accepts it; later changes need a REOPEN.
Base: `origin/main` a507e2f. Spike evidence: `origin/spike/editor-window-cost` (throwaway).

## 1. Scope

New Script / Edit open the editor in a separate native, modal, resizable window owned by the main window. **The old in-main editor route is removed outright in E2 (no flag, no fallback).**
Out of scope: `ui/webview/serve*` (a concurrent PR renames `app.local` → `app.invalid`; the editor URL uses the serve host constant, never a literal), multiple editors, a Recover-draft UI, changes to save/conflict semantics.

Spike facts: a second controller from the **same environment** works; it stays white without the `WM_SHOWWINDOW` Reassert (`presentSurface`); no new `NewCallback`/`NewComProc` sites (same class, `hostProc`, package-level vtables); ≈+35 MB private/+95 MB WS while open; `EnableWindow` modality works.

## 2. Architecture

### 2.1 Process host and Windows (replaces `current atomic.Pointer[UI]`)

```
host (process singleton; UI-thread-owned state)
  env     *edge.Environment        // ONE, shared by both controllers
  windows map[win.HWND]*Window     // UI-thread only
  disp    *rpc.Dispatcher          // ONE shared serial dispatcher
Window { role main|editor; gen uint64; hwnd; browser; navGate; lockdown; resFilter; bootstrapJS;
         owner *Window; state; closed atomic.Bool }
```

- `hostProc` resolves `windows[hwnd]`; unknown hwnd → `DefWindowProc`. Same class and proc for both windows; behaviour switches on `Window.role`.
- Per window: `navGate` (the editor may navigate only within its own `index.html?window=editor&gen=…`), `ProcessFailed`, `Lockdown`, `WebResourceRequested` filter, bootstrap script (injects `window.__role`, `window.__gen`, nonce). Min-size, `OnActivate`, `OnClose`, `reportBounds` become per-role callbacks. `Quarantine`, `Reload`, `ProbeNavigationLock` stay main-only; `Stats()` sums both windows.
- `WM_SHOWWINDOW(wp≠0)` → `presentSurface("shown")` for both windows.
- Show / Hide / minimise / restore (today main-only, `ui_windows.go:342,488`) become `Window` methods; the main-only minimise-to-tray rule applies to `role==main` only.

### 2.2 Non-blocking embed

`editor.openWindow` (a worker job) only **posts** `createEditorWindow(gen, scriptID)` to the UI thread and returns. On the UI thread: create hidden owned window → async `CreateCoreWebView2Controller` against the shared env; the completion callback finishes setup (navGate, lockdown, filter, bootstrap, `Navigate`) on the UI thread. No nested `EmbedTimeout` pump inside RPC jobs. The main window's existing blocking embed is unchanged.

### 2.3 Shared serial Dispatcher, request-local sinks, caller identity

```go
type Reply  func([]byte)                         // must not block
type Caller struct{ Role Role; Gen uint64; HWND uintptr }  // attached by the transport, never by the page
func (d *Dispatcher) Start()
func (d *Dispatcher) Submit(raw string, caller Caller, reply Reply)
```

- One worker, strictly serial (the bridge assumes it). Global queue limit; BUSY goes to the submitter only.
- `reply` and `caller` are stored in the queue `item` and used on **every** path: malformed (BAD_REQUEST), BUSY, handler error, success.
- **Identity is caller-context, not params.** Existing param structs and strict decoding (`bridge/errors.go:88`) are unchanged. Handlers needing identity are registered with `regCtx` and read `rpc.CallerFrom(ctx)`.
- **Generation is validated at three points:** (a) at **dequeue** (just before the handler), for every method of role `editor`; a stale `Caller.Gen` → `STALE_GEN`, handler not run (covers queued `capture.*`, `editor.*`, `settings.*` reachable from the editor); (b) inside the handler under `b.mu` when it commits (existing session check); (c) at **delivery** (UI thread, `windows[hwnd].gen == Caller.Gen`; otherwise dropped).
- **When a gen becomes invalid:** at the moment the host executes `invalidate(gen)` on the UI thread, which happens (in this order) **immediately before** it posts the force-close/cleanup job, and in the same step marks `Window.closed`. So any request of that gen dequeued after this point is rejected; a request already running finishes (its commit check against the session still protects state) but its reply is dropped.
- Closing the editor never closes the dispatcher; only app shutdown does.

### 2.4 Event routing (`bridge.emitTo(Target, …)`, Target ∈ Main|Editor|Both)

| Event | Target |
|---|---|
| `state`, `scripts.changed`, `settings.changed`, `save.failed` | Both |
| `update.*`, `nav.goto` | Main |
| `editor.reset`, `editor.closeRequested`, `capture.hotkey`, `capture.key`, `capture.mouse`, `capture.ended` | Editor — **destination (hwnd, gen, sessionId) is captured when the event is emitted**, so a late capture event for a destroyed editor is dropped, never delivered to a replacement |
| `editor.windowFailed`, `editor.windowClosed` | Main |
| `app.closing` | Both |

## 3. RPC contract

Wire conventions unchanged (`{id,method,params}` → `{id,result|error}`). Method names are the registered ones (`bridge/bridge.go:585-620`). Identity/generation are caller context (§2.3), not params. New error codes: `FORBIDDEN_ROLE`, `STALE_GEN`, `SHUTTING_DOWN` (exists as stopping), `DRAFT_TOO_LARGE`, `EDITOR_WINDOW_BUSY`.

### 3.1 New / changed methods

| Method | Role | Params | Result |
|---|---|---|---|
| `editor.openWindow` | main | `{id?: string}` | `{status:"opening"\|"focused", gen:number}`. Existing editor (any state ≥ Creating) → `focused`: restore if iconic, activate, no new session, for any `id` (main toasts "Editor already open"). `SHUTTING_DOWN` during shutdown. |
| `editor.windowReady` | editor | `{}` | `{}` — page booted; informational. |
| `editor.open` | editor (page, not main) | `{id?: string}` (unchanged) | `EditorSession` unchanged (+`revision:number`, the session's current revision, §6.1). Rejected `STALE_GEN`. |
| `editor.measured` | editor | `{token:number, cssW:number, cssH:number, dpr:number}`; ints 1..8192 | `{action:"show"\|"remeasure", cssW?:number}`. **Exactly one result:** the handler completes only when the host's UI-thread decision arrives (the worker job awaits a completion channel with a 2 s bound), so there is never an immediate plus a later reply. |
| `editor.measureEnd` | editor | `{token:number}` | `{}` — page left `data-measure`. Sent after `show` or after the fallback decision; the host shows the window on this or after a 1 s grace (§5.4). |
| `editor.checkpoint` | editor | `{sessionId, revision:number, script}` | `{applied:boolean, revision:number}` — `applied:false` is a **no-op success** (revision not newer than the stored one, §6.2). |
| `editor.guardActive` | editor | `{sessionId}` | `{leaseMs:number}` or `STALE_GEN`/`EDITOR_CLOSED`. Renews the close lease (§4.5). Rejected during shutdown (`SHUTTING_DOWN`). |
| `editor.closeDecision` | editor | `{sessionId, decision:"save"\|"discard"\|"cancel", script?:Script, revision?:number}` | `{}` — the **only** way the page influences close (§4.3). |
| `editor.save` | editor | unchanged (`sessionId`, `script`, existing optional fields) | unchanged; `EDITOR_CLOSED` after cleanup |
| `editor.close` | host-internal only | — | **No longer registered for pages.** The existing handler body becomes the internal `closeSession(sess, mode)` called by the host. A page call → `FORBIDDEN_ROLE`. |

### 3.2 Exhaustive role matrix

| Methods | main | editor |
|---|---|---|
| `app.boot`/`app.ready`/`state.get`, `actions.catalog`, `clipboard.write`, `settings.get` | ✓ | ✓ |
| `app.closingReady` | ✓ | ✓ (§7) |
| `editor.openWindow` | ✓ | ✗ |
| `editor.windowReady/open/measured/measureEnd/checkpoint/guardActive/closeDecision/save`, `editor.describeActions`, `editor.toJSON`, `editor.fromJSON` | ✗ | ✓ |
| `capture.hotkey.start`, `capture.key.start`, `capture.mouse.start`, `capture.mouse.stop`, `capture.stop` | ✗ | ✓ |
| `scripts.*`, `settings.update`, `settings.setSidebarCollapsed`, `overlay.*`, `autoactive.set`, `update.*`, `window.*`, `app.setGlobalActive`, `app.exit` | ✓ | ✗ (`FORBIDDEN_ROLE`) |

An unlisted method is **denied** for the editor (default deny). Role comes from `Caller`, not params.

### 3.3 Events (typed)

```ts
editor.closeRequested {gen:number, sessionId?:string, reason:"user"|"app-closing"|"owner-closing"}
editor.windowFailed   {gen:number, stage:"create"|"embed"|"timeout"|"navigate"|"measure"|"renderer", message:string}
editor.windowClosed   {gen:number, saved:boolean, forced:boolean, recoveredPath?:string, reason?:string}
editor.reset          {reason:string, sessionId:string, gen:number}
app.closing           {deadline_ms:number, roles:("main"|"editor")[]}
```
`dto.go`, `api/validate.ts` and the `bridge_test.go` round-trip tables list every new method/event (existing tests enforce it).

## 4. Window/session state machine

### 4.1 States

```
 None ─openWindow→ Creating ─embed ok→ Loading ─windowReady→ Measuring ─show→ Open
   ▲                 │ fail/timeout      │ 10 s (Creating+Loading)   │ measure guard (§5)   │ WM_CLOSE / app.closing
   │                 ▼                   ▼                           ▼                      ▼
   │              Rollback ◄─────────────┴───────────────────────────┘                CloseRequested ──keep editing/cancel──► Open
   │                 │                                                                    │ decision(save|discard) | deadline
   └──── Destroyed ◄─┴───── Closing ◄────────────────────────────────────────────────────┘
```
`Rollback`/`Failed` (§4.7) applies from every state. Only `None` accepts `openWindow` as create; any other state focuses.

### 4.2 Modality and execution gating (acceptance-critical)

1. **Creation start (before anything else):** `SetEditDialogOpen(true)` (script hotkeys blocked from this moment), then `EnableWindow(main,FALSE)`, then create the hidden editor. Main is never interactive while the editor can be. Rollback reverses both. `editor.open` later binds the session to the already-open gate (it does not toggle it).
2. **Close:** `EnableWindow(main,TRUE)` **before** `DestroyWindow(editor)`; then restore focus (below). `endModal()` is one idempotent UI-thread function guarded by a `modalHeld` flag.
3. **Focus/visibility on close:** record `mainWasVisible` at creation start. On close: if `mainWasVisible` → `SW_SHOW`/restore main as it was (preserve maximized: restore uses `SW_RESTORE` only when iconic, never un-maximizes), `SetForegroundWindow(main)`, `Focus()` on its browser. If main was hidden to tray or minimised at open → **preserve that state** (do not reveal; no foreground steal).
4. **Repeat New/Edit:** restore the editor if iconic (`SW_RESTORE`, keeping maximized), `SetForegroundWindow`; never create a second hwnd.

### 4.3 Single authoritative close path (host-only cleanup)

The frontend guard (existing `close()` logic: save, conflict dialog, discard) **returns a decision** — `save` (with the final script/revision), `discard`, or `cancel` — through `editor.closeDecision`. It **never releases the session**: `finish()`/`editor.close` are removed from the page. The host alone performs cleanup:

1. `WM_CLOSE`/Alt-F4/X on the editor: `hostProc` returns 0, state → `CloseRequested`, emit `editor.closeRequested`, start the close timers (§4.5). Repeated requests in `CloseRequested` do not re-emit, do not reset timers.
2. Page runs the guard, then sends `closeDecision`:
   - `save`: host runs the normal `saveScript` with the supplied script. Success → cleanup. Conflict/validation failure → the guard stays on the page: the page shows its conflict dialog and sends a new `closeDecision` when the user resolves it (override save, discard, or cancel). The close attempt continues under the lease (§4.5).
   - `discard`: cleanup without saving; the session checkpoint is dropped.
   - `cancel` ("keep editing"): state → `Open`, **this close attempt is cancelled** (all timers cleared). The 60 s ceiling restarts only with a **new** close request after returning to `Open`.
3. **Cleanup (`closeSession`)**: end capture, drop session + checkpoint, `invalidate(gen)`, `endModal()`, `SetEditDialogOpen(false)`, destroy, `editor.windowClosed`. Idempotent; a second call is a no-op. Because the page never releases, the session is guaranteed to exist until the host decides.

### 4.4 (merged into §4.3)

### 4.5 Close timers: base deadline, renewable lease, ceiling

Timers exist **only** in `CloseRequested` (never while `Open`). Let `T0` = first close request after entering `CloseRequested`.

- **Base deadline** `T0+10 s`: if no `closeDecision`/`guardActive` arrived, force-close.
- `editor.guardActive` (sent by the page every ≤1 s while a guard dialog is open **and** the page is responsive): grants a **lease of 3 s** from receipt. Deadline = `min(T0+60 s, max(base, now+3 s))`. A hung page cannot renew → the base/lease expires.
- **Absolute ceiling** `T0+60 s`, never extended.
- **Never extended during app shutdown**: `guardActive` → `SHUTTING_DOWN`; shutdown uses its own deadlines (§7).
- A valid `closeDecision` ends the lease; `save` failure with a conflict re-arms it only through new `guardActive` pings (so an unresponsive page after a failed save still force-closes at the lease/base).
- **Force close** (`forceClose(gen)`): `invalidate(gen)`; recover (§6.3); `closeSession`; destroy (browser `Close()` bounded 1 s, then `DestroyWindow`). The forced path runs on the **UI thread for the invalidate/endModal/destroy steps** (needs no worker) and on a **dedicated recovery goroutine** for the persistence step, **not** queued behind the dispatcher worker. So a worker stuck in a slow handler cannot stop the window from being destroyed and main re-enabled; only the recovery write may be delayed, bounded by 2 s (then `recovered/` file only).
- **Queue delay and the 10 s guarantee:** the 10 s bounds *window destruction* (UI thread timers, independent of the worker). Persistence is bounded separately (≤2 s after the deadline). The recovery uses the checkpoint snapshot already in memory.

### 4.6 Generation tokens

`gen` (monotonic `uint64`) is assigned at `openWindow` and stored on `Window` and `editorSession`; the nonce in the bootstrap script must match. Native callbacks (controller completion, `ProcessFailed`, timers, `NavigationStarting`) capture `gen` and no-op on mismatch (releasing resources they created). Validation points: §2.3.

### 4.7 Failure in every state (runtime and startup)

`failEditor(gen, stage, cause)` runs on the UI thread from any state (`Creating`, `Loading`, `Measuring`, `Open`, `CloseRequested`, `Closing`):

1. `invalidate(gen)`; stop timers.
2. If a session exists: recover from the latest checkpoint (§6.3) on the recovery goroutine (best effort, bounded 2 s).
3. End capture, drop session, `SetEditDialogOpen(false)`, `endModal()`, destroy the editor window (or release its pending controller), restore focus per §4.2.3.
4. Emit `editor.windowFailed{stage,message}` (plus `recoveredPath`) to main, which toasts.

Distinguish the failing process:
- **Editor renderer failure** (`ProcessFailed` with kind render-exit/unresponsive on the editor's controller): `failEditor(stage:"renderer")`. Main continues untouched (**no** main reload).
- **Shared browser-process failure** (browser exit, or the environment's `BrowserProcessExited`): affects both controllers → editor `failEditor` first (so main is re-enabled and the draft recovered), then the existing main recovery/fatal policy (`main.go:487-497`) runs. The existing `ResetSession("processFailed")` callback is replaced by a role-aware version: it must not reset the editor session when the main renderer failed, and must not reload main when only the editor failed.
- Repeated editor renderer failures (≥3 in 60 s) are logged; each still recovers cleanly (no fatal for an auxiliary window).

## 5. Size measurement

### 5.1 Design

Default size = the editor's **intrinsic natural size at compact density**, measured in the page, converted by the host. No hard-coded size except the failure fallback (§5.4).

- **Numeric floor:** compact-density minimum content `minCssW=720`, `minCssH=480` CSS px (host `scaledMin` converts). Natural size below the floor is raised to it.
- **"No empty space" criterion (measurable):** after show at the measured size, `scrollWidth ≤ clientWidth` and `scrollHeight ≤ clientHeight` on the measurement root, and the bottom of the last content block is within 24 CSS px of the client bottom (E1c asserts this from page-reported metrics).
- Actions-pane preferred height token `--actions-pref-h: 360px` (bounded; list scrolls inside it).

### 5.2 Page side (E2)

On `?window=editor`: set `<html data-measure>`: the layout becomes intrinsic (`height:auto`, `width:max-content`, no `100vh`, no `1fr` stretching, actions pane at `--actions-pref-h`), independent of the viewport. Compact density forced. After session loaded, `document.fonts.ready`, two rAF → `ceil` of the measurement root's border box → `editor.measured`. On `{remeasure,cssW}` set root width to `cssW`, wait two rAF, measure, report again. On `{show}` or fallback: remove `data-measure`, send `editor.measureEnd`; the layout becomes viewport-driven (`100vh`, `1fr`). Leaving measure mode must not change window size.

### 5.3 Host side — target monitor and DPI first

1. Choose the **target monitor**: remembered bounds' monitor if valid (§9), else the owner's monitor.
2. Position the **hidden** HWND on that monitor (default rect at the monitor's work-area centre), let `GetDpiForWindow` settle on that monitor (`WM_DPICHANGED` for the hidden window is processed before measuring is accepted).
3. Only then accept `measured` (its `token` equals the current measurement token); convert: `scale = dpi/96`, `clientPx=ceil(css*scale)`, `outer = client + AdjustWindowRectExForDpi frame`, clamp to the target work area (min = `min(scaledMin, work area)`), centre on owner unless remembered.
4. Reply `show` (see 5.4) or `remeasure`.

```mermaid
sequenceDiagram
  participant M as main page
  participant W as worker
  participant H as host (UI thread)
  participant E as editor page
  M->>W: editor.openWindow{id}
  W->>H: post createEditor(gen) ; reply {opening,gen}
  H->>H: SetEditDialogOpen(true); EnableWindow(main,FALSE); hidden owned HWND on target monitor; async controller
  H->>E: navigate index.html?window=editor&gen
  E->>W: editor.windowReady ; editor.open{id}
  W-->>E: EditorSession
  E->>E: data-measure; fonts.ready; 2×rAF; measure
  E->>W: editor.measured{token,cssW,cssH,dpr}
  W->>H: size job (await)
  H->>H: dpi→px, +frame, clamp
  alt clamped narrower, or DPI changed (budget left)
    H-->>E: {remeasure,cssW}  (new token)
    E->>W: editor.measured
  end
  H-->>E: {show}
  E->>W: editor.measureEnd
  H->>H: SetWindowPos; ShowWindow; presentSurface; activate
```

### 5.4 One retry policy; fallback; hung page

- **One budget: at most 2 extra measure rounds**, shared by width remeasure **and** DPI change (a `WM_DPICHANGED` while Measuring consumes one round and **invalidates the token**; a `measured` carrying an old token is ignored, replied `{action:"remeasure"}` with the new token only if budget remains).
- Budget exhausted, guard (5 s per round, 12 s total) expired, `measured` out of range, or page error → **fallback size 960×720 CSS px** (clamped like any size), log line, the host replies/pushes `editor.measureEnd`-equivalent `show`.
- **Fallback must exit measure mode:** the host sends the page `{action:"show"}` (via the `measured` reply when it is pending, else `editor.measureEnd` request event); the page removes `data-measure` and sends `editor.measureEnd`.
- **Hung page** (never reports, never acks): the host **shows anyway** at the fallback size after the guard; `editor.measureEnd` is then not required. The close watchdog covers any later hang.
- Remembered valid bounds (§9) take precedence: the window opens there; the page still measures only to know when to exit measure mode; the reported size is ignored.

## 6. Draft checkpoints and recovery

### 6.1 One revision per session

A single monotonically increasing **`revision`** (JS-safe integer, `1..2^53−1`; overflow → `DRAFT_TOO_LARGE`-style `BAD_REQUEST`) per session, assigned by the **page** and used by both `editor.checkpoint` and `editor.save`'s response and `closeDecision`. The session starts at `revision` returned by `editor.open` (the stored revision); the page increments it per edit snapshot. The backend keeps `sess.lastSavedRev` and `sess.checkpoint{revision, script, valid, at}`.

### 6.2 Checkpoint rules

- Page sends debounced (500 ms trailing, 2 s max-wait), one in flight, only when dirty; the bound is **1 MiB** per message for **every draft kind** (checkpoint, failed-save draft, `closeDecision.script`, recovered file). Over → `DRAFT_TOO_LARGE`, previous checkpoint kept.
- Accept only `revision > stored`; otherwise **no-op success** `{applied:false}`.
- `scriptID` must equal the session's; invalid scripts are stored with `valid=false` (not rejected).
- A **successful save clears all checkpoints with `revision ≤` the saved revision**; the failed-save draft (`sess.draft`) is stamped with the revision of the failing save and ordered by the same number.
- `RecoverDraft(sessionID)` returns the highest-revision of {checkpoint, failed-save draft} not yet cleared.

### 6.3 Recovery and persistence

- Valid + non-conflicting: autosaved through `saveScript` (same path as a user save).
- Invalid or conflicting: **never written into `scripts/`** and never overwrites the stored revision. Written to `<state-dir>/recovered/<scriptID>-<unixms>.json` (atomic temp+rename, 0600, ≤5 per script, oldest pruned) with the envelope:
  ```json
  {"v":1,"scriptId":"…","revision":N,"savedAt":"RFC3339","script":{…}}
  ```
  (`model.Script` has no version field, so the envelope carries `v`.) The path is returned in `editor.windowClosed.recoveredPath` and toasted on main. No auto-reapply, no Recover UI in 1.6.0.
- **Persistence path bypasses the mutation fence:** `recoverPersist` is a dedicated function that does **not** go through `fenceMutations`/`hard` (the fence rejects after `hard=true`), takes `b.sessMu` with a 500 ms try-lock; if the lock or the save fails it falls back to the `recovered/` file write, which needs no lock. It runs on the recovery goroutine, never queued behind the worker.

### 6.4 Idempotence

`closeSession`, `failEditor`, `forceClose`, rollback are idempotent and drop the checkpoint with the session. Late `checkpoint`/`save`/`closeDecision`/`guardActive`/`closingReady` after cleanup → `EDITOR_CLOSED`/`STALE_GEN`, no side effect.

## 7. Shutdown

Three absolute deadlines from shutdown start `S` (`BeginShutdown`, UI thread):

| Deadline | Time | What must be done |
|---|---|---|
| **D1 cooperative ack** | `S+1.5 s` (existing) | all roles in `roles` have sent `app.closingReady` (editor present ⇒ editor must; main's ack alone does **not** release). |
| **D2 recovery/persistence** | `S+2.5 s` | if the editor still exists at D1 (or no ack): `forceClose` — `invalidate`, recovery via the dedicated path (§6.3, bounded to D2). |
| **D3 hard destroy** | `S+3.0 s` | editor window destroyed and main re-enabled regardless (UI thread, needs no worker). |

Then the existing `quiesce(quiesceGrace=3 s)` starts at `max(D1, ack time)` **after** recovery has run (`hard` is set only after recovery), and `then()` runs. **Worst-case total from `S` to `then()`: 1.5 + 1.0 (recovery) + 3.0 (quiesce) = 5.5 s** (old worst case 4.5 s). Destruction (D3) is inside that bound and independent of quiesce.

- Sequence: `app.closing{deadline_ms:1500, roles}` to Both → editor (if `Open`) enters `CloseRequested(reason:"app-closing")`, page runs the guard in **decision-only** mode and sends `closeDecision` (save/discard; `cancel` is treated as `discard`-without-save → draft recovered) and then `app.closingReady`. The host cleanup (§4.3 step 3) runs before it records the editor's readiness. `guardActive` is rejected (no lease).
- `closingReady` is accepted from role `editor` only when `Caller.Gen` is current; from a role not in `roles` it is ignored.
- Shutdown during **creation** (Creating/Loading/Measuring): `failEditor` with no toast; no ack awaited from a window that never loaded.
- During **save**: the in-flight save completes under `sessMu`, then cleanup; recovery's try-lock (500 ms) bounds waiting. During **capture**: `capture.End(EndEditorClosed)`, `CaptureBackend.Stop()`.
- `Dispatcher.Close()` only after the editor is destroyed.
- `shutdownAllowed` becomes: `editor.save`, `editor.closeDecision`, `editor.checkpoint`, `app.closingReady`.

## 8. Capture, hotkeys, tray, minimise

- **Capture targeting:** `CaptureBackend.HWND` → `ActiveModalHWND()` (the editor's hwnd while a session/editor exists, else main), evaluated per event. **Mouse capture is gated too:** `StartMousePick` gets the same `ForegroundIs(ActiveModalHWND)` predicate (today it is ungated); a mouse press while another app is foreground is ignored. Every capture event is stamped with the (hwnd, gen, sessionId) at emission (§2.4). Capture ends on any editor teardown (`closeSession`/`failEditor`).
- **Hotkeys:** script hotkeys stay blocked from creation start to cleanup (§4.2.1); the emergency and overlay toggles stay functional (real-input evidence in E1c).
- **Tray Show:** restores main **and**, when the editor exists and was hidden with its owner, restores/activates the editor (maximized preserved: restore only when iconic or hidden, never alters maximized state). Focus goes to the editor (main is disabled).
- **Minimise:** minimising the editor alone minimises it normally (owned: no taskbar button; it minimises with its owner) and never triggers main's hide-to-tray. Main's `SC_MINIMIZE` hide-to-tray (main hwnd only) also hides the editor (`hiddenWithOwner=true`). The close timers are UI-thread timers and are unaffected by visibility.
- `WM_ACTIVATE` of the editor does not call main's `OnActivate`; main losing activation while the editor is open must not end capture.

## 9. Config schema

```go
type EditorWindow struct {
    X         int  `json:"x"`
    Y         int  `json:"y"`
    W         int  `json:"w"`
    H         int  `json:"h"`
    Maximized bool `json:"maximized,omitempty"`
    Version   int  `json:"version"`   // EditorWindowLayoutVersion = 1
}
// AppConfig.EditorWindow EditorWindow `json:"editor_window,omitempty"`
```

X, Y, W, H are separate fields with their own JSON names (physical px). **Decision: new `EditorWindow`; `EditDialogPos` is left as is (still parsed, still marshalled as an empty `{}` `WindowPos`), not removed or hidden.** Justification: it has no reader/writer in code, so there is nothing to migrate; changing its serialization (custom `MarshalJSON`) adds risk for zero benefit, and an older build reading a newer file still finds the key. It gets a `// Deprecated: unused` comment only.
Validity: saved bounds accepted only if `W,H ≥ scaledMin` and the rect intersects a current work area by ≥120×60 px (`onSomeMonitor`); else dropped and measurement runs. A `Version` bump invalidates the remembered **size** (position kept). Saved on `WM_EXITSIZEMOVE` and on close through the existing settings write path (no new locking model).

## 10. DPI, monitors, tiny work areas

- `WM_DPICHANGED` on the editor: apply the suggested rect, `NotifyParentWindowPositionChanged`, `browser.Resize()`. While Measuring it consumes a retry round and invalidates the token (§5.4); when Open the page just reflows.
- Display change / monitor removed: on `WM_DISPLAYCHANGE` and at open, an editor rect not intersecting any work area by the threshold is centred on the owner's monitor (clamped).
- Tiny work area: outer = work area, content scrolls in the actions pane; `WM_GETMINMAXINFO` min = `min(scaledMin, work area)`; never larger than the work area.
- The pure maths (`scale`, frame add, clamp, validity, retry budget) lives in a testable function `computeEditorBounds(...)` with table tests.

## 11. Ownership split

| Milestone | Scope | Files |
|---|---|---|
| **E1a.2** host/Window routing + shared dispatcher | Window split, hwnd→Window map, per-window gates, non-blocking embed, `Submit(raw,caller,reply)`, caller-context + role matrix, `emitTo`, `WM_SHOWWINDOW` both, Show/Hide/restore as Window methods | `ui/webview/ui_windows.go`, new `ui/webview/{host,window}_windows.go`, `ui/webview/rpc/rpc.go`(+tests), `bridge/bridge.go` (role matrix, `emitTo`, `regCtx`), `bridge/dto.go`, `main.go` (wiring) |
| **E1b** session/watchdog/shutdown/checkpoints | state machine, modality, `openWindow/closeDecision/measured/measureEnd/checkpoint/guardActive`, close timers, `failEditor`, recovery + `recoverPersist`, shutdown D1–D3, bounds maths, config, capture targeting incl. mouse gate, tray/minimise, **native host files**: `ui/webview/{window,host}_windows.go` (timers, WM_CLOSE/DPI/DISPLAYCHANGE), `ui/webview/hotkey/capture*` (mouse gate), tray handler | `bridge/editor.go`, new `bridge/{editor_window,checkpoint,recover}.go`, `bridge/bridge.go` (shutdown), `model/config.go`, `ui/capture_backend_windows.go`, `ui/webview/*_windows.go`, tray file, `main.go` (`Recovery` role-awareness) |
| **E2** frontend editor mode | `?window=editor`, intrinsic measurement + `data-measure`, checkpoint sender, decision-only close (remove `finish()`/`editor.close` from the page and the in-main editor route), `guardActive` pings, `editor.closeRequested` | `frontend/src/lib/{editor.svelte,store.svelte}.ts`, `frontend/src/components/EditorScreen.svelte`, `frontend/src/api/{validate,types}.ts`, tests. **Not** `api/mock.ts` (E3's). |
| **E1c** native Windows evidence | host-fixture evidence may start after E1b; **final integrated acceptance runs after E2** | `.github/workflows/*selftest*`, `tools/selftest/**`, a **measurement hook** (`window.__editorMetrics()` exposing `{scrollW,clientW,scrollH,clientH,lastBlockBottom}`) and an **input hook** (a SendInput driver) agreed between E2 and E1c before E2 starts |
| **E3** simulator | second window in dev sim and Vercel | `tools/sim/**`, `frontend/src/api/mock.ts`, vercel config. Starts after E2. |
| **Integrated acceptance** | one pass on the real app after E3 | CI artifacts (§13) |

Ordering: E1a.2 → E1b → E2 → E3; E1c host-fixture alongside E1b/E2, final E1c after E2; integrated acceptance last. `ui/webview/serve*` untouched.

## 12. Acceptance tests

**U** = unit/headless; **W** = native Windows CI (E1c); **B** = real browser (Playwright, not jsdom); **H** = hardware/manual evidence (windows-latest has no multi-monitor/mixed-DPI).

**E1a.2:** U dispatcher: per-request reply on success/error/BAD_REQUEST/BUSY; closed sink drops; `Caller` carried; dequeue-time gen rejection; `Close()` unaffected by window close; U role matrix incl. default-deny; U `emitTo` table and capture destination capture; W both windows render (non-white), per-window navGate counts, Reassert for both.

**E1b:** U state machine incl. illegal transitions and `failEditor` from every state; U close timers: none while Open; base 10 s; lease 3 s renew; 60 s ceiling; repeated requests do not reset; cancel returns to Open and a new request restarts the ceiling; `guardActive` rejected in shutdown (fake clock); U single close path (page never releases; double cleanup no-op); U revision ordering, clearing on save, `{applied:false}`, 1 MiB for all draft kinds; U recovery valid/invalid/conflicting + envelope; U shutdown D1/D2/D3 with a stuck worker, main-ack-alone not releasing, worst case ≤5.5 s; U `computeEditorBounds` tables (clamp, retry budget, validity, tiny area, DPI). W: main disabled **before** the editor is interactive and enabled **before** destroy (ordered log); script hotkeys blocked from creation start; focus restore (main visible at open → foreground; main in tray at open → stays in tray); repeated New/Edit restores an iconic editor and keeps maximized; startup failure rollback (injected embed failure + 10 s hang); tray Show restores both and preserves maximized; editor minimise does not hide main; shutdown during creation/save/capture; editor renderer failure while Open (checkpoint recovered, main untouched, no main reload); shared browser-process failure (editor cleaned before main policy); forced close with an unresponsive renderer (checkpoint on disk; ≤10 s to destroy; recovered file/revision asserted); reopen after forced close (new gen works, old gen events ignored); capture targeting for key, hotkey and **mouse** (foreground-gated, ignored when another app is foreground); emergency and overlay hotkeys with real input while a script hotkey is blocked. **H:** mixed-DPI move, monitor-disconnect recovery, tiny work area on a real small display (CI simulates tiny via a fake work-area input only).

**E2:** **B** intrinsic measurement is independent of viewport size (resize the real browser viewport, identical result), bounded actions height, after fonts/render; remeasure/budget; `data-measure` exit incl. fallback and hung host; "no empty space" metrics; reflow at natural/min/work-area (screenshots); U checkpoint debounce/max-wait/revision/clear-on-save; U decision-only close (save/discard/cancel/conflict), `guardActive` ping cadence only while responsive; the in-main editor route is gone (no references).

**E3:** dev sim and Vercel show the editor as a second window with the same handshake; close returns to main.

**Integrated (after E3):** one W run on the shipped binary with the real frontend covering the full E1b W list plus size-fit metrics.

## 13. Windows CI evidence (E1c)

Per scenario (JSON + screenshots as artifacts): editor content/client/outer bounds (px), DPI for editor and main (`GetDpiForWindow`), monitor work area, `devicePixelRatio`, reported `cssW/cssH`, token and retry rounds used, clamp result, no-empty-space metrics (`scrollW/clientW/scrollH/clientH/lastBlockBottom`), `IsWindowEnabled(main)` timeline (before editor show, during, after re-enable, before destroy), foreground window after close, tray state before/after, resize/reflow results at three sizes, close/reopen outcomes (X, Alt-F4, decision paths, watchdog forced, shutdown), recovered file path + content hash, WS/private memory open vs closed, screenshots of main and editor, real-input log for the hotkey scenario. Failures fail the job. H items are attached as a manual evidence note with screenshots.

## 14. Open questions

None blocking. Decided and folded in: failure-only fallback 960×720 (§5.4); guard lease 3 s / ceiling 60 s (§4.5); focus-only for a second New/Edit (§3.1, §4.2); no Recover UI, `recovered/` + toast (§6.3); in-main editor route removed outright (§1, §11 E2).
