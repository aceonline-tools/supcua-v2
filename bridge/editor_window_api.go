package bridge

import (
	"context"
	"encoding/json"
	"time"
)

// Events of the editor window (contract §3.3). Names are routed by TargetFor.
const (
	evEditorCloseRequested = "editor.closeRequested"
	evEditorMeasureRequest = "editor.measureRequest"
	evEditorMeasureAbort   = "editor.measureAbort"
	evEditorWindowFailed   = "editor.windowFailed"
	evEditorWindowClosed   = "editor.windowClosed"
)

type closeRequestedEvent struct {
	Gen       uint64 `json:"gen"`
	SessionID string `json:"sessionId,omitempty"`
	Reason    string `json:"reason"` // user | app-closing | owner-closing
}
type measureRequestEvent struct {
	Token uint64 `json:"token"`
}
type measureAbortEvent struct {
	Reason string `json:"reason"` // fallback | dpi | timeout
}
type windowFailedEvent struct {
	Gen           uint64 `json:"gen"`
	Stage         string `json:"stage"`
	Message       string `json:"message"`
	RecoveredPath string `json:"recoveredPath,omitempty"`
}
type windowClosedEvent struct {
	Gen           uint64 `json:"gen"`
	Saved         bool   `json:"saved"`
	Forced        bool   `json:"forced"`
	RecoveredPath string `json:"recoveredPath,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (b *Bridge) sessionIDForGen(gen uint64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != nil && b.session.gen == gen && gen != 0 {
		return b.session.id
	}
	return ""
}

// savedUpTo reports whether every edit the page reported is persisted (lastSavedRev >= dirtyRev).
func (b *Bridge) savedUpTo(sessionID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.session != nil && b.session.id == sessionID && b.session.lastSavedRev >= b.session.dirtyRev
}

// cleanupSession is the off-UI-thread half of every editor teardown, bound to the generation that
// started it: it touches only that generation's session and capture, and releases script execution
// only when no editor generation is active. The session was already marked closing (lock-free)
// BEFORE the window was destroyed. Order: bounded wait for in-flight saves, detach the session (so a
// reopened editor can open its own), end that session's capture, recover its draft by deadline.
func (b *Bridge) cleanupSession(gen uint64, kind teardownKind, stage, msg string, deadline time.Time) {
	sess := b.sessAtomic.Load()
	if sess != nil && (sess.gen != gen || gen == 0) {
		sess = nil
	}
	inflight := false
	saved := false
	var rr RecoverResult
	if sess != nil {
		inflight = b.waitSaves(sess, saveLeaseWaitMax)
		b.sessMu.Lock()
		b.mu.Lock()
		sess.detached = true
		saved = sess.lastSavedRev > 0
		if b.session == sess {
			b.setSessionLocked(nil)
		}
		b.mu.Unlock()
		b.sessMu.Unlock()
		b.capture.EndSession(sess.id, EndEditorClosed)
		if kind != tdNormal {
			rr = b.recoverDraft(sess, inflight, deadline)
		}
	}
	if w := b.winCtl(); w != nil {
		w.cleanupDone() // releases the gate only when no editor generation is active
	} else if b.d.Controller != nil {
		b.d.Controller.SetEditDialogOpen(false)
	}
	if kind == tdFailed {
		if stage == "shutdown" { // no toast: the app is closing
			return
		}
		b.emit(evEditorWindowFailed, windowFailedEvent{Gen: gen, Stage: stage, Message: msg, RecoveredPath: rr.Path})
		return
	}
	b.emit(evEditorWindowClosed, windowClosedEvent{Gen: gen, Saved: saved || rr.Persisted, Forced: kind == tdForced, RecoveredPath: rr.Path, Reason: rr.Reason})
}

func (b *Bridge) needWindow() (*windowCtl, error) {
	if w := b.winCtl(); w != nil {
		return w, nil
	}
	return nil, newErr(CodeEditorClosed)
}

func (b *Bridge) editorOpenWindow(p struct {
	ID *string `json:"id"`
}) (any, error) {
	w, err := b.needWindow()
	if err != nil {
		return nil, err
	}
	if b.stopping.Load() {
		return nil, newErr(CodeShuttingDown)
	}
	var res struct {
		Status string `json:"status"`
		Gen    uint64 `json:"gen"`
	}
	var oerr error
	if err := b.callUI(func() error {
		res.Status, res.Gen, oerr = w.openWindow(p.ID)
		return nil
	}); err != nil {
		return nil, err
	}
	if oerr != nil {
		return nil, oerr
	}
	return res, nil
}

func (b *Bridge) editorWindowReady() (any, error) {
	w, err := b.needWindow()
	if err != nil {
		return nil, err
	}
	var rerr error
	if err := b.callUI(func() error { rerr = w.pageReady(); return nil }); err != nil {
		return nil, err
	}
	return empty{}, rerr
}

func (b *Bridge) editorMeasured(p struct {
	Token uint64  `json:"token"`
	CssW  int     `json:"cssW"`
	CssH  int     `json:"cssH"`
	Dpr   float64 `json:"dpr"`
}) (any, error) {
	w, err := b.needWindow()
	if err != nil {
		return nil, err
	}
	var res measuredResult
	var merr error
	if err := b.callUI(func() error { res, merr = w.measured(p.Token, p.CssW, p.CssH); return nil }); err != nil {
		return measuredResult{Next: "show"}, nil // the host could not answer in time: leave measure mode
	}
	return res, merr
}

func (b *Bridge) editorMeasureAck(p struct {
	Token uint64 `json:"token"`
}) (any, error) {
	w, err := b.needWindow()
	if err != nil {
		return nil, err
	}
	_ = b.callUI(func() error { w.measureAck(p.Token); return nil })
	return empty{}, nil
}

func (b *Bridge) editorGuardActive(p struct {
	SessionID string `json:"sessionId"`
}) (any, error) {
	w, err := b.needWindow()
	if err != nil {
		return nil, err
	}
	return w.guardActive(p.SessionID)
}

func (b *Bridge) editorCloseDecision(p struct {
	SessionID string `json:"sessionId"`
	Decision  string `json:"decision"`
}) (any, error) {
	w, err := b.needWindow()
	if err != nil {
		return nil, err
	}
	var derr error
	if err := b.callUI(func() error { derr = w.closeDecision(p.SessionID, p.Decision); return nil }); err != nil {
		return nil, err
	}
	return empty{}, derr
}

// CtxMethods are the handlers that need the caller identity (rpc.CallerFrom through Deps.CallerRole).
// app.closingReady is also registered in Methods (single-window behaviour); this one overrides it.
func (b *Bridge) CtxMethods() map[string]func(context.Context, json.RawMessage) (any, error) {
	return map[string]func(context.Context, json.RawMessage) (any, error){
		"app.closingReady": func(ctx context.Context, raw json.RawMessage) (any, error) {
			if b.d.CallerRole != nil && b.d.CallerRole(ctx) == "editor" {
				// The editor's readiness is set by host cleanup, never by a page call.
				return empty{}, nil
			}
			return b.closingReadyHandler()
		},
	}
}

// clock is the controller's clock (real unless a test attached a fake one).
func (b *Bridge) clock() Clock {
	if w := b.winCtl(); w != nil {
		return w.clk
	}
	return realClock{}
}

func (b *Bridge) afterChan(d time.Duration) (<-chan struct{}, func()) {
	ch := make(chan struct{})
	t := b.clock().AfterFunc(d, func() { close(ch) })
	return ch, func() { t.Stop() }
}
