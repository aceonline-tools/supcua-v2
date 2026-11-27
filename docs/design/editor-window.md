# Editor window — design contract (v1.6.0)

Status: **contract rev 3.3 (E1a.1)**, revised after reviews of ff96774 and ccb9008. Frozen once the Lead accepts it; later changes need a REOPEN.
> **Changelog — rev 3.3 (E2).** §3.1 `editor.open` gains two additive `EditorSession` fields, `isNew:boolean` (host created the script: New vs Edit title) and `groups:string[]` (existing group names for the combobox; `scripts.list` stays forbidden for the editor); `TransitionalMain` is emptied now that the in-main editor route is gone (main can call no editor-only method).
>
> **Changelog — rev 3.2 (E1b review of 5c451dd).** §6.5 adds amendment I (sealed final snapshot; a late publish becomes its own file-only recovery file), J (one admission lock for "a newer generation opened this script" and recovery's enqueue), the late-file handshake, and the note that deadlines bound cleanup, not abandoned workers. §6.3 recovery file names gain the revision and a collision suffix.
>
> **Changelog — rev 3.1 (E1b repair, Lead-authorized amendments A–H).** §6.2/§6.5 recovery persistence is a store compare-and-swap (A) with a bounded enqueue deadline (B) and an asynchronous recovery file (C); saves publish their payload as a draft at entry (D); teardown detaches the session before the native state changes (E); cleanups own unique tokens (F); cleanup steps are bounded or asynchronous (G); §7 documents the explicit, logged shutdown escape (H). "Nothing writes after D2" is withdrawn: nothing is **enqueued** after D2.

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
- **When a gen becomes invalid:** `invalidate(gen)` runs on the UI thread, **immediately before** it posts the cleanup job, and in the same step marks `Window.closed`. A request of that gen dequeued after this point is rejected. A request already running is **not** stopped by `invalidate`: it is fenced by the per-session `closing` state and save lease (§6.5), which is the only thing that guards the existing commit check (`bridge/editor.go:154`, `b.session != sess`). Its reply is dropped (§2.3 delivery check).
- Closing the editor never closes the dispatcher; only app shutdown does.

### 2.4 Event routing (`bridge.emitTo(Target, …)`, Target ∈ Main|Editor|Both)

| Event | Target |
|---|---|
| `state`, `scripts.changed`, `settings.changed`, `save.failed` | Both |
| `update.*`, `nav.goto` | Main |
| `editor.reset`, `editor.closeRequested`, `editor.measureRequest`, `editor.measureAbort`, `capture.hotkey`, `capture.key`, `capture.mouse`, `capture.ended` | Editor — **destination (hwnd, gen, sessionId) is captured when the event is emitted**, so a late capture event for a destroyed editor is dropped, never delivered to a replacement |
| `editor.windowFailed`, `editor.windowClosed` | Main |
| `app.closing` | Both |

## 3. RPC contract

Wire conventions unchanged (`{id,method,params}` → `{id,result|error}`). Method names are the registered ones (`bridge/bridge.go:585-620`). Identity/generation are caller context (§2.3), not params. New error codes: `FORBIDDEN_ROLE`, `STALE_GEN`, `SHUTTING_DOWN` (exists as stopping), `DRAFT_TOO_LARGE`, `EDITOR_WINDOW_BUSY`, `SESSION_CLOSING`.

### 3.1 New / changed methods

| Method | Role | Params | Result |
|---|---|---|---|
| `editor.openWindow` | main | `{id?: string}` | `{status:"opening"\|"focused", gen:number}`. Existing editor (any state ≥ Creating) → `focused`: restore if iconic, activate, no new session, for any `id` (main toasts "Editor already open"). `SHUTTING_DOWN` during shutdown. |
| `editor.windowReady` | editor | `{}` | `{}` — page booted; informational. |
| `editor.open` | editor (page, not main) | `{id?: string}` (unchanged) | `EditorSession` unchanged (+`revision:number`, the session's current revision, §6.1). Rejected `STALE_GEN`. |
| `editor.measured` | editor | `{token:number, cssW:number, cssH:number, dpr:number}`; ints 1..8192 | `{next:"show"\|"remeasure", token?:number, cssW?:number}` — `token` and `cssW` are present iff `next:"remeasure"`. **Exactly one result per call:** the handler waits on a completion channel for the UI-thread decision, bounded 2 s (§5.4); on that bound the result is `{next:"show"}` and the fallback applies. |
| `editor.measureAck` | editor | `{token:number}` | `{}` — page has left `data-measure` (page → host ack; distinct from any host instruction). |
| `editor.checkpoint` | editor | `{sessionId, revision:number, script}` | `{applied:boolean, revision:number}` — `applied:false` is a **no-op success**: `revision ≤ max(stored checkpoint revision, lastSavedRev)` (§6.2). |
| `editor.guardActive` | editor | `{sessionId}` | `{leaseMs:number}` or `STALE_GEN`/`EDITOR_CLOSED`. Renews the close lease (§4.5). Rejected during shutdown (`SHUTTING_DOWN`). |
| `editor.closeDecision` | editor | `{sessionId, decision:"saved"\|"discard"\|"cancel"}` — **no script, no save**: the host never saves on this call (§4.3) | `{}`; `EDITOR_CLOSED`/`STALE_GEN`/`SESSION_CLOSING` otherwise. `saved` is accepted only if `sess.lastSavedRev ≥ sess.dirtyRev` (every edit the page reported is persisted), else `BAD_REQUEST`. |
| `editor.save` | editor | **strict-decode change** (`bridge/editor.go:76-81` struct + `bridge/errors.go:88` strict decode): existing `sessionId, script, overrideConflicts, ignore_conflicts` **plus new optional `snapshotRev:number`** (required by the E2 frontend; absent ⇒ treated as `sess.dirtyRev+1`, i.e. old callers keep working). | `saveResult` (`bridge/editor.go:71`) **plus `savedRev:number`** (= the request's `snapshotRev`); the existing `rev` stays the global scripts revision. `SESSION_CLOSING` / `EDITOR_CLOSED` after cleanup began. |
| `editor.requestClose` | editor | `{}` | `{}` — same path as WM_CLOSE (§4.3): emits `editor.closeRequested`, starts the §4.5 timers; no-op if not `Open` or already `CloseRequested`. |
| `editor.close` | host-internal only | — | **No longer registered for pages.** The existing handler body becomes the internal `closeSession(sess, mode)` called by the host. A page call → `FORBIDDEN_ROLE`. |

### 3.2 Exhaustive role matrix

| Methods | main | editor |
|---|---|---|
| `app.boot`/`app.ready`/`state.get`, `actions.catalog`, `clipboard.write`, `settings.get` | ✓ | ✓ |
| `app.closingReady` | ✓ | ✓ (§7) |
| `editor.openWindow` | ✓ | ✗ |
| `editor.windowReady/open/measured/measureAck/checkpoint/guardActive/closeDecision/save`, `editor.describeActions`, `editor.toJSON`, `editor.fromJSON` | ✗ | ✓ |
| `capture.hotkey.start`, `capture.key.start`, `capture.mouse.start`, `capture.mouse.stop`, `capture.stop` | ✗ | ✓ |
| `scripts.*`, `settings.update`, `settings.setSidebarCollapsed`, `overlay.*`, `autoactive.set`, `update.*`, `window.*`, `app.setGlobalActive`, `app.exit` | ✓ | ✗ (`FORBIDDEN_ROLE`) |

An unlisted method is **denied** for the editor (default deny). Role comes from `Caller`, not params.

### 3.3 Events (typed)

```ts
editor.closeRequested {gen:number, sessionId?:string, reason:"user"|"app-closing"|"owner-closing"}
editor.windowFailed   {gen:number, stage:"create"|"embed"|"timeout"|"navigate"|"measure"|"renderer", message:string, recoveredPath?:string}
editor.measureRequest {token:number}                                  // host → page, initial and every retry
editor.measureAbort   {reason:"fallback"|"dpi"|"timeout"}            // host → page: leave data-measure now
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

### 4.3 Single authoritative close path

**One save, performed by the page; one cleanup, performed by the host.**

1. `WM_CLOSE`/Alt-F4/X on the editor (`hostProc` today destroys via `u.Quit`, `ui/webview/ui_windows.go:492`): the editor branch returns 0, state → `CloseRequested`, emit `editor.closeRequested`, start the close timers (§4.5). Repeats do not re-emit nor reset timers.
2. The page's existing guard (`editor.svelte.ts` `close()`) runs **its own save through `editor.save`** with today's flow (hotkey conflict dialog → `overrideConflicts`, `ignore_conflicts` flush, result handling) and does **not** call `editor.close`/`finish()` any more. Only when the guard is finished does it send:
   - `closeDecision{decision:"saved"}` — after `editor.save` succeeded (host check: `lastSavedRev ≥ dirtyRev`);
   - `closeDecision{decision:"discard"}` — the user chose Discard, or the editor is clean;
   - `closeDecision{decision:"cancel"}` — the user chose keep editing: state → `Open`, **this close attempt is cancelled** (all timers cleared). The 60 s ceiling restarts only on a **new** close request after returning to `Open`.
3. The host **never saves on `closeDecision`**. Conflict, validation and name errors are returned to the page by `editor.save` (unchanged error shapes, `bridge/editor.go:95-190`); the page shows its dialog, may call `editor.save` again with `overrideConflicts`, and sends `closeDecision` only afterwards. The lease (§4.5) keeps the attempt alive only while `guardActive` pings arrive.
4. On `saved`/`discard` the host runs **cleanup** (`closeSession`): set `closing` (§6.5), end capture, drop session + checkpoint, `invalidate(gen)`, `endModal()`, `SetEditDialogOpen(false)`, destroy (§4.5), emit `editor.windowClosed`. Idempotent. **Successful cleanup itself marks the editor ready for shutdown (§7):** the page never sends `app.closingReady` after a `closeDecision`.
5. **Force close** (watchdog, shutdown deadline, `failEditor`) never calls `editor.save`: it uses recovery (§6.3).

### 4.4 (merged into §4.3)

### 4.5 Close timers: base deadline, renewable lease, ceiling

Timers exist **only** in `CloseRequested` (never while `Open`). Let `T0` = first close request after entering `CloseRequested`.

- **Base deadline** `T0+10 s`: if no `closeDecision`/`guardActive` arrived, force-close.
- `editor.guardActive` (sent by the page every ≤1 s while a guard dialog is open **and** the page is responsive): grants a **lease of 3 s** from receipt. Deadline = `min(T0+60 s, max(base, now+3 s))`. A hung page cannot renew → the base/lease expires.
- **Absolute ceiling** `T0+60 s`, never extended.
- **Never extended during app shutdown**: `guardActive` → `SHUTTING_DOWN`; shutdown uses its own deadlines (§7).
- A valid `closeDecision` ends the lease; `save` failure with a conflict re-arms it only through new `guardActive` pings (so an unresponsive page after a failed save still force-closes at the lease/base).
- **Force close** (`forceClose(gen)`), timeline from `T0`: the watchdog **fires** at `T_f` (`T0+10 s` base, or the lease/ceiling deadline); **teardown completes by `T_f+1 s`**.
  1. `T_f` (UI thread): `invalidate(gen)`; set `closing` via `b.sessMu` **try-lock 100 ms** — if the lock is held by a worker, set the lock-free atomic `sess.closingFlag` instead (the save lease honours either, §6.5); `endModal()` (re-enable main); `SetEditDialogOpen(false)` is posted to the recovery goroutine (it takes `sessMu`), not run on the UI thread; **post** `browser.Close()` to the UI message queue as a deferred call (never a synchronous COM call inside the timer handler) and `DestroyWindow(hwnd)` right after posting.
  2. Recovery goroutine (not the dispatcher worker): §6.3, bounded 2 s.
  3. **Native destroy never waits** on `sessMu`, the save lease, the dispatcher, a capture-stop join, or recovery. The only UI-thread blocking calls are non-COM Win32 (`EnableWindow`, `DestroyWindow`).
  4. **If `Close()` hangs** (COM never returns or the renderer ignores it): it is posted, so it blocks nothing; the HWND is already destroyed; the controller and renderer process linger as a **dangling renderer that dies at app exit** (acceptable, logged at 5 s). `windowClosed` is emitted at HWND destroy, not at `Close()` completion.
  Hence: guarantee = main re-enabled and editor HWND gone by `T_f + 1 s`; persistence is bounded separately (≤2 s after `T_f`).

### 4.6 Generation tokens

`gen` (monotonic `uint64`) is assigned at `openWindow` and stored on `Window` and `editorSession`; the nonce in the bootstrap script must match. Native callbacks (controller completion, `ProcessFailed`, timers, `NavigationStarting`) capture `gen` and no-op on mismatch (releasing resources they created). Validation points: §2.3.

### 4.7 Failure in every state (runtime and startup)

`failEditor(gen, stage, cause)` runs on the UI thread from any state (`Creating`, `Loading`, `Measuring`, `Open`, `CloseRequested`, `Closing`):

1. `invalidate(gen)`; stop timers.
2. If a session exists: recover from the latest checkpoint (§6.3) on the recovery goroutine (best effort, bounded 2 s).
3. End capture, drop session, `SetEditDialogOpen(false)`, `endModal()`, destroy the editor window (or release its pending controller), restore focus per §4.2.3.
4. Emit `editor.windowFailed{gen,stage,message,recoveredPath?}` to main (payload in §3.3), which toasts.

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

On `?window=editor` the page boots in measure mode (`<html data-measure>`: intrinsic layout, `height:auto`, `width:max-content`, no `100vh`/`1fr`, actions pane at `--actions-pref-h`, compact density), runs `editor.windowReady` + `editor.open`, then **waits for the host event `editor.measureRequest{token}`** (the initial token is delivered only there). On it: wait for the session render, `document.fonts.ready`, two rAF, measure the root's border box (`ceil`), call `editor.measured{token,…}`. The reply `{next, token?, cssW?}` drives it: `remeasure` → set root width to `cssW`, two rAF, measure again, call `editor.measured` with the **new** `token`; `show` → leave `data-measure` and call `editor.measureAck{token}`. The page also leaves `data-measure` and sends `editor.measureAck` on `editor.measureAbort`. A `measureRequest` with a newer token supersedes any measurement in progress (old replies ignored by token). Leaving measure mode must not change the window size.

### 5.3 Host side — target monitor and DPI first

1. Choose the **target monitor**: remembered bounds' monitor if valid (§9), else the owner's monitor.
2. Position the **hidden** HWND on that monitor (default rect at the monitor's work-area centre), let `GetDpiForWindow` settle on that monitor (`WM_DPICHANGED` for the hidden window is processed before measuring is accepted).
3. Only then accept `measured` (its `token` equals the current measurement token); convert: `scale = dpi/96`, `clientPx=ceil(css*scale)`, `outer = client + AdjustWindowRectExForDpi frame`, clamp to the target work area (min = `min(scaledMin, work area)`), centre on owner unless remembered.
4. Reply `{next:"show"}` or `{next:"remeasure", token, cssW}` (§3.1).

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
  H-->>E: event editor.measureRequest{token}
  E->>E: data-measure; fonts.ready; 2×rAF; measure
  E->>W: editor.measured{token,cssW,cssH,dpr}
  W->>H: size job (await)
  H->>H: dpi→px, +frame, clamp
  alt clamped narrower, or DPI changed (budget left)
    H-->>E: reply {next:remeasure, token', cssW}
    E->>W: editor.measured{token',…}
  end
  H-->>E: reply {next:show}
  E->>W: editor.measureAck{token}
  H->>H: SetWindowPos; ShowWindow; presentSurface; activate
```

### 5.4 One retry policy, timeouts, fallback

- **Budget:** at most **2 extra rounds** (initial + ≤2 retries), shared by width remeasure **and** DPI change. Every retry issues a **new token**; the previous token is dead. A `WM_DPICHANGED` while Measuring consumes one round: the host sends `editor.measureRequest{newToken}`; a `measured` with a dead token gets `{next:"remeasure", token:<current>, cssW}` only if budget remains, else `{next:"show"}`.
- **Timeouts:** the host starts a **5 s timer per round** (cleared by the matching `measured`) and a **12 s total** cap from `windowReady`. The `measured` handler's own wait for the UI-thread decision is 2 s (§3.1).
- **Fallback** (budget exhausted, any timer expired, out-of-range report, page error): size **960×720 CSS px** clamped like any size; log line. The host then (a) answers a pending `measured` with `{next:"show"}`, or else (b) sends `editor.measureAbort{reason}` to the page, which leaves `data-measure` and sends `editor.measureAck`.
- **Hung page** (no `measured`, no `measureAck`): the host **shows anyway** at the fallback size 1 s after the abort/instruction; the close watchdog (§4.5) covers any later hang.
- **Remembered bounds** (§9): the window opens there; the page is still sent `measureRequest` only to learn when to leave `data-measure`; the reported size is ignored.

## 6. Draft checkpoints and recovery

### 6.1 One revision per session

A single monotonically increasing **`revision`** (JS-safe integer `1..2^53−1`; beyond it `BAD_REQUEST`), assigned by the **page** per edit snapshot, starting from the `revision` returned by `editor.open`. It is used by `editor.checkpoint.revision` and `editor.save.snapshotRev`; `editor.save` returns `savedRev` (the same value). The global scripts revision (`saveResult.Rev`, `bridge/editor.go:71,168`) keeps its meaning and is unrelated. Backend session fields: `lastSavedRev`, `dirtyRev` (highest revision reported by checkpoint or save), `checkpoint{revision, script, valid}`, `draft` (the failed-save draft, `bridge/editor.go:19`) stamped with the failing save's `snapshotRev`.

### 6.2 Checkpoint rules

- Page sends debounced (500 ms trailing, 2 s max-wait), one in flight, only when dirty. **1 MiB** per message for every draft kind (checkpoint, failed-save draft, recovered file); over → `DRAFT_TOO_LARGE`, previous checkpoint kept.
- Accept only `revision > max(checkpoint.revision, lastSavedRev)`; otherwise **no-op success** `{applied:false}`.
- A successful `editor.save` sets `lastSavedRev = snapshotRev` (inside the commit critical section, `bridge/editor.go:152-165`) and clears the checkpoint and draft with `revision ≤ lastSavedRev`.
- `scriptID` must equal the session's; invalid scripts are stored with `valid=false`.
- `RecoverDraft` returns the newest (by revision) of {checkpoint, entry-draft, failed-save draft} with `revision > lastSavedRev` (**amendment D**: every save publishes its payload as the session's newest draft at save **entry**, before any check that can reject it — including a save refused with `SESSION_CLOSING` by the closing fence — so a save rejected late is never lost).

### 6.3 Recovery and persistence

- Valid + non-conflicting: autosaved through `saveScript` (same path as a user save).
- Invalid or conflicting: **never written into `scripts/`** and never overwrites the stored revision. Written to `<state-dir>/recovered/<scriptID>-<unixms:13 digits>-<revision:10 digits>[-n].json` (reserve-then-rename: the final name is created with `O_CREATE|O_EXCL`, retrying `-n`, so no two writers own one name on any filesystem; the content goes to a synced temp file renamed over that reservation only. A crash leaves the empty reservation or the complete file, never a partial one. Pruning orders numerically by (ms, revision, n) and ignores names it cannot parse. 0600, ≤5 per script, oldest pruned) with the envelope:
  ```json
  {"v":1,"scriptId":"…","revision":N,"savedAt":"RFC3339","script":{…}}
  ```
  (`model.Script` has no version field, so the envelope carries `v`.) The path is returned in `editor.windowClosed.recoveredPath` and toasted on main. No auto-reapply, no Recover UI in 1.6.0.
- **Persistence path bypasses the mutation fence:** `recoverPersist` does not go through `fenceMutations`/`hard` (`bridge/bridge.go:502-520`, `hard` set at `:402-413`). Ordering and ownership rules are in §6.5.

### 6.5 Session detach, save lease, and persistence ordering

State on `editorSession`: `closing` (bool, set atomically under `sessMu`; plus an atomic mirror `closingFlag` for the lock-free path in §4.5), `saving` (counter of in-flight saves), `lastSavedRev`.

1. **`editorSave` holds a session lease for its whole path** (`bridge/editor.go:76` → `saveScript` `:95-190`): under `b.mu`, `requireSession` must see `!closing` else `SESSION_CLOSING`; it then increments `saving`; `defer` decrements. The lease spans check → validate → commit (`:152-165`) → `b.queue(changed)` (`:172`) → `FlushSaves` (`:175`) → result. The commit check at `:154` additionally re-tests `closing`.
2. **Cleanup start (`closeSession`, `failEditor`, forceClose recovery)** sets `closing` atomically (under `sessMu`, or `closingFlag`) **before** anything else. From then on new saves get `SESSION_CLOSING`. Cleanup then **waits at most 500 ms** on `saving == 0` (condition variable on `b.mu`); it never waits longer and **native destroy never participates** in this wait.
3. **Outstanding persistence queued before `closing` belongs to the store** (`Store.QueueSave`/`FlushSaves`): it completes normally; it is not cancelled or re-ordered. A save whose commit ran before `closing` is a normal save and is reflected in `lastSavedRev`.
4. **Recovery snapshot:** after step 2 (or its 500 ms bound), recovery takes under `b.mu` the newest of checkpoint/draft (§6.2) and `lastSavedRev`, and **always writes the recovery FILE** (no lock needed).
5. **Recovery also persists to the script only if** `saving == 0` **and** the snapshot's `revision > lastSavedRev` **and** the draft is valid and non-conflicting; it then runs `saveScript` through a dedicated entry that skips the session `closing` rejection (it carries a one-shot recovery token) and sets `lastSavedRev`. Otherwise **recovery-file-only**. A recovery persistence can **never overwrite a newer revision**: the `revision > lastSavedRev` test and the commit are under the same `b.mu` critical section (`bridge/editor.go:152-165`).
6. If the 500 ms wait expires with `saving > 0`, recovery is **file-only**; the late save completes normally under rule 3.

**Amendments (rev 3.1) — these replace rules 4–5 where they differ:**
- **A. Compare-and-swap persistence.** The session captures the store's revision (etag) of its script at `editor.open` and updates it on each of its own enqueues. Recovery persists into the script only if, checked atomically at enqueue under the **store's own queue lock** (`RevStore.QueueSaveIf`), the store's current revision still equals the etag (nobody else saved or deleted it — Delete leaves a tombstone revision) **and** no newer editor generation has that script open (lock-free `activeScript`, or a live session for it). Otherwise **file-only**. This replaces "`lastSavedRev` only". A store without `RevStore` is file-only.
- **B. Deadline semantics.** Recovery must **enqueue** before its deadline (D2 at shutdown, else teardown + 2 s). A three-state CAS (pending → enqueued by the admit check under the store lock, pending → abandoned at the deadline) guarantees exactly one outcome, so a recovery not enqueued by the deadline can never enqueue later. Anything enqueued is flushed by the existing store machinery / `quiesce` (bounded by `quiesceGrace`). Recovery never waits for the flush.
- **C. Recovery file** is written on its own goroutine. Cleanup waits for it at most until the deadline; the gate release never waits on it. A late write is acceptable and is logged.
- **E. Detach before state change.** Teardown (UI thread) sets `closingFlag` and an atomic `detached` flag and clears the lock-free mirror **before** the native state returns to None; every lookup under `b.mu` drops a detached session, so B's `editor.open` never sees A (no `EDITOR_BUSY`). The lease wait runs on the detached object; saves to it get `SESSION_CLOSING`.
- **F. Per-cleanup ownership.** Each teardown with a session registers a unique token; the gate (`SetEditDialogOpen(false)`) is released only when the token set is empty **and** no generation is active. Teardowns without a session register none and release only under the same condition. `editor.close` and `ResetSession` release through the same check.
- **I. Sealed snapshot.** Recovery sets the session's atomic `sealed` flag and then loads the final snapshot; every entry publication stores its snapshot and then reads `sealed`. With sequentially consistent atomics a payload is in the final snapshot or its publisher sees `sealed` and writes the payload (same envelope, its own revision) as an additional **file-only** recovery file, logged. Late payloads go to **one writer per session**, coalesced to the newest revision (a payload not newer than the sealed snapshot or a pending late one is superseded by it), so a page flooding saves after close cannot spawn unbounded writers. Nothing is logged while `lateMu` is held, so a stalled log sink cannot stall a seal. Nothing is dropped; a duplicate file is possible and harmless.
- **J. Atomic admission.** `openWindow` registers its script under `admitMu`; recovery holds `admitMu` across its "no newer generation has this script" check and the compare-and-swap enqueue. Either recovery enqueues first (B's `editor.open` then sees the recovered script and the store revision it produced as its etag), or B registered first and recovery is file-only. Lock order: `admitMu → store queueMu` (`QueueSaveIf` takes the store's queue lock beneath it); nothing is taken under `queueMu`, and no I/O happens under either lock.
- **Late file handshake.** The file writer and the deadline race through one CAS; whichever loses knows it: a write completing after cleanup gave up is always logged.
- **Abandoned workers.** Deadlines bound **cleanup** (gate, `EnableMain`, events, shutdown), not the workers it abandoned: a recovery goroutine blocked on a lock, a stalled filesystem write or a stalled capture stop may outlive cleanup. They can no longer enqueue (B, I), and they only log when they finish.
- **G. Bounded cleanup.** Cleanup takes no `sessMu`/`b.mu`; `capture.EndSession` runs asynchronously; persistence and the file run on goroutines bounded by the deadline. `EnableMain` and the gate never wait on them.
7. `ResetSession` (`bridge/bridge.go:430-442`) is changed to use the same `closing`/detach protocol; it keeps its blocking `sessMu.Lock()` only on its own goroutine (never on the UI thread, never in destroy).

### 6.4 Idempotence

`closeSession`, `failEditor`, `forceClose`, rollback are idempotent and drop the checkpoint with the session. Late `checkpoint`/`save`/`closeDecision`/`guardActive`/`closingReady` after cleanup → `EDITOR_CLOSED`/`STALE_GEN`, no side effect.

## 7. Shutdown

`S` = start of `BeginShutdown` (`bridge/bridge.go:373`, UI thread). Three absolute deadlines:

| Deadline | Time | Requirement |
|---|---|---|
| **D1 cooperative ack** | `S+1.5 s` (existing `main.go:419`) | editor present ⇒ the **editor's readiness** (below) is required; main's ack alone does **not** release |
| **D2 recovery complete** | `S+2.5 s` | recovery (§6.3/§6.5) enqueued or abandoned (file-only); an enqueued write may still land during `quiesce` (amendment B) |
| **D3 hard destroy** | `S+3.0 s` | editor HWND destroyed, main re-enabled (UI thread, independent of everything else) |

**Editor readiness** = (a) host cleanup of the editor completed successfully (`closeSession` finished after a `closeDecision`), **or** (b) force-close/destroy done. A closed editor never sends `app.closingReady`; the readiness flag is set by cleanup itself. Main's readiness = its `app.closingReady` (`bridge/bridge.go:416`).

**Barrier and arithmetic.** `then()` runs when **both** hold: `quiesce` done **and** the editor destroyed.
- Recovery starts at `min(D1, last required ack)` and is bounded by D2 (`≤ S+2.5 s`).
- `quiesce` starts **at recovery completion** (≤ D2), and is bounded by `quiesceGrace = 3 s` (`bridge/bridge.go:397`): it ends by `S+2.5+3.0 = S+5.5 s`.
- Editor destroy runs **concurrently on the UI thread** (D3 = `S+3.0 s`) and finishes inside the quiesce window.
- **Worst case from `S` to `then()` = 1.5 (D1) + 1.0 (recovery to D2) + 3.0 (quiesce) = 5.5 s.** If destroy were serialised before quiesce it would be 6.0 s (destroy ≤0.5 s after D2 ≈ `S+3.0`, quiesce `S+3.0+3.0`), which is why they overlap. Old worst case: 1.5 + 3.0 = 4.5 s.
- Sequence: `app.closing{deadline_ms:1500, roles}` to Both; the editor (if Open) enters `CloseRequested(reason:"app-closing")`; the page runs the guard (its own `editor.save` flush, which shutdown allows), then `closeDecision`; host cleanup marks the editor ready. `guardActive` → `SHUTTING_DOWN` (no lease). `closeDecision:"cancel"` during shutdown is treated as `discard` + recovery of the draft.
- Shutdown in Creating/Loading/Measuring: `failEditor` (no toast); no ack awaited. During save: the lease (§6.5) bounded 500 ms. During capture: `capture.End`, stop handled off the UI thread.
- `shutdownAllowed` (`bridge/bridge.go:494`) becomes `editor.save`, `editor.closeDecision`, `editor.checkpoint`, `app.closingReady`; `editor.close` is no longer a page method.
- `Dispatcher.Close()` only after the editor is destroyed.
- **Amendment H — bounded escape.** The process must be able to exit: if the editor's destruction is not confirmed by `D3 + 0.5 s` (a wedged UI thread), shutdown logs `editor destroy not confirmed; proceeding` and continues past the barrier. This is the only exception to the AND barrier.

## 8. Capture, hotkeys, tray, minimise

- **Capture targeting:** `CaptureBackend.HWND` → `ActiveModalHWND()` (the editor's hwnd while a session/editor exists, else main), evaluated per event. **Mouse capture is gated too:** `StartMousePick` gets the same `ForegroundIs(ActiveModalHWND)` predicate (today it is ungated); a mouse press while another app is foreground is ignored. Every capture event is stamped with the (hwnd, gen, sessionId) at emission (§2.4). Capture ends on any editor teardown (`closeSession`/`failEditor`).
- **Hotkeys:** script hotkeys stay blocked from creation start to cleanup (§4.2.1); the emergency and overlay toggles stay functional (real-input evidence in E1c).
- **Tray Show:** restores main **and** restores/activates **any** existing editor: `SW_SHOW` if hidden, `SW_RESTORE` if iconic (never un-maximizes a visible maximized window). It does not depend on `hiddenWithOwner`. Focus goes to the editor (main is disabled).
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
| **E1b** session/watchdog/shutdown/checkpoints | state machine, modality, `openWindow/closeDecision/measured/measureAck/checkpoint/guardActive`, close timers, `failEditor`, recovery + `recoverPersist`, shutdown D1–D3, bounds maths, config, capture targeting incl. mouse gate, tray/minimise, **native host files**: `ui/webview/{window,host}_windows.go` (timers, WM_CLOSE/DPI/DISPLAYCHANGE), `ui/webview/hotkey/capture*` (mouse gate), tray handler | `bridge/editor.go`, new `bridge/{editor_window,checkpoint,recover}.go`, `bridge/bridge.go` (shutdown), `model/config.go`, `ui/capture_backend_windows.go`, `ui/webview/*_windows.go`, tray file, `main.go` (`Recovery` role-awareness) |
| **E2** frontend editor mode | `?window=editor`, intrinsic measurement + `data-measure`, checkpoint sender, decision-only close (remove `finish()`/`editor.close` from the page and the in-main editor route), `guardActive` pings, `editor.closeRequested` | `frontend/src/lib/{editor.svelte,store.svelte}.ts`, `frontend/src/components/EditorScreen.svelte`, `frontend/src/api/{validate,types}.ts`, tests. **Not** `api/mock.ts` (E3's). |
| **E1c** native Windows evidence | host-fixture evidence may start after E1b; **final integrated acceptance runs after E2** | `.github/workflows/*selftest*`, `tools/selftest/**`, a **measurement hook** (`window.__editorMetrics()` exposing `{scrollW,clientW,scrollH,clientH,lastBlockBottom}`) and an **input hook** (a SendInput driver) agreed between E2 and E1c before E2 starts |
| **E3** simulator | second window in dev sim and Vercel | `tools/sim/**`, `frontend/src/api/mock.ts`, vercel config. Starts after E2. |
| **Integrated acceptance** | one pass on the real app after E3 | CI artifacts (§13) |

Ordering: E1a.2 → E1b → E2 → E3; E1c host-fixture alongside E1b/E2, final E1c after E2; integrated acceptance last. `ui/webview/serve*` untouched.

## 12. Acceptance tests

**U** = unit/headless; **W** = native Windows CI (E1c); **B** = real browser (Playwright, not jsdom); **H** = hardware/manual evidence (windows-latest has no multi-monitor/mixed-DPI).

**E1a.2:** U dispatcher: per-request reply on success/error/BAD_REQUEST/BUSY; closed sink drops; `Caller` carried; dequeue-time gen rejection; `Close()` unaffected by window close; U role matrix incl. default-deny; U `emitTo` table and capture destination capture; W both windows render (non-white), per-window navGate counts, Reassert for both.

**E1b:** U state machine incl. illegal transitions and `failEditor` from every state; U close timers: none while Open; base 10 s; lease 3 s renew; 60 s ceiling; repeated requests do not reset; cancel returns to Open and a new request restarts the ceiling; `guardActive` rejected in shutdown (fake clock); U single close path (page never releases; double cleanup no-op); U revision ordering, clearing on save, `{applied:false}`, 1 MiB for all draft kinds; U recovery valid/invalid/conflicting + envelope; U shutdown D1/D2/D3 with a stuck worker, main-ack-alone not releasing, worst case ≤5.5 s; U `computeEditorBounds` tables (clamp, retry budget, validity, tiny area, DPI). W: main disabled **before** the editor is interactive and enabled **before** destroy (ordered log); script hotkeys blocked from creation start; focus restore (main visible at open → foreground; main in tray at open → stays in tray); repeated New/Edit restores an iconic editor and keeps maximized; startup failure rollback (injected embed failure + 10 s hang); tray Show restores both and preserves maximized; editor minimise does not hide main; shutdown during creation/save/capture; editor renderer failure while Open (checkpoint recovered, main untouched, no main reload); shared browser-process failure (editor cleaned before main policy); forced close with an unresponsive renderer (checkpoint on disk; watchdog fires at 10 s, teardown done by 11 s; recovered file/revision asserted); reopen after forced close (new gen works, old gen events ignored); capture targeting for key, hotkey and **mouse** (foreground-gated, ignored when another app is foreground); emergency and overlay hotkeys with real input while a script hotkey is blocked. **H:** mixed-DPI move, monitor-disconnect recovery, tiny work area on a real small display (CI simulates tiny via a fake work-area input only).

**E1b (added in rev 3):** U save lease: cleanup during an in-flight save waits ≤500 ms, later saves get `SESSION_CLOSING`, recovery never overwrites a newer `lastSavedRev` (race test with `b.at` hooks at `validated`/`commit`); U `editor.save` with and without `snapshotRev`, result `savedRev`; U checkpoint `≤ lastSavedRev` → `{applied:false}`; U `closeDecision` never saves (exactly one `QueueSave` per close); U cleanup marks editor shutdown-ready without a page `closingReady`; U shutdown barrier (quiesce AND destroy) with destroy and quiesce overlapping: worst case 5.5 s; U measurement token tables (dead token, DPI retry, 12 s cap, fallback → `measureAbort` → `measureAck`, hung page shown after 1 s); W forced close: `T_f` = 10 s, HWND gone and main enabled by 11 s with `Close()` stubbed to hang; W tray Show restores an iconic editor without `hiddenWithOwner`.

**E2:** **B** intrinsic measurement is independent of viewport size (resize the real browser viewport, identical result), bounded actions height, after fonts/render; remeasure/budget; `data-measure` exit incl. fallback and hung host; "no empty space" metrics; reflow at natural/min/work-area (screenshots); U checkpoint debounce/max-wait/revision/clear-on-save; U decision-only close (save/discard/cancel/conflict), `guardActive` ping cadence only while responsive; the in-main editor route is gone (no references).

**E3:** dev sim and Vercel show the editor as a second window with the same handshake; close returns to main.

**Integrated (after E3):** one W run on the shipped binary with the real frontend covering the full E1b W list plus size-fit metrics.

## 13. Windows CI evidence (E1c)

Per scenario (JSON + screenshots as artifacts): editor content/client/outer bounds (px), DPI for editor and main (`GetDpiForWindow`), monitor work area, `devicePixelRatio`, reported `cssW/cssH`, token and retry rounds used, clamp result, no-empty-space metrics (`scrollW/clientW/scrollH/clientH/lastBlockBottom`), `IsWindowEnabled(main)` timeline (before editor show, during, after re-enable, before destroy), foreground window after close, tray state before/after, resize/reflow results at three sizes, close/reopen outcomes (X, Alt-F4, decision paths, watchdog forced, shutdown), recovered file path + content hash, WS/private memory open vs closed, screenshots of main and editor, real-input log for the hotkey scenario. Failures fail the job. H items are attached as a manual evidence note with screenshots.

## 14. Open questions

None blocking. Decided and folded in: failure-only fallback 960×720 (§5.4); guard lease 3 s / ceiling 60 s (§4.5); focus-only for a second New/Edit (§3.1, §4.2); no Recover UI, `recovered/` + toast (§6.3); in-main editor route removed outright (§1, §11 E2).
