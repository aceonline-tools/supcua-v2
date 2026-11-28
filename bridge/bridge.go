// Package bridge is the platform-neutral backend of the WebView2 UI: it implements every API
// method and event of the contract (docs/design/phase2-architecture.md §9) on top of app/,
// storage/, model/ and autoactive/. The host owns the dispatcher, the single RPC worker goroutine
// and the JS transport; handlers returned by Methods are called only from that worker.
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yanmad27/supcua-v2/app/status"
	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/updater"
)

// Handler is one API method. Registered as an alias so the host needs no named type.
type Handler = func(params json.RawMessage) (any, error)

// Emitter delivers events to JS. Emit must not block and may be called from any goroutine.
type Emitter interface {
	Emit(event string, payload any)
}

// UIThread posts work to the UI thread (non-blocking).
type UIThread interface {
	Post(func())
}

type Controller interface {
	SetGlobalActive(active bool)
	Snapshot() status.Snapshot
	Subscribe(fn func(status.Snapshot))
	SetEditDialogOpen(open bool)
	SetHotkeyRecording(on bool)
}

type Store interface {
	List() ([]model.Script, error)
	Delete(id string) error
	NameOrFileTaken(name string) bool
	NameOrFileTakenExcept(name, excludeID string) bool
	QueueSave(scripts ...model.Script)
	FlushSaves(timeout time.Duration) bool
	SetOnSaveError(hook func(script model.Script, err error))
}

// RevStore is the optional compare-and-swap surface of a Store (editor-window §4.5 amendment A).
// Without it, editor recovery never persists into a script (file only).
type RevStore interface {
	Revision(id string) uint64
	QueueSaveIf(id string, expect uint64, anyRev bool, admit func() bool, scripts ...model.Script) (uint64, bool)
}

type Config interface {
	Get() model.AppConfig
	Update(fn func(*model.AppConfig)) error
}

type HotkeySet interface {
	SetScripts(scripts []model.Script)
}

// HotkeyMute mutes and restores all registered hotkeys without unregistering them.
type HotkeyMute interface {
	SetSuspended(on bool)
}

type AutoActive interface {
	Start()
	Stop()
	SetProcessName(name string)
}

// LockState reports the executor's input-lock state; safe from any goroutine.
type LockState interface {
	MouseLocked() bool
	KeyboardLocked() bool
}

// Overlay is implemented by the host. Methods are invoked on the UI thread (via UIThread.Post),
// except State, which is also called once from New and from Shutdown (UI thread).
type Overlay interface {
	State() OverlayDTO
	SetEnabled(enabled bool) error
	SetVisible(visible bool)
	SetDraggable(draggable bool)
	// ApplyStyle applies the style live and returns the effective (screen-clamped) position.
	ApplyStyle(style model.OverlayStyle, x, y int) (int, int, error)
}

// Hotkey kinds accepted by Host.RebindHotkey.
const (
	HotkeyGlobal  = "global"
	HotkeyOverlay = "overlay"
	HotkeyCapture = "capture"
)

// Host groups the remaining host-owned operations. Hide, Show and Exit are invoked on the UI
// thread via UIThread.Post; the others are called from the RPC worker (RebindHotkey must restore
// the previous registration itself when it fails) or the update goroutine (ApplyUpdate).
type Host interface {
	Hide()
	Show()
	Exit()
	WriteClipboard(text string) error
	BootInfo() BootState
	RebindHotkey(kind string, hk model.Hotkey) error
	// ApplyUpdate installs the current offer; a non-empty version must match it (else ErrStaleOffer).
	ApplyUpdate(version string, progress func(pct int)) error
	// SkipUpdate clears the offer only while it is still the given version (empty: unconditionally).
	SkipUpdate(version string)
}

