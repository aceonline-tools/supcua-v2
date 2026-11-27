package bridge

import (
	"sync"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// The editor window controller (editor-window contract §4, §5, §7): the state machine of the
// separate modal editor window, its measurement handshake, close timers and teardown. It is
// platform-neutral: the native window is driven through EditorHost, time through Clock.
//
// Threading: every state-changing method is a UI-thread entry point (RPC handlers reach it with
// Bridge.callUI, timers with EditorHost.Post), serialised by c.mu. Persistence work (recovery,
// session release) runs on its own goroutine and never blocks native destroy.

type EditorHost interface {
	Post(func()) // run on the UI thread, non-blocking
	MainVisible() bool
	EnableMain(enabled bool)
	RevealMain() // restore and activate the main window
	// CreateEditor creates the hidden window owned by main and starts its asynchronous controller;
	// the host later calls WindowReady/WindowFailed with the returned generation. On a synchronous
	// failure it returns the error WITH the generation of the half-made (invalidated) window, which
	// the caller destroys via DestroyEditor after rolling back; it never calls WindowFailed then.
	CreateEditor() (gen uint64, err error)
	// PrepareEditor positions the hidden window on the target monitor (the remembered rect's, else
	// the owner's), lets its DPI settle and returns that monitor's facts.
	PrepareEditor(gen uint64, remembered model.EditorWindow) (EditorMetrics, error)
	PlaceEditor(gen uint64, r Rect, maximized bool)
	ShowEditor(gen uint64) // restore when iconic or hidden, then activate
	EditorBounds(gen uint64) (r Rect, maximized bool, ok bool)
	// DestroyEditor invalidates the generation, posts the controller Close() and destroys the HWND.
	// It never blocks (no COM call, no lock).
	DestroyEditor(gen uint64)
	EmitEditor(gen uint64, event string, payload any)
}

// EditorMetrics are the target monitor's facts at its real DPI.
type EditorMetrics struct {
	DPI              int
	FrameW, FrameH   int
	Work             Rect
	OwnerCX, OwnerCY int
}

type Timer interface{ Stop() bool }

// Clock is time as the controller sees it (a fake in tests).
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

type realClock struct{}

func (realClock) Now() time.Time                            { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

type windowState int

const (
	stNone windowState = iota
	stCreating
	stLoading
	stMeasuring
	stOpen
	stCloseRequested
	stClosing
)

func (s windowState) String() string {
	return [...]string{"none", "creating", "loading", "measuring", "open", "closeRequested", "closing"}[s]
}

// Timing constants (contract §4.5, §5.4).
const (
	startupGuard    = 10 * time.Second
	measureRound    = 5 * time.Second
	measureTotal    = 12 * time.Second
	measureGrace    = time.Second
	maxMeasureTries = 2 // extra rounds, shared by width remeasure and DPI change
	closeBase       = 10 * time.Second
	closeLease      = 3 * time.Second
	closeCeiling    = 60 * time.Second
	LeaseMs         = 3000
)

type teardownKind int

const (
	tdNormal teardownKind = iota // decision saved/discard
	tdForced                     // watchdog or shutdown deadline: recover, windowClosed{forced}
	tdFailed                     // failure in any state: recover, windowFailed
)

type windowCtl struct {
	b   *Bridge
	h   EditorHost
	clk Clock

	mu             sync.Mutex
	state          windowState
	gen            uint64
	scriptID       *string
	mainWasVisible bool
	token          uint64
	tries          int
	metrics        EditorMetrics
	shown          bool
	shutdown       bool
	cleaning       int       // teardown cleanups still running (the gate is released only at 0 with no window)
	recoverBy      time.Time // shutdown: the D2 recovery deadline (zero outside shutdown)

	t0, ceiling time.Time
	deadline    time.Time
	startupT    Timer
	roundT      Timer
	totalT      Timer
	graceT      Timer
	closeT      Timer

	pendingOut *BoundsOut // fallback size waiting for the page's measureAck or the grace timer

	destroyed chan struct{} // closed when this window's HWND is gone
	done      chan struct{} // closed when its session cleanup (recovery included) finished
}

// AttachEditorHost enables the separate editor window. recoveredDir receives recovered drafts.
func (b *Bridge) AttachEditorHost(h EditorHost, clk Clock, recoveredDir string) {
	if clk == nil {
		clk = realClock{}
	}
	b.recoveredDir = recoveredDir
	b.winMu.Lock()
	b.win = &windowCtl{b: b, h: h, clk: clk}
	b.winMu.Unlock()
}

func (b *Bridge) winCtl() *windowCtl {
	b.winMu.RLock()
	defer b.winMu.RUnlock()
	return b.win
}

// EditorEvents are the host's reports about the editor window (all on the UI thread).
type EditorEvents interface {
	WindowReady(gen uint64)
	WindowFailed(gen uint64, stage, msg string)
	CloseRequested(gen uint64)
	ProcessFailed(gen uint64, kind int32)
	DPIChanged(gen uint64)
	BoundsChanged(gen uint64, r Rect, maximized bool)
	SharedBrowserFailed()
}

// EditorEvents returns the controller to wire native callbacks to (nil before AttachEditorHost).
func (b *Bridge) EditorEvents() EditorEvents {
	if w := b.winCtl(); w != nil {
		return w
	}
	return nil
}

// EditorOpen reports whether an editor window exists (any state).
func (b *Bridge) EditorOpen() bool {
	w := b.winCtl()
	return w != nil && w.active()
}

func (c *windowCtl) currentGen() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *windowCtl) pendingID() (*string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.scriptID, c.state != stNone && c.state != stClosing
}

// active reports whether an editor window exists (any state but none).
func (c *windowCtl) active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state != stNone
}

