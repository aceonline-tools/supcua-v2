// Package bridge is the platform-neutral backend of the WebView2 UI: it implements every API
// method and event of the contract (docs/design/phase2-architecture.md §9) on top of app/,
// storage/, model/ and autoactive/. The host owns the dispatcher, the single RPC worker goroutine
// and the JS transport; handlers returned by Methods are called only from that worker.
package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yanmad27/supcua-v2/app/status"
	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
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

type Config interface {
	Get() model.AppConfig
	Update(fn func(*model.AppConfig)) error
}

type HotkeySet interface {
	SetScripts(scripts []model.Script)
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
	ApplyUpdate(progress func(pct int)) error
	SkipUpdate()
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
	Host       Host
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

	mu      sync.Mutex
	scripts []model.Script
	rev     uint64
	failed  map[string]struct{}
	session *editorSession
	closed  bool

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

func (b *Bridge) emit(event string, payload any) {
	if b.emitter != nil {
		b.emitter.Emit(event, payload)
	}
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

// BeginShutdown starts a cooperative shutdown; call it on the UI thread. It returns at once:
// new mutating RPCs (except editor.save, editor.close and app.closingReady) are rejected, the page
// gets app.closing{deadline_ms}, and a one-shot goroutine waits for app.closingReady or the
// deadline before posting then to the UI thread. Later calls are no-ops.
func (b *Bridge) BeginShutdown(deadline time.Duration, then func()) {
	started := false
	b.beginOnce.Do(func() {
		started = true
		b.stopping.Store(true)
		b.emit(EvAppClosing, appClosingEvent{DeadlineMs: deadline.Milliseconds()})
	})
	if !started {
		return
	}
	go func() {
		timer := time.NewTimer(deadline)
		defer timer.Stop()
		select {
		case <-b.closingReady:
		case <-timer.C:
		}
		b.quiesce(quiesceGrace)
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
	sessionID := ""
	if b.session != nil {
		sessionID = b.session.id
	}
	b.session = nil // in-flight saves/captures hold the old *editorSession and fail their commit check
	b.mu.Unlock()
	b.capture.End(endReason)
	if b.d.Controller != nil {
		b.d.Controller.SetEditDialogOpen(false)
	}
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
		b.session = nil
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
var shutdownAllowed = map[string]bool{"editor.save": true, "editor.close": true, "app.closingReady": true}

// mutating lists every method that changes state; they run under the fence and are rejected
// once shutdown begins (hard-rejected, including editor.save/close, once Shutdown has run).
var mutating = []string{
	"app.setGlobalActive", "app.exit", "window.hide", "window.show", "window.reportBounds",
	"scripts.setEnabled", "scripts.setGroupEnabled", "scripts.setGroupCollapsed", "settings.setSidebarCollapsed", "scripts.clone",
	"scripts.delete", "scripts.retrySave", "editor.open", "editor.save", "editor.close",
	"capture.hotkey.start", "capture.key.start", "capture.mouse.start", "capture.stop", "capture.mouse.stop",
	"settings.update", "overlay.set", "autoactive.set", "update.apply", "update.skip", "app.closingReady",
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
	reg(m, "editor.close", b.editorClose, "sessionId")
	reg(m, "editor.describeActions", b.editorDescribe, "actions")
	reg(m, "editor.toJSON", b.editorToJSON, "script")
	reg(m, "editor.fromJSON", b.editorFromJSON, "text")
	regNoParams(m, "actions.catalog", func() (any, error) { return buildCatalog(), nil })
	reg(m, "capture.hotkey.start", b.captureHotkeyStart, "sessionId", "field")
	reg(m, "capture.key.start", b.captureKeyStart, "sessionId")
	reg(m, "capture.stop", b.captureStop, "sessionId")
	reg(m, "capture.mouse.start", b.captureMouseStart, "sessionId")
	reg(m, "capture.mouse.stop", b.captureStop, "sessionId")
	reg(m, "clipboard.write", b.clipboardWrite, "text")
	regNoParams(m, "settings.get", b.settingsGet)
	reg(m, "settings.update", b.settingsUpdate, "patch")
	reg(m, "overlay.set", b.overlaySet)
	reg(m, "autoactive.set", b.autoActiveSet)
	regNoParams(m, "update.apply", b.updateApply)
	regNoParams(m, "update.skip", b.updateSkip)
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

func (b *Bridge) updateApply() (any, error) {
	if b.d.Host == nil {
		return nil, newErr(CodeUpdateFailed)
	}
	if !b.updating.CompareAndSwap(false, true) {
		return nil, newErr(CodeBusy)
	}
	go func() {
		defer b.updating.Store(false)
		err := b.d.Host.ApplyUpdate(func(pct int) {
			b.emit(EvUpdateProgress, updateProgressEvent{Pct: max(0, min(100, pct))})
		})
		if err != nil {
			log.Printf("bridge: update failed: %v", err)
			b.emit(EvUpdateFailed, updateFailedEvent{Message: messages[CodeUpdateFailed]})
		}
	}()
	return empty{}, nil
}

func (b *Bridge) updateSkip() (any, error) {
	if b.d.Host != nil {
		b.d.Host.SkipUpdate()
	}
	return empty{}, nil
}