// UpdateCheckResult is the update.check reply. Status is "up_to_date", "available" or "error";
// Error is "offline", "rate_limited" or "other".
type UpdateCheckResult struct {
	Status  string `json:"status"`
	Current string `json:"current"`
	Tag     string `json:"tag,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Deps are the collaborators of a Bridge. Controller, Store and Config are required.
type Deps struct {
	Controller Controller
	Store      Store
	Config     Config
	Hotkeys    HotkeySet
	AutoActive AutoActive
	Locks      LockState
	Overlay    Overlay
	UI         UIThread
	Capture    CaptureBackend
	// HotkeyMute (optional) mutes every registered hotkey while the Settings recorder runs.
	HotkeyMute HotkeyMute
	Host       Host
	// CallerRole names the window a call came from ("main"|"editor"); set by the host transport.
	CallerRole func(ctx context.Context) string
	// UpdateCheck (optional) runs a release check: the new tag and download URL, or an error kind
	// ("offline", "rate_limited", "other"). Wired to updater.CheckKind.
	UpdateCheck func(current string) (tag, dlURL, errKind string)
	// NewDeferred (optional) returns a handler result that the host's dispatcher completes later
	// through resolve, so a slow handler does not hold the single RPC worker. Wired to rpc.NewDeferred.
	NewDeferred func() (reply any, resolve func(out any, err error))
	// Offers (required with UpdateCheck) is the offer store shared with the startup check and the host.
	Offers *updater.OfferStore
}

var uiCallTimeout = 3 * time.Second

const saveWait = 3 * time.Second

// uiCallTimeoutForTest overrides the UI-call timeout and returns the restore function.
func uiCallTimeoutForTest(d time.Duration) func() {
	old := uiCallTimeout
	uiCallTimeout = d
	return func() { uiCallTimeout = old }
}

// Bridge implements the API contract.
type Bridge struct {
	d       Deps
	emitter Emitter
	checker *hotkey.ConflictChecker
	known   map[string]bool
	capture *CaptureManager

	// fence: mutating handlers hold RLock; Shutdown takes Lock so no save is queued after the final flush.
	fence    sync.RWMutex
	hard     atomic.Bool // shutdown ran: every mutating RPC is rejected
	stopping atomic.Bool // shutdown began: mutating RPCs except editor.save/close are rejected
	// sessMu orders editor-session changes together with the edit-dialog-open flag.
	sessMu       sync.Mutex
	qMu          sync.Mutex // orders QueueSave against Shutdown closing the queue
	qClosed      bool
	quiesceFail  atomic.Bool
	testHook     func(stage string) // tests only: called at named interleaving points
	beginOnce    sync.Once
	closingReady chan struct{}
	readyOnce    sync.Once

	initial   []model.Script // the list read once in New; immutable
	initialOK bool           // false when that listing failed

	mu         sync.Mutex
	scripts    []model.Script
	rev        uint64
	failed     map[string]struct{}
	session    *editorSession
	sessAtomic atomic.Pointer[editorSession] // lock-free mirror of session (UI-thread teardown, recovery)
	admitMu    sync.Mutex                    // a generation registering its script vs recovery's admission + enqueue; order admitMu → store queueMu, no I/O
	lastClosed atomic.Pointer[editorSession] // the session a teardown detached last (saves to it get SESSION_CLOSING)
	closed     bool

	winMu        sync.RWMutex
	win          *windowCtl // editor window controller; nil until AttachEditorHost (read it through winCtl)
	recoveredDir string     // where recovered drafts are written ("" disables the file)

	ovMu    sync.Mutex
	overlay OverlayDTO

	updating atomic.Bool

	shutdownOnce sync.Once
	shutdownErr  error
}

// New builds a Bridge, loads the script list and subscribes to the status hub.
func New(d Deps, e Emitter) *Bridge {
	b := &Bridge{
		d:       d,
		emitter: e,
		checker: hotkey.NewConflictChecker(),
		known:   knownActionTypes(),
		failed:  map[string]struct{}{},

		closingReady: make(chan struct{}),
	}
	b.capture = newCaptureManager(d.Capture, b.emit, defaultCaptureIdle)
	b.capture.suspend = func(on bool) {
		if d.Controller != nil {
			d.Controller.SetHotkeyRecording(on)
		}
		if d.HotkeyMute != nil {
			d.HotkeyMute.SetSuspended(on)
		}
	}
	if d.Store != nil {
		if scripts, err := d.Store.List(); err != nil {
			log.Printf("bridge: initial script list failed: %v", err)
		} else {
			b.scripts = scripts
			b.initial = append([]model.Script(nil), scripts...)
			b.initialOK = true
		}
		d.Store.SetOnSaveError(b.onSaveError)
	}
	b.refreshOverlay(false)
	if d.Controller != nil {
		d.Controller.Subscribe(func(s status.Snapshot) { b.emit(EvState, b.stateDTO(s)) })
	}
	return b
}

// InitialScripts is the script list New loaded. The host seeds script-hotkey registration from it
// so startup lists the scripts directory exactly once.
func (b *Bridge) InitialScripts() []model.Script { return append([]model.Script(nil), b.initial...) }

// InitialScriptsOK reports whether New's listing succeeded; when it did not, the host re-lists.
func (b *Bridge) InitialScriptsOK() bool { return b.initialOK }

func (b *Bridge) at(stage string) {
	if b.testHook != nil {
		b.testHook(stage)
	}
}

func (b *Bridge) emit(event string, payload any) { b.emitTo(TargetFor(event), event, payload) }

// emitTo routes an event to the main window, the editor window or both. A single-window emitter
// receives everything (the in-main editor route lives until E2).
func (b *Bridge) emitTo(t Target, event string, payload any) {
	if b.emitter == nil {
		return
	}
	if te, ok := b.emitter.(TargetEmitter); ok {
		te.EmitTo(t, event, payload)
		return
	}
	b.emitter.Emit(event, payload)
}

func (b *Bridge) post(fn func()) {
	if b.d.UI != nil {
		b.d.UI.Post(fn)
		return
	}
	fn()
}

// callUI runs fn on the UI thread and waits (RPC worker only, never from the UI thread). If the
// UI thread does not start the job within the timeout the job is cancelled and never runs; once it
// has started its real outcome is awaited, so a rollback never races a late-running job.
func (b *Bridge) callUI(fn func() error) error {
	if b.d.UI == nil {
		return fn()
	}
	const pending, started, cancelled = 0, 1, 2
	var state atomic.Int32
	done := make(chan error, 1)
	b.d.UI.Post(func() {
		if !state.CompareAndSwap(pending, started) {
			return
		}
		done <- fn()
	})
	timer := time.NewTimer(uiCallTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		if state.CompareAndSwap(pending, cancelled) {
			return newErr(CodeBusy)
		}
		return <-done
	}
}

// refreshOverlay re-reads the overlay state into the cache; viaUI marshals the read to the UI thread.
func (b *Bridge) refreshOverlay(viaUI bool) {
	if b.d.Overlay == nil {
		cfg := b.d.Config
		if cfg != nil {
			c := cfg.Get()
			b.setOverlay(OverlayDTO{Enabled: c.OverlayEnabled, X: c.OverlayPosition.X, Y: c.OverlayPosition.Y})
		}
		return
	}
	if !viaUI {
		b.setOverlay(b.d.Overlay.State())
		return
	}
	var st OverlayDTO
	if err := b.callUI(func() error { st = b.d.Overlay.State(); return nil }); err == nil {
		b.setOverlay(st)
	}
}

// RefreshOverlay re-reads the overlay state and emits state. Call it on the UI thread after a
// drag end, the overlay hotkey or a tray toggle.
func (b *Bridge) RefreshOverlay() {
	b.refreshOverlay(false)
	b.persistOverlayPosition()
	b.NotifyStateChanged()
}

func (b *Bridge) setOverlay(st OverlayDTO) {
	b.ovMu.Lock()
	b.overlay = st
	b.ovMu.Unlock()
}

func (b *Bridge) overlayState() OverlayDTO {
	b.ovMu.Lock()
	defer b.ovMu.Unlock()
	return b.overlay
}

func (b *Bridge) stateDTO(s status.Snapshot) StateDTO {
	cfg := b.d.Config.Get()
	name := s.RunningScriptName
	if s.RunningScriptID != "" {
		b.mu.Lock()
		if i := b.indexLocked(s.RunningScriptID); i >= 0 {
			name = b.scripts[i].Name
		}
		b.mu.Unlock()
	}
	dto := StateDTO{
		GlobalActive: s.GlobalActive,
		RunningID:    s.RunningScriptID,
		RunningName:  name,
		EditorOpen:   s.EditDialogOpen,
		Overlay:      b.overlayState(),
		Theme:        themeOf(cfg),
		AutoActive:   AutoActiveDTO{Enabled: cfg.AutoActiveEnabled, ProcessName: cfg.AutoActiveProcessName},
	}
	if b.d.Locks != nil {
		dto.MouseLocked = b.d.Locks.MouseLocked()
		dto.KeyboardLocked = b.d.Locks.KeyboardLocked()
	}
	return dto
}

func themeOf(cfg model.AppConfig) string {
	switch cfg.Theme {
	case "light", "dark", "system":
		return cfg.Theme
	}
	return "system"
}

func (b *Bridge) currentState() StateDTO { return b.stateDTO(b.d.Controller.Snapshot()) }

// NotifyStateChanged re-emits the state event (e.g. after an executor lock changes).
func (b *Bridge) NotifyStateChanged() { b.emit(EvState, b.currentState()) }

// NotifyUpdateAvailable tells the page a new release exists.
func (b *Bridge) NotifyUpdateAvailable(tag string) {
	b.emit(EvUpdateAvailable, updateAvailableEvent{Tag: tag})
}

// NavGoto asks the page to navigate (e.g. tray "Auto Active Process...").
func (b *Bridge) NavGoto(route string) { b.emit(EvNavGoto, navGotoEvent{Route: route}) }

// NotifyBlur ends an active capture because the host window lost activation.
func (b *Bridge) NotifyBlur() { b.capture.End(EndBlur) }

// Reload re-reads the script directory after an external change and emits scripts.changed.
func (b *Bridge) Reload() error {
	scripts, err := b.d.Store.List()
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.scripts = scripts
	b.rev++
	rev := b.rev
	b.mu.Unlock()
	b.syncHotkeys()
	b.emit(EvScriptsChanged, scriptsChangedEvent{Rev: rev})
	return nil
}

func (b *Bridge) onSaveError(script model.Script, err error) {
	log.Printf("bridge: save %s failed: %v", script.ID, err)
	b.mu.Lock()
	b.failed[script.ID] = struct{}{}
	b.mu.Unlock()
	b.emit(EvSaveFailed, saveFailedEvent{ScriptID: script.ID, Name: script.Name, Message: messages[CodeSaveFailed]})
}

// Shutdown deadlines, from S = BeginShutdown (editor-window contract §7). deadline is D1 (1.5 s in
// production); the editor's recovery must be done by D2 = D1 + shutdownRecovery and its window
// destroyed by D3 = D1 + shutdownDestroy. quiesce starts at recovery completion, so the worst case
// from S to then() is D1 + shutdownRecovery + quiesceGrace (1.5 + 1.0 + 3.0 = 5.5 s).
const (
	shutdownRecovery = time.Second
	shutdownDestroy  = 1500 * time.Millisecond
	// shutdownDestroyGrace: how long past D3 the barrier waits for destroy before giving way.
	shutdownDestroyGrace = 500 * time.Millisecond
)

// BeginShutdown starts a cooperative shutdown; call it on the UI thread. It returns at once:
// new mutating RPCs (except editor.save, editor.closeDecision, editor.checkpoint and
// app.closingReady) are rejected, the pages get app.closing{deadline_ms, roles}, and a one-shot
// goroutine waits for every required acknowledgement or D1, then closes the editor window if it is
// still there, runs quiesce, and posts then to the UI thread once quiesce is done AND the editor
// is destroyed. The editor's readiness is set by host cleanup (never by its page); main's ack
// alone does not release while an editor window exists. Later calls are no-ops.
func (b *Bridge) BeginShutdown(deadline time.Duration, then func()) {
	started := false
	var w *windowCtl
	b.beginOnce.Do(func() {
		started = true
		b.stopping.Store(true)
		roles := []string{"main"}
		if wc := b.winCtl(); wc != nil && wc.active() {
			w = wc
			roles = append(roles, "editor")
		}
		b.emit(EvAppClosing, appClosingEvent{DeadlineMs: deadline.Milliseconds(), Roles: roles})
		if w != nil {
			w.beginShutdown(b.clock().Now().Add(deadline + shutdownRecovery)) // D2: nothing persists after it
		}
	})
	if !started {
		return
	}
	d1, stop1 := b.afterChan(deadline)
	d2, stop2 := b.afterChan(deadline + shutdownRecovery)
	d3, stop3 := b.afterChan(deadline + shutdownDestroy)
	d3x, stop3x := b.afterChan(deadline + shutdownDestroy + shutdownDestroyGrace)
	go func() {
		defer stop1()
		defer stop2()
		defer stop3()
		defer stop3x()
		var destroyed, done <-chan struct{}
		if w != nil {
			destroyed, done = w.waitChans()
		}
		mainCh, edCh := (<-chan struct{})(b.closingReady), done
	wait:
		for mainCh != nil || edCh != nil {
			select {
			case <-mainCh:
				mainCh = nil
			case <-edCh:
				edCh = nil
			case <-d1:
				break wait
			}
		}
		if w != nil {
			select {
			case <-destroyed:
			default:
				b.post(w.forceForShutdown)
			}
			select { // recovery/persistence is bounded by D2
			case <-done:
			case <-d2:
			}
		}
		qdone := make(chan struct{})
		go func() { b.quiesce(quiesceGrace); close(qdone) }()
		if w != nil {
			select { // D3: teardown runs on the UI thread, independent of quiesce and of any Go-side wait
			case <-destroyed:
			case <-d3:
				b.post(w.forceForShutdown)
			}
			select { // the AND barrier; it gives way only when the UI thread itself is wedged
			case <-destroyed:
			case <-d3x:
				log.Printf("editor destroy not confirmed; proceeding") // §7 amendment H: the bounded escape
			}
		}
		<-qdone
		if then != nil {
			b.post(then)
		}
	}()
}

const quiesceGrace = 3 * time.Second

// quiesce waits (off the UI thread, bounded by grace) until no mutating handler holds the fence,
// then marks the bridge hard-closed so later handlers are rejected at entry. If a handler stays
// stuck it records quiesceFail; Shutdown then still closes the queue, so nothing is queued late.
func (b *Bridge) quiesce(grace time.Duration) {
	limit := time.Now().Add(grace)
	for !b.fence.TryLock() {
		if time.Now().After(limit) {
			b.quiesceFail.Store(true)
			b.hard.Store(true)
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	b.hard.Store(true)
	b.fence.Unlock()
}

func (b *Bridge) closingReadyHandler() (any, error) {
	b.readyOnce.Do(func() { close(b.closingReady) })
	return empty{}, nil
}

// ResetSession ends any capture, drops the editor session and draft and re-enables script
// execution (host: ProcessFailed, navigation away). Safe from any goroutine; idempotent.
func (b *Bridge) ResetSession(reason string) {
	endReason := reason
	switch reason {
	case EndStopped, EndBlur, EndTimeout, EndEditorClosed:
	default:
		endReason = EndEditorClosed
	}
	b.sessMu.Lock()
	b.mu.Lock()
	// DISPOSITION (editor-window §4.7 vs §6.5): ResetSession serves the MAIN window's renderer
	// recovery. A session opened by the editor window (gen != 0) belongs to the editor window's own
	// controller, which already detaches it under the §6.5 protocol (closing flag, save lease,
	// recovery) when the editor's renderer or the shared browser fails; a main-renderer reload does
	// not affect the editor's renderer, so the session is deliberately left alone here.
	if s := b.liveLocked(); s != nil && s.gen != 0 {
		b.mu.Unlock()
		b.sessMu.Unlock()
		return
	}
	sessionID := ""
	if b.session != nil {
		sessionID = b.session.id
	}
	b.setSessionLocked(nil) // in-flight saves/captures hold the old *editorSession and fail their commit check
	b.mu.Unlock()
	b.capture.End(endReason)
	b.releaseGate()
	b.sessMu.Unlock()
	b.emit(EvEditorReset, editorResetEvent{Reason: reason, SessionID: sessionID})
}

// Shutdown runs the §4 rule 7 steps the bridge owns, on the UI thread: stop captures, fence out
// RPC work (in-flight mutations finish first, later ones are rejected), editor-open off,
// auto-active stop, overlay position, flush saves. Idempotent; it emits app.closing itself only
// if BeginShutdown was not used.
func (b *Bridge) Shutdown(timeout time.Duration) error {
	b.shutdownOnce.Do(func() {
		b.beginOnce.Do(func() {
			b.stopping.Store(true)
			b.emit(EvAppClosing, appClosingEvent{})
		})
		b.capture.End(EndEditorClosed)
		// Quiesce in-flight mutations (bounded); a handler stuck on the UI thread must not hang us.
		b.quiesce(max(timeout/2, 50*time.Millisecond))
		// Close the queue: after this no handler can reach QueueSave, so the flush below is final.
		b.qMu.Lock()
		b.qClosed = true
		b.qMu.Unlock()
		b.sessMu.Lock()
		b.mu.Lock()
		b.closed = true
		b.setSessionLocked(nil)
		b.mu.Unlock()
		if b.d.Controller != nil {
			b.d.Controller.SetEditDialogOpen(false)
		}
		b.sessMu.Unlock()
		if b.d.AutoActive != nil {
			b.d.AutoActive.Stop()
		}
		if b.d.Overlay != nil && b.d.Config != nil {
			st := b.d.Overlay.State()
			if err := b.d.Config.Update(func(c *model.AppConfig) {
				c.OverlayPosition.X, c.OverlayPosition.Y = st.X, st.Y
			}); err != nil {
				log.Printf("bridge: persist overlay position failed: %v", err)
			}
		}
		if b.quiesceFail.Load() {
			b.shutdownErr = errors.New("bridge: in-flight RPC work did not finish before shutdown")
		}
		if !b.d.Store.FlushSaves(timeout) {
			b.shutdownErr = errors.New("bridge: pending script saves did not flush before timeout")
		}
	})
	return b.shutdownErr
}

// shutdownAllowed are the mutating methods still accepted while shutdown is in progress.
var shutdownAllowed = map[string]bool{"editor.save": true, "editor.close": true, "editor.closeDecision": true, "editor.checkpoint": true, "app.closingReady": true}

// mutating lists every method that changes state; they run under the fence and are rejected
// once shutdown begins (hard-rejected, including editor.save/close, once Shutdown has run).
var mutating = []string{
	"app.setGlobalActive", "app.exit", "window.hide", "window.show", "window.reportBounds",
	"scripts.setEnabled", "scripts.setGroupEnabled", "scripts.setGroupCollapsed", "settings.setSidebarCollapsed", "scripts.clone",
	"scripts.delete", "scripts.retrySave", "editor.open", "editor.save", "editor.close", "editor.closeDecision", "editor.checkpoint",
	"capture.hotkey.start", "capture.key.start", "capture.mouse.start", "capture.stop", "capture.mouse.stop",
	"settings.update", "overlay.set", "overlay.setStyle", "overlay.resetStyle", "autoactive.set", "update.apply", "update.skip", "update.check", "app.closingReady",
}

func (b *Bridge) fenceMutations(m map[string]Handler) {
	for _, name := range mutating {
		inner, ok := m[name]
		if !ok {
			continue
		}
		allowed := shutdownAllowed[name]
		m[name] = func(raw json.RawMessage) (any, error) {
			b.fence.RLock()
			defer b.fence.RUnlock()
			if b.hard.Load() || (b.stopping.Load() && !allowed) {
				return nil, newErr(CodeBusy)
			}
			return inner(raw)
		}
	}
}

func (b *Bridge) syncHotkeys() {
	if b.d.Hotkeys == nil {
		return
	}
	b.mu.Lock()
	list := append([]model.Script(nil), b.scripts...)
	b.mu.Unlock()
	b.d.Hotkeys.SetScripts(list)
}

func (b *Bridge) indexLocked(id string) int {
	for i := range b.scripts {
		if b.scripts[i].ID == id {
			return i
		}
	}
	return -1
}

func reg[P any](m map[string]Handler, name string, fn func(P) (any, error), required ...string) {
	m[name] = func(raw json.RawMessage) (any, error) {
		p, err := decode[P](raw, required...)
		if err != nil {
			return nil, err
		}
		return fn(p)
	}
}

func regNoParams(m map[string]Handler, name string, fn func() (any, error)) {
	m[name] = func(raw json.RawMessage) (any, error) {
		if t := bytes.TrimSpace(raw); len(t) > 0 && !bytes.Equal(t, []byte("null")) {
			if _, err := decode[struct{}](t); err != nil { // an object with zero fields, any whitespace
				return nil, err
			}
		}
		return fn()
	}
}

// MethodNames lists every contract method the bridge implements.
func MethodNames() []string {
	var names []string
	for n := range (&Bridge{}).Methods() {
		names = append(names, n)
	}
	return names
}

// Methods returns the method table. Unknown methods are rejected by the host dispatcher.
func (b *Bridge) Methods() map[string]Handler {
	m := map[string]Handler{}
	regNoParams(m, "app.getBootState", b.bootState)
	regNoParams(m, "app.ready", b.ready)
	regNoParams(m, "state.get", b.stateGet)
	reg(m, "app.setGlobalActive", b.setGlobalActive, "active")
	regNoParams(m, "app.exit", b.appExit)
	regNoParams(m, "window.hide", func() (any, error) { return b.hostCall(func(h Host) { h.Hide() }) })
	regNoParams(m, "window.show", func() (any, error) { return b.hostCall(func(h Host) { h.Show() }) })
	reg(m, "window.reportBounds", b.reportBounds, "x", "y", "w", "h")
	regNoParams(m, "scripts.list", b.scriptsList)
	reg(m, "scripts.get", b.scriptsGet, "id")
	reg(m, "scripts.setEnabled", b.scriptsSetEnabled, "id", "enabled")
	reg(m, "scripts.setGroupEnabled", b.scriptsSetGroupEnabled, "group", "enabled")
	reg(m, "scripts.setGroupCollapsed", b.scriptsSetGroupCollapsed, "group", "collapsed")
	reg(m, "settings.setSidebarCollapsed", b.settingsSetSidebarCollapsed, "collapsed")
	reg(m, "scripts.clone", b.scriptsClone, "id")
	reg(m, "scripts.delete", b.scriptsDelete, "id")
	reg(m, "scripts.retrySave", b.scriptsRetrySave, "id")
	reg(m, "editor.open", b.editorOpen, "id?")
	reg(m, "editor.save", b.editorSave, "sessionId", "script")
	reg(m, "editor.openWindow", b.editorOpenWindow)
	regNoParams(m, "editor.windowReady", b.editorWindowReady)
	reg(m, "editor.measured", b.editorMeasured, "token", "cssW", "cssH")
	reg(m, "editor.measureAck", b.editorMeasureAck, "token")
	regNoParams(m, "editor.requestClose", b.editorRequestClose)
	reg(m, "editor.checkpoint", b.editorCheckpoint, "sessionId", "revision", "script")
	reg(m, "editor.guardActive", b.editorGuardActive, "sessionId")
	reg(m, "editor.closeDecision", b.editorCloseDecision, "sessionId", "decision")
	reg(m, "editor.close", b.editorClose, "sessionId")
	reg(m, "editor.describeActions", b.editorDescribe, "actions")
	reg(m, "editor.toJSON", b.editorToJSON, "script")
	reg(m, "editor.fromJSON", b.editorFromJSON, "text")
	regNoParams(m, "actions.catalog", func() (any, error) { return buildCatalog(), nil })
	reg(m, "settings.capture.start", b.settingsCaptureStart, "field")
	reg(m, "settings.capture.stop", b.settingsCaptureStop)
	reg(m, "capture.hotkey.start", b.captureHotkeyStart, "sessionId", "field")
	reg(m, "capture.key.start", b.captureKeyStart, "sessionId")
	reg(m, "capture.stop", b.captureStop, "sessionId")
	reg(m, "capture.mouse.start", b.captureMouseStart, "sessionId")
	reg(m, "capture.mouse.stop", b.captureStop, "sessionId")
	reg(m, "clipboard.write", b.clipboardWrite, "text")
	regNoParams(m, "settings.get", b.settingsGet)
	reg(m, "settings.update", b.settingsUpdate, "patch")
	reg(m, "overlay.set", b.overlaySet)
	reg(m, "overlay.setStyle", b.overlaySetStyle, "radius", "background", "text_color", "background_opacity", "opacity", "width", "height", "x", "y")
	regNoParams(m, "overlay.resetStyle", b.overlayResetStyle)
	reg(m, "autoactive.set", b.autoActiveSet)
	reg(m, "update.apply", b.updateApply, "version")
	reg(m, "update.skip", b.updateSkip, "version")
	regNoParams(m, "update.check", b.updateCheck)
	regNoParams(m, "app.closingReady", b.closingReadyHandler)
	b.fenceMutations(m)
	return m
}

func (b *Bridge) bootState() (any, error) {
	st := BootState{Mode: "main", Macs: []string{}}
	if b.d.Host != nil {
		st = b.d.Host.BootInfo()
		if st.Macs == nil {
			st.Macs = []string{}
		}
	}
	return st, nil
}

func (b *Bridge) ready() (any, error) {
	b.NotifyStateChanged()
	return empty{}, nil
}

func (b *Bridge) stateGet() (any, error) {
	b.refreshOverlay(true)
	return b.currentState(), nil
}

func (b *Bridge) setGlobalActive(p struct {
	Active bool `json:"active"`
}) (any, error) {
	b.d.Controller.SetGlobalActive(p.Active)
	return b.currentState(), nil
}

func (b *Bridge) hostCall(fn func(Host)) (any, error) {
	if b.d.Host == nil {
		return nil, newErr(CodeInternal)
	}
	b.post(func() { fn(b.d.Host) })
	return empty{}, nil
}

func (b *Bridge) appExit() (any, error) { return b.hostCall(func(h Host) { h.Exit() }) }

func (b *Bridge) reportBounds(p struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}) (any, error) {
	const lim = 100000
	if p.X < -lim || p.X > lim || p.Y < -lim || p.Y > lim || p.W < 0 || p.W > lim || p.H < 0 || p.H > lim {
		return nil, badRequest()
	}
	want := model.WindowPos{X: p.X, Y: p.Y, Width: p.W, Height: p.H}
	if b.d.Config.Get().MainWindowPos == want {
		return empty{}, nil
	}
	if err := b.d.Config.Update(func(c *model.AppConfig) { c.MainWindowPos = want }); err != nil {
		log.Printf("bridge: persist window bounds failed: %v", err)
		return nil, newErr(CodeSaveFailed)
	}
	return empty{}, nil
}

func (b *Bridge) clipboardWrite(p struct {
	Text string `json:"text"`
}) (any, error) {
	if len(p.Text) > 64*1024 {
		return nil, badRequest()
	}
	if b.d.Host == nil {
		return nil, newErr(CodeInternal)
	}
	if err := b.d.Host.WriteClipboard(p.Text); err != nil {
		log.Printf("bridge: clipboard write failed: %v", err)
		return nil, newErr(CodeInternal)
	}
	return empty{}, nil
}

func (b *Bridge) updateApply(p updateParams) (any, error) {
	if p.Version == "" {
		return nil, badRequest()
	}
	if b.d.Host == nil {
		return nil, newErr(CodeUpdateFailed)
	}
	if !b.updating.CompareAndSwap(false, true) {
		return nil, newErr(CodeBusy)
	}
	go func() {
		defer b.updating.Store(false)
		err := b.d.Host.ApplyUpdate(p.Version, func(pct int) {
			b.emit(EvUpdateProgress, updateProgressEvent{Pct: max(0, min(100, pct))})
		})
		if err != nil {
			log.Printf("bridge: update failed: %v", err)
			msg := messages[CodeUpdateFailed]
			if errors.Is(err, updater.ErrStaleOffer) {
				msg = UpdateStaleMessage
			}
			b.emit(EvUpdateFailed, updateFailedEvent{Message: msg})
		}
	}()
	return empty{}, nil
}

// updateCheck runs an on-demand release check. Concurrency is bounded by UpdateCheck itself
// (updater.CheckUpdateShared joins in-flight checks, including the startup one). An available
// update is recorded on the host exactly as the startup check does, so update.apply installs it;
// no update.available event is emitted (the page shows the result inline, not the dialog).
//
// The network round trip (up to the updater's 10 s timeout) must not hold the single RPC worker,
// which every other page call shares, so the reply is deferred and resolved from its own goroutine.
func (b *Bridge) updateCheck() (any, error) {
	if b.d.Host == nil || b.d.UpdateCheck == nil || b.d.Offers == nil {
		return nil, newErr(CodeInternal)
	}
	if b.d.NewDeferred == nil {
		return b.checkUpdate()
	}
	reply, resolve := b.d.NewDeferred()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("bridge: update check panicked: %v", r)
				resolve(nil, newErr(CodeInternal))
			}
		}()
		resolve(b.checkUpdate())
	}()
	return reply, nil
}

func (b *Bridge) checkUpdate() (any, error) {
	current := b.d.Host.BootInfo().Version
	seq := b.d.Offers.Begin()
	tag, dl, kind := b.d.UpdateCheck(current)
	if kind != "" {
		return UpdateCheckResult{Status: "error", Current: current, Error: kind}, nil
	}
	if tag != "" && dl == "" {
		return UpdateCheckResult{Status: "error", Current: current, Error: "other"}, nil
	}
	offer := updater.Offer{Version: tag, URL: dl}
	if !b.d.Offers.Publish(seq, offer) {
		offer = b.d.Offers.Current() // a check that started later already published: report that one
	}
	if offer.Version == "" {
		return UpdateCheckResult{Status: "up_to_date", Current: current}, nil
	}
	return UpdateCheckResult{Status: "available", Current: current, Tag: offer.Version}, nil
}

// UpdateStaleMessage is the update.failed text when the offer changed after the page showed it.
const UpdateStaleMessage = "A newer update was found. Check again."

type updateParams struct {
	Version string `json:"version"`
}

func (b *Bridge) updateSkip(p updateParams) (any, error) {
	if p.Version == "" {
		return nil, badRequest()
	}
	if b.d.Host != nil {
		b.d.Host.SkipUpdate(p.Version)
	}
	return empty{}, nil
}