func (c *windowCtl) State() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.String()
}

func (c *windowCtl) stopTimersLocked() {
	for _, t := range []*Timer{&c.startupT, &c.roundT, &c.totalT, &c.graceT, &c.closeT} {
		if *t != nil {
			(*t).Stop()
			*t = nil
		}
	}
}

// post runs f on the UI thread unless the window's generation moved on.
func (c *windowCtl) postGen(gen uint64, f func()) {
	c.h.Post(func() {
		c.mu.Lock()
		cur := c.gen == gen && c.state != stNone && c.state != stClosing
		c.mu.Unlock()
		if cur {
			f()
		}
	})
}

// ---- open / focus ----

// openWindow is editor.openWindow (UI thread): focus the existing editor, else create one.
//
// An older window's cleanup may still be running (recovery). Decision: the new window does NOT wait
// for it. Every cleanup action is bound to the generation that started it, so the older cleanup
// only touches its own (detached) session and capture and releases the gate only when no editor
// generation is active (cleanupDone).
func (c *windowCtl) openWindow(id *string) (status string, gen uint64, err error) {
	visible := c.h.MainVisible()
	c.mu.Lock()
	if c.state != stNone && c.state != stClosing {
		gen = c.gen
		c.mu.Unlock()
		c.h.ShowEditor(gen)
		return "focused", gen, nil
	}
	if c.state == stClosing || c.b.stopping.Load() {
		c.mu.Unlock()
		return "", 0, newErr(CodeShuttingDown)
	}
	// Creation start: the state and the gate are raised together under c.mu, so an older cleanup's
	// gate release (also under c.mu) can never slip between them. Script hotkeys are blocked and main
	// is disabled BEFORE the editor can be interactive (contract §4.2).
	c.mainWasVisible = visible
	c.state, c.gen, c.scriptID = stCreating, 0, id
	c.shown, c.shutdown, c.token, c.tries = false, false, 0, 0
	c.recoverBy = time.Time{}
	c.destroyed, c.done = make(chan struct{}), make(chan struct{})
	c.b.d.Controller.SetEditDialogOpen(true)
	c.mu.Unlock()
	c.h.EnableMain(false)
	gen, cerr := c.h.CreateEditor()
	if cerr != nil {
		// A synchronous failure returns here and is rolled back here (never through the async
		// callback): main is re-enabled and the gate restored BEFORE the half-made window is destroyed.
		c.h.EnableMain(true)
		c.mu.Lock()
		c.state, c.gen = stNone, 0
		close(c.destroyed)
		close(c.done)
		c.releaseGateLocked()
		c.mu.Unlock()
		if gen != 0 {
			c.h.DestroyEditor(gen)
		}
		c.b.emit(evEditorWindowFailed, windowFailedEvent{Gen: gen, Stage: "create", Message: "could not open the editor window"})
		return "", 0, newErr(CodeInternal)
	}
	c.mu.Lock()
	c.gen = gen
	c.startupT = c.clk.AfterFunc(startupGuard, func() {
		c.postGen(gen, func() { c.failEditor(gen, "timeout", "the editor window did not start in time") })
	})
	c.mu.Unlock()
	return "opening", gen, nil
}

// releaseGateLocked releases script execution when no editor generation is active and no
// teardown cleanup is still running (c.mu held).
func (c *windowCtl) releaseGateLocked() {
	if c.state == stNone && c.cleaning == 0 && c.b.d.Controller != nil {
		c.b.d.Controller.SetEditDialogOpen(false)
	}
}

// cleanupDone is the end of one generation's cleanup.
func (c *windowCtl) cleanupDone() {
	c.mu.Lock()
	if c.cleaning > 0 {
		c.cleaning--
	}
	c.releaseGateLocked()
	c.mu.Unlock()
}

// WindowReady is the host's report that the controller exists and navigation started.
func (c *windowCtl) WindowReady(gen uint64) {
	c.mu.Lock()
	if c.gen == gen && c.state == stCreating {
		c.state = stLoading
	}
	c.mu.Unlock()
}

// WindowFailed is the host's report that controller creation or navigation failed.
func (c *windowCtl) WindowFailed(gen uint64, stage, msg string) { c.failEditor(gen, stage, msg) }

// pageReady is editor.windowReady: start the measurement handshake.
func (c *windowCtl) pageReady() error {
	c.mu.Lock()
	if c.state != stCreating && c.state != stLoading {
		c.mu.Unlock()
		return newErr(CodeBadRequest)
	}
	gen := c.gen
	if c.startupT != nil {
		c.startupT.Stop()
		c.startupT = nil
	}
	c.state = stMeasuring
	c.mu.Unlock()
	m, err := c.h.PrepareEditor(gen, c.rememberedCfg())
	if err != nil {
		c.failEditor(gen, "measure", "could not prepare the editor window")
		return nil
	}
	c.mu.Lock()
	if c.gen != gen || c.state != stMeasuring {
		c.mu.Unlock()
		return nil
	}
	c.metrics = m
	c.token = 1
	tok := c.token
	c.armMeasureLocked(gen)
	c.totalT = c.clk.AfterFunc(measureTotal, func() { c.postGen(gen, func() { c.fallback(gen, "timeout") }) })
	c.mu.Unlock()
	c.h.EmitEditor(gen, evEditorMeasureRequest, measureRequestEvent{Token: tok})
	return nil
}

func (c *windowCtl) armMeasureLocked(gen uint64) {
	if c.roundT != nil {
		c.roundT.Stop()
	}
	tok := c.token
	c.roundT = c.clk.AfterFunc(measureRound, func() {
		c.postGen(gen, func() {
			c.mu.Lock()
			stale := c.token != tok || c.state != stMeasuring
			c.mu.Unlock()
			if !stale {
				c.fallback(gen, "timeout")
			}
		})
	})
}

func (c *windowCtl) rememberedCfg() model.EditorWindow {
	if c.b.d.Config == nil {
		return model.EditorWindow{}
	}
	return c.b.d.Config.Get().EditorWindow
}

type measuredResult struct {
	Next  string `json:"next"`
	Token uint64 `json:"token,omitempty"`
	CssW  int    `json:"cssW,omitempty"`
}

// measured is editor.measured (UI thread): decide show or remeasure.
func (c *windowCtl) measured(token uint64, cssW, cssH int) (measuredResult, error) {
	c.mu.Lock()
	if c.state != stMeasuring {
		c.mu.Unlock()
		return measuredResult{Next: "show"}, nil
	}
	gen := c.gen
	if token != c.token { // a dead token: remeasure at the current one while budget remains
		if c.tries < maxMeasureTries {
			r := measuredResult{Next: "remeasure", Token: c.token, CssW: clampInt(cssW, MinCssW, maxCss)}
			c.mu.Unlock()
			return r, nil
		}
		c.mu.Unlock()
		c.fallback(gen, "fallback")
		return measuredResult{Next: "show"}, nil
	}
	if cssW < 1 || cssH < 1 || cssW > maxCss || cssH > maxCss {
		c.mu.Unlock()
		c.fallback(gen, "fallback")
		return measuredResult{Next: "show"}, nil
	}
	out := computeEditorBounds(c.boundsInLocked(cssW, cssH))
	if out.WidthClamped && !out.FromMemory && c.tries < maxMeasureTries {
		c.tries++
		c.token++
		c.armMeasureLocked(gen)
		r := measuredResult{Next: "remeasure", Token: c.token, CssW: out.RemeasureCssW}
		c.mu.Unlock()
		return r, nil
	}
	c.mu.Unlock()
	c.present(gen, out)
	return measuredResult{Next: "show"}, nil
}

func (c *windowCtl) boundsInLocked(cssW, cssH int) BoundsIn {
	m := c.metrics
	return BoundsIn{CssW: cssW, CssH: cssH, DPI: m.DPI, FrameW: m.FrameW, FrameH: m.FrameH, Work: m.Work,
		OwnerCX: m.OwnerCX, OwnerCY: m.OwnerCY, Remembered: c.rememberedCfg()}
}

// present places and shows the window and enters Open.
func (c *windowCtl) present(gen uint64, out BoundsOut) {
	c.mu.Lock()
	if c.gen != gen || c.state != stMeasuring {
		c.mu.Unlock()
		return
	}
	c.state, c.shown = stOpen, true
	for _, t := range []*Timer{&c.roundT, &c.totalT, &c.graceT} {
		if *t != nil {
			(*t).Stop()
			*t = nil
		}
	}
	c.mu.Unlock()
	c.h.PlaceEditor(gen, out.Outer, out.Maximized)
	c.h.ShowEditor(gen)
}

// fallback shows the window at the failure-only size and tells the page to leave measure mode.
func (c *windowCtl) fallback(gen uint64, reason string) {
	c.mu.Lock()
	if c.gen != gen || c.state != stMeasuring {
		c.mu.Unlock()
		return
	}
	out := computeEditorBounds(c.boundsInLocked(FallbackCssW, FallbackCssH))
	c.mu.Unlock()
	c.h.EmitEditor(gen, evEditorMeasureAbort, measureAbortEvent{Reason: reason})
	// A hung page still gets a window: show it after a short grace (measureAck shows it at once).
	c.mu.Lock()
	if c.graceT == nil {
		c.graceT = c.clk.AfterFunc(measureGrace, func() { c.postGen(gen, func() { c.present(gen, out) }) })
	}
	c.pendingOut = &out
	c.mu.Unlock()
}

// measureAck is editor.measureAck: the page left measure mode; show now if a fallback is pending.
func (c *windowCtl) measureAck(token uint64) {
	c.mu.Lock()
	out, gen := c.pendingOut, c.gen
	c.mu.Unlock()
	if out != nil {
		c.present(gen, *out)
	}
}

// DPIChanged is WM_DPICHANGED on the editor: while measuring it spends a retry round and dead-ends
// the token (contract §5.4).
func (c *windowCtl) DPIChanged(gen uint64) {
	c.mu.Lock()
	if c.gen != gen || c.state != stMeasuring {
		c.mu.Unlock()
		return
	}
	if c.tries >= maxMeasureTries {
		c.mu.Unlock()
		c.fallback(gen, "dpi")
		return
	}
	c.tries++
	c.token++
	c.mu.Unlock()
	m, err := c.h.PrepareEditor(gen, c.rememberedCfg())
	c.mu.Lock()
	if err == nil && c.gen == gen && c.state == stMeasuring {
		c.metrics = m
		tok := c.token
		c.armMeasureLocked(gen)
		c.mu.Unlock()
		c.h.EmitEditor(gen, evEditorMeasureRequest, measureRequestEvent{Token: tok})
		return
	}
	c.mu.Unlock()
}

// ---- close flow ----

// CloseRequested is WM_CLOSE / Alt-F4 on the editor: the window is never destroyed by it.
func (c *windowCtl) CloseRequested(gen uint64) { c.requestClose(gen, "user") }

func (c *windowCtl) requestClose(gen uint64, reason string) {
	c.mu.Lock()
	if c.gen != gen || c.state != stOpen {
		c.mu.Unlock() // not interactive yet, or a repeat: no re-emit, no timer reset
		return
	}
	c.state = stCloseRequested
	now := c.clk.Now()
	c.t0, c.ceiling = now, now.Add(closeCeiling)
	c.deadline = now.Add(closeBase)
	c.armCloseLocked(gen)
	sid := c.b.sessionIDForGen(gen)
	c.mu.Unlock()
	c.h.EmitEditor(gen, evEditorCloseRequested, closeRequestedEvent{Gen: gen, SessionID: sid, Reason: reason})
}

func (c *windowCtl) armCloseLocked(gen uint64) {
	if c.closeT != nil {
		c.closeT.Stop()
	}
	d := c.deadline.Sub(c.clk.Now())
	if d < 0 {
		d = 0
	}
	c.closeT = c.clk.AfterFunc(d, func() {
		c.postGen(gen, func() {
			c.mu.Lock()
			late := c.state == stCloseRequested && !c.clk.Now().Before(c.deadline)
			c.mu.Unlock()
			if late {
				c.teardown(gen, tdForced, "", "")
			}
		})
	})
}

type leaseResult struct {
	LeaseMs int `json:"leaseMs"`
}

// guardActive is editor.guardActive: renew a 3 s lease, never past the 60 s ceiling, never during shutdown.
func (c *windowCtl) guardActive(sessionID string) (leaseResult, error) {
	if c.b.stopping.Load() {
		return leaseResult{}, newErr(CodeShuttingDown)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != stCloseRequested || c.b.sessionIDForGen(c.gen) != sessionID || sessionID == "" {
		return leaseResult{}, newErr(CodeEditorClosed)
	}
	d := c.clk.Now().Add(closeLease)
	if d.Before(c.t0.Add(closeBase)) {
		d = c.t0.Add(closeBase)
	}
	if d.After(c.ceiling) {
		d = c.ceiling
	}
	c.deadline = d
	c.armCloseLocked(c.gen)
	return leaseResult{LeaseMs: LeaseMs}, nil
}

// closeDecision is editor.closeDecision: the page's guard result; the host alone cleans up.
func (c *windowCtl) closeDecision(sessionID, decision string) error {
	c.mu.Lock()
	gen, st := c.gen, c.state
	c.mu.Unlock()
	if st == stNone || st == stClosing || c.b.sessionIDForGen(gen) != sessionID || sessionID == "" {
		return newErr(CodeEditorClosed)
	}
	switch decision {
	case "cancel":
		c.mu.Lock()
		if c.state == stCloseRequested && !c.shutdown {
			c.state = stOpen
			if c.closeT != nil {
				c.closeT.Stop()
				c.closeT = nil
			}
		} else if c.shutdown {
			c.mu.Unlock()
			c.teardown(gen, tdForced, "", "") // during shutdown "keep editing" is not possible
			return nil
		}
		c.mu.Unlock()
	case "saved":
		if st != stCloseRequested || !c.b.savedUpTo(sessionID) {
			return newErr(CodeBadRequest)
		}
		c.teardown(gen, tdNormal, "", "")
	case "discard":
		if st != stCloseRequested {
			return newErr(CodeBadRequest)
		}
		c.teardown(gen, tdNormal, "", "")
	default:
		return newErr(CodeBadRequest)
	}
	return nil
}

// ---- failure ----

// failEditor tears the window down from ANY state: recover the draft, release modality, destroy,
// toast main (contract §4.7). Idempotent.
func (c *windowCtl) failEditor(gen uint64, stage, msg string) { c.teardown(gen, tdFailed, stage, msg) }

// ProcessFailed is a WebView2 process failure of the editor's own controller.
func (c *windowCtl) ProcessFailed(gen uint64, kind int32) {
	c.failEditor(gen, "renderer", "the editor stopped responding")
}

// SharedBrowserFailed is a browser-process failure that also kills the editor's controller: the
// editor is cleaned up first so main is re-enabled before main's own recovery policy runs.
func (c *windowCtl) SharedBrowserFailed() {
	c.mu.Lock()
	gen, st := c.gen, c.state
	c.mu.Unlock()
	if st != stNone && st != stClosing {
		c.failEditor(gen, "renderer", "the editor stopped responding")
	}
}

// ---- teardown ----

// teardown is the single cleanup path (UI thread). Order, always: the session is marked closing
// (lock-free, so a save not past its commit point is rejected and none new takes a lease), main is
// re-enabled, script execution is restored when there is no session to recover, THEN the window is
// destroyed (posted Close, no lock, no wait); focus is restored per the visibility rule; session
// cleanup (recovery for forced and failed closes) continues off the UI thread, bound to this
// generation. Idempotent.
func (c *windowCtl) teardown(gen uint64, kind teardownKind, stage, msg string) {
	c.mu.Lock()
	if c.gen != gen || c.state == stNone || c.state == stClosing {
		c.mu.Unlock()
		return
	}
	c.state = stClosing
	c.stopTimersLocked()
	wasVisible := c.mainWasVisible
	destroyed, done := c.destroyed, c.done
	deadline := c.clk.Now().Add(recoverBound)
	if !c.recoverBy.IsZero() && c.recoverBy.Before(deadline) {
		deadline = c.recoverBy
	}
	c.mu.Unlock()

	hasSession := false
	if s := c.b.sessAtomic.Load(); s != nil && s.gen == gen && gen != 0 {
		s.closingFlag.Store(true) // the commit fence, set before anything is destroyed
		hasSession = true
	}
	if r, max, ok := c.h.EditorBounds(gen); ok && c.shownBefore() {
		c.saveBounds(r, max)
	}
	c.h.EnableMain(true) // before destroy: otherwise Windows activates another application
	c.mu.Lock()
	if hasSession {
		c.cleaning++
	} else if c.b.d.Controller != nil {
		c.b.d.Controller.SetEditDialogOpen(false) // nothing to recover: restore the gate before destroy
	}
	c.mu.Unlock()
	c.h.DestroyEditor(gen)
	close(destroyed)
	if wasVisible {
		c.h.RevealMain()
	}
	c.mu.Lock()
	c.state, c.gen, c.pendingOut = stNone, 0, nil
	c.mu.Unlock()

	go func() {
		defer close(done)
		c.b.cleanupSession(gen, kind, stage, msg, deadline)
	}()
}

func (c *windowCtl) shownBefore() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.shown
}

func (c *windowCtl) saveBounds(r Rect, maximized bool) {
	if c.b.d.Config == nil || r.W() <= 0 || r.H() <= 0 {
		return
	}
	want := model.EditorWindow{X: r.L, Y: r.T, W: r.W(), H: r.H(), Maximized: maximized, Version: model.EditorLayoutVersion}
	if c.b.d.Config.Get().EditorWindow == want {
		return
	}
	_ = c.b.d.Config.Update(func(a *model.AppConfig) { a.EditorWindow = want })
}

// BoundsChanged is the end of a move or resize of the editor window.
func (c *windowCtl) BoundsChanged(gen uint64, r Rect, maximized bool) {
	c.mu.Lock()
	ok := c.gen == gen && c.shown
	c.mu.Unlock()
	if ok {
		c.saveBounds(r, maximized)
	}
}

// ---- shutdown ----

// beginShutdown (UI thread) asks an open editor to close for app exit; windows that never became
// interactive are rolled back silently.
func (c *windowCtl) beginShutdown(recoverBy time.Time) {
	c.mu.Lock()
	gen, st := c.gen, c.state
	c.shutdown = true
	c.recoverBy = recoverBy
	c.mu.Unlock()
	switch st {
	case stOpen:
		c.requestClose(gen, "app-closing")
	case stCreating, stLoading, stMeasuring:
		c.teardown(gen, tdFailed, "shutdown", "")
	}
}

// forceForShutdown (UI thread) closes whatever is left of the editor at D1.
func (c *windowCtl) forceForShutdown() {
	c.mu.Lock()
	gen, st := c.gen, c.state
	c.mu.Unlock()
	if st != stNone && st != stClosing {
		c.teardown(gen, tdForced, "", "")
	}
}

// waitChans returns the current window's destroyed/done channels (closed ones when none).
func (c *windowCtl) waitChans() (destroyed, done <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.destroyed == nil {
		z := make(chan struct{})
		close(z)
		return z, z
	}
	return c.destroyed, c.done
}
