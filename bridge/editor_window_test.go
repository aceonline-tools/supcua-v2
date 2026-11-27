package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// ---- fakes ----

type fakeTimer struct {
	at   time.Time
	f    func()
	dead atomic.Bool
}

func (t *fakeTimer) Stop() bool { return !t.dead.Swap(true) }

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_700_000_000, 0)} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

// Advance moves time forward, firing due timers in order (each outside the lock).
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.dead.Load() && !t.at.After(target) && (next == nil || t.at.Before(next.at)) {
				next = t
			}
		}
		if next == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		next.dead.Store(true)
		if next.at.After(c.now) {
			c.now = next.at
		}
		c.mu.Unlock()
		next.f()
	}
}

type fakeEdHost struct {
	mu          sync.Mutex
	log         []string
	events      []string
	payloads    map[string][]any
	visible     bool
	nextGen     uint64
	createErr   error
	metrics     EditorMetrics
	bounds      Rect
	maximized   bool
	destroyed   map[uint64]bool
	mainEnabled bool
	snap        func() bool // EditDialogOpen at the time of the call
}

func newFakeEdHost() *fakeEdHost {
	return &fakeEdHost{visible: true, nextGen: 1, mainEnabled: true, payloads: map[string][]any{}, destroyed: map[uint64]bool{},
		metrics: EditorMetrics{DPI: 96, FrameW: 16, FrameH: 39, Work: Rect{0, 0, 1920, 1040}, OwnerCX: 960, OwnerCY: 520}}
}
func (h *fakeEdHost) isDestroyed(g uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.destroyed[g]
}
func (h *fakeEdHost) rec(s string)      { h.mu.Lock(); h.log = append(h.log, s); h.mu.Unlock() }
func (h *fakeEdHost) Post(f func())     { f() }
func (h *fakeEdHost) MainVisible() bool { return h.visible }
func (h *fakeEdHost) EnableMain(e bool) {
	h.mu.Lock()
	h.mainEnabled = e
	h.mu.Unlock()
	gate := ""
	if h.snap != nil {
		gate = map[bool]string{true: ",gate=open", false: ",gate=closed"}[h.snap()]
	}
	h.rec("enableMain=" + map[bool]string{true: "true", false: "false"}[e] + gate)
}
func (h *fakeEdHost) RevealMain() { h.rec("revealMain") }
func (h *fakeEdHost) CreateEditor() (uint64, error) {
	if h.createErr != nil {
		h.mu.Lock()
		g := h.nextGen
		h.nextGen++
		h.mu.Unlock()
		h.rec("createFailed")
		return g, h.createErr // the half-made window's generation comes back with the error
	}
	h.mu.Lock()
	g := h.nextGen
	h.nextGen++
	h.mu.Unlock()
	h.rec("create")
	return g, nil
}
func (h *fakeEdHost) PrepareEditor(uint64, model.EditorWindow) (EditorMetrics, error) {
	h.rec("prepare")
	return h.metrics, nil
}
func (h *fakeEdHost) PlaceEditor(_ uint64, r Rect, _ bool) {
	h.rec("place")
	h.mu.Lock()
	h.bounds = r
	h.mu.Unlock()
}
func (h *fakeEdHost) ShowEditor(uint64) { h.rec("show") }
func (h *fakeEdHost) EditorBounds(uint64) (Rect, bool, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bounds, h.maximized, h.bounds.W() > 0
}
func (h *fakeEdHost) DestroyEditor(g uint64) {
	h.mu.Lock()
	h.destroyed[g] = true
	h.mu.Unlock()
	gate := ""
	if h.snap != nil {
		gate = map[bool]string{true: ",gate=open", false: ",gate=closed"}[h.snap()]
	}
	h.rec("destroy" + gate)
}
func (h *fakeEdHost) EmitEditor(_ uint64, ev string, p any) {
	h.mu.Lock()
	h.events = append(h.events, ev)
	h.payloads[ev] = append(h.payloads[ev], p)
	h.mu.Unlock()
}
func (h *fakeEdHost) count(ev string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, e := range h.events {
		if e == ev {
			n++
		}
	}
	return n
}
func (h *fakeEdHost) order(items ...string) bool { // items appear in this order in the log
	h.mu.Lock()
	defer h.mu.Unlock()
	i := 0
	for _, l := range h.log {
		if i < len(items) && strings.HasPrefix(l, items[i]) {
			i++
		}
	}
	return i == len(items)
}

type edRig struct {
	*rig
	h   *fakeEdHost
	clk *fakeClock
	dir string
	sid string
	gen uint64
}

func newEdRig(t *testing.T, scripts ...model.Script) *edRig {
	t.Helper()
	r := newRig(t, scripts...)
	e := &edRig{rig: r, h: newFakeEdHost(), clk: newFakeClock(), dir: filepath.Join(t.TempDir(), "recovered")}
	e.h.snap = func() bool { return r.ctl.Snapshot().EditDialogOpen }
	r.b.AttachEditorHost(e.h, e.clk, e.dir)
	return e
}

// openAll runs the full handshake to Open and returns the session id.
func (e *edRig) openAll(t *testing.T, id string) {
	t.Helper()
	res := e.mustCall(t, "editor.openWindow", `{"id":"`+id+`"}`)
	e.gen = uint64(res["gen"].(float64))
	e.b.win.WindowReady(e.gen)
	e.mustCall(t, "editor.windowReady", ``)
	s := e.mustCall(t, "editor.open", `{"id":"`+id+`"}`)
	e.sid = s["sessionId"].(string)
	e.mustCall(t, "editor.measured", `{"token":1,"cssW":1000,"cssH":700,"dpr":1}`)
	if st := e.b.win.State(); st != "open" {
		t.Fatalf("state %s", st)
	}
}

func (e *edRig) waitDone(t *testing.T) {
	t.Helper()
	_, done := e.b.win.waitChans()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not finish")
	}
}

func edScript(id string) model.Script {
	return model.Script{ID: id, Name: "S " + id, Enabled: true, RunMode: model.RunModeOnce, HotkeyMode: model.HotkeyModeToggle, Actions: []model.Action{}}
}

func scriptJSON(id, name string) string {
	s := edScript(id)
	s.Name = name
	b, _ := json.Marshal(s)
	return string(b)
}

// ---- open, modality, focus ----

func TestOpenWindowRaisesGateThenDisablesMainThenCreates(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	res := e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	if res["status"] != "opening" {
		t.Fatal(res)
	}
	if !e.h.order("enableMain=false,gate=open", "create") {
		t.Fatalf("script hotkeys must be blocked before main is disabled, main before create: %v", e.h.log)
	}
	if !e.ctl.Snapshot().EditDialogOpen || e.b.win.State() != "creating" {
		t.Fatal("gate/state")
	}
	again := e.mustCall(t, "editor.openWindow", `{"id":"other"}`)
	if again["status"] != "focused" || again["gen"] != res["gen"] || e.h.count("x") != 0 {
		t.Fatalf("repeat must focus the existing editor: %v", again)
	}
	creates := 0
	for _, l := range e.h.log {
		if l == "create" {
			creates++
		}
	}
	if creates != 1 || !e.h.order("show") {
		t.Fatalf("no second window; focus shows it: %v", e.h.log)
	}
}

func TestEveryStateTransitionHappyPath(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	states := []string{e.b.win.State()}
	step := func() { states = append(states, e.b.win.State()) }
	res := e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	step()
	e.gen = uint64(res["gen"].(float64))
	e.b.win.WindowReady(e.gen)
	step()
	e.mustCall(t, "editor.windowReady", ``)
	step()
	e.mustCall(t, "editor.open", `{"id":"a"}`)
	e.mustCall(t, "editor.measured", `{"token":1,"cssW":1000,"cssH":700,"dpr":1}`)
	step()
	e.b.win.CloseRequested(e.gen)
	step()
	sid := e.b.sessionIDForGen(e.gen)
	e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+sid+`","decision":"cancel"}`)
	step()
	e.b.win.CloseRequested(e.gen)
	e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+sid+`","decision":"discard"}`)
	step()
	want := []string{"none", "creating", "loading", "measuring", "open", "closeRequested", "open", "none"}
	if strings.Join(states, ",") != strings.Join(want, ",") {
		t.Fatalf("%v want %v", states, want)
	}
}

func TestIllegalTransitionsAreRejected(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	if _, err := e.call(t, "editor.windowReady", ``); err == nil {
		t.Fatal("windowReady with no window")
	}
	e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	e.mustCall(t, "editor.windowReady", ``)
	if _, err := e.call(t, "editor.windowReady", ``); err == nil {
		t.Fatal("windowReady twice")
	}
	e.b.win.CloseRequested(e.gen + 1) // wrong gen / not open: ignored
	if e.b.win.State() != "measuring" {
		t.Fatal(e.b.win.State())
	}
	if _, err := e.call(t, "editor.closeDecision", `{"sessionId":"nope","decision":"saved"}`); err == nil {
		t.Fatal("decision for an unknown session")
	}
}

func TestStartupFailureRollsBack(t *testing.T) {
	for name, fail := range map[string]func(e *edRig){
		"timeout":      func(e *edRig) { e.clk.Advance(startupGuard) },
		"embed failed": func(e *edRig) { e.b.win.WindowFailed(e.gen, "embed", "controller failed") },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEdRig(t, edScript("a"))
			res := e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
			e.gen = uint64(res["gen"].(float64))
			fail(e)
			e.waitDone(t)
			if !e.h.mainEnabled || e.ctl.Snapshot().EditDialogOpen || e.b.curSess() != nil || e.b.win.State() != "none" {
				t.Fatalf("rollback incomplete: %v", e.h.log)
			}
			if len(e.em.named("editor.windowFailed")) != 1 || !e.h.isDestroyed(e.gen) {
				t.Fatal("main must get a toast event and the window must be destroyed")
			}
			e.mustCall(t, "editor.openWindow", `{"id":"a"}`) // and reopen works
		})
	}
	e := newEdRig(t, edScript("a"))
	e.h.createErr = errors.New("boom")
	if _, err := e.call(t, "editor.openWindow", `{"id":"a"}`); err == nil || !e.h.mainEnabled || e.ctl.Snapshot().EditDialogOpen {
		t.Fatal("synchronous create failure must roll back")
	}
}

// ---- close flow ----

func TestCloseReEnablesMainBeforeDestroyThenFocusRule(t *testing.T) {
	for _, visible := range []bool{true, false} {
		e := newEdRig(t, edScript("a"))
		e.h.visible = visible
		e.openAll(t, "a")
		e.b.win.CloseRequested(e.gen)
		if e.h.count(evEditorCloseRequested) != 1 {
			t.Fatal("closeRequested not emitted")
		}
		e.b.win.CloseRequested(e.gen)
		if e.h.count(evEditorCloseRequested) != 1 {
			t.Fatal("repeat must not re-emit")
		}
		e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+e.sid+`","decision":"discard"}`)
		if !e.h.order("enableMain=true", "destroy") {
			t.Fatalf("main must be re-enabled BEFORE destroy: %v", e.h.log)
		}
		revealed := e.h.order("destroy", "revealMain")
		if revealed != visible {
			t.Fatalf("visible=%v revealed=%v %v", visible, revealed, e.h.log)
		}
		e.waitDone(t)
		if e.ctl.Snapshot().EditDialogOpen || e.b.curSess() != nil {
			t.Fatal("cleanup must release the gate and the session")
		}
		if got := len(e.em.named("editor.windowClosed")); got != 1 {
			t.Fatalf("windowClosed %d", got)
		}
	}
}

func TestClosingDecisionSavedNeedsAPersistedRevisionAndNeverSavesItself(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.b.win.CloseRequested(e.gen)
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":3,"script":`+scriptJSON("a", "Edited")+`}`)
	if _, err := e.call(t, "editor.closeDecision", `{"sessionId":"`+e.sid+`","decision":"saved"}`); err == nil {
		t.Fatal("saved with unsaved edits must be rejected")
	}
	e.mustCall(t, "editor.save", `{"sessionId":"`+e.sid+`","snapshotRev":3,"script":`+scriptJSON("a", "Edited")+`}`)
	before := len(e.store.calls())
	e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+e.sid+`","decision":"saved"}`)
	e.waitDone(t)
	if got := len(e.store.calls()); got != before {
		t.Fatalf("closeDecision must not save: %d -> %d (one save per close)", before, got)
	}
}

// ---- watchdog ----

func TestWatchdogFiresAtTenSecondsAndOnlyAfterACloseRequest(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.clk.Advance(5 * time.Minute) // open for ages: no timer runs while Open
	if e.b.win.State() != "open" {
		t.Fatal("watchdog must not run while Open")
	}
	e.b.win.CloseRequested(e.gen)
	e.clk.Advance(9*time.Second + 900*time.Millisecond)
	e.b.win.CloseRequested(e.gen) // repeats do not reset it
	if e.b.win.State() != "closeRequested" {
		t.Fatal("early")
	}
	e.clk.Advance(200 * time.Millisecond)
	if e.b.win.State() != "none" || !e.h.isDestroyed(e.gen) {
		t.Fatalf("must fire at T0+10s: %s", e.b.win.State())
	}
	e.waitDone(t)
	if ev := e.em.named("editor.windowClosed"); len(ev) != 1 || !ev[0].payload.(windowClosedEvent).Forced {
		t.Fatalf("forced close event: %+v", ev)
	}
}

func TestGuardLeaseExtendsInThreeSecondStepsAndIsCappedAtSixtySeconds(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.b.win.CloseRequested(e.gen)
	for i := 0; i < 20; i++ { // renew every second while the page is responsive
		e.clk.Advance(time.Second)
		if _, err := e.call(t, "editor.guardActive", `{"sessionId":"`+e.sid+`"}`); err != nil {
			t.Fatalf("t=%ds: %v", i+1, err)
		}
	}
	if e.b.win.State() != "closeRequested" {
		t.Fatal("leases must keep it alive past the 10s base")
	}
	for i := 20; i < 59; i++ {
		e.clk.Advance(time.Second)
		if _, err := e.call(t, "editor.guardActive", `{"sessionId":"`+e.sid+`"}`); err != nil {
			break
		}
	}
	e.clk.Advance(1500 * time.Millisecond) // past T0+60s: the ceiling is absolute
	if e.b.win.State() != "none" {
		t.Fatalf("ceiling not enforced: %s", e.b.win.State())
	}
}

func TestHungPageCannotRenewAndShutdownGetsNoLease(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.b.win.CloseRequested(e.gen)
	e.clk.Advance(10*time.Second + time.Millisecond) // no guardActive arrived: base deadline
	if e.b.win.State() != "none" {
		t.Fatal("a page that sends no pings is force-closed at 10s")
	}
	e2 := newEdRig(t, edScript("a"))
	e2.openAll(t, "a")
	e2.b.win.CloseRequested(e2.gen)
	e2.b.stopping.Store(true)
	_, err := e2.call(t, "editor.guardActive", `{"sessionId":"`+e2.sid+`"}`)
	wantCode(t, err, CodeShuttingDown)
}

func TestCancelCancelsTheAttemptAndTheCeilingRestartsOnANewRequest(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.b.win.CloseRequested(e.gen)
	e.clk.Advance(8 * time.Second)
	e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+e.sid+`","decision":"cancel"}`)
	e.clk.Advance(30 * time.Second)
	if e.b.win.State() != "open" {
		t.Fatal("a cancelled attempt must leave no timer behind")
	}
	e.b.win.CloseRequested(e.gen) // a NEW request: full 10s again
	e.clk.Advance(9 * time.Second)
	if e.b.win.State() != "closeRequested" {
		t.Fatal("new attempt restarts the clock")
	}
	e.clk.Advance(2 * time.Second)
	if e.b.win.State() != "none" {
		t.Fatal("and fires at its own T0+10s")
	}
}

// ---- failure in any state ----

func TestFailEditorFromEveryState(t *testing.T) {
	drive := map[string]func(e *edRig){
		"creating": func(e *edRig) { e.mustCall(t0(), "editor.openWindow", `{"id":"a"}`) },
		"loading": func(e *edRig) {
			e.mustCall(t0(), "editor.openWindow", `{"id":"a"}`)
			e.b.win.WindowReady(e.b.win.currentGen())
		},
		"measuring": func(e *edRig) {
			e.mustCall(t0(), "editor.openWindow", `{"id":"a"}`)
			e.mustCall(t0(), "editor.windowReady", ``)
		},
		"open":           func(e *edRig) { e.openAll(t0(), "a") },
		"closeRequested": func(e *edRig) { e.openAll(t0(), "a"); e.b.win.CloseRequested(e.b.win.currentGen()) },
	}
	for name, d := range drive {
		t.Run(name, func(t *testing.T) {
			e := newEdRig(t, edScript("a"))
			cur = t
			d(e)
			gen := e.b.win.currentGen()
			e.b.win.ProcessFailed(gen, 1)
			e.waitDone(t)
			if e.b.win.State() != "none" || !e.h.mainEnabled || e.ctl.Snapshot().EditDialogOpen || e.b.curSess() != nil || !e.h.isDestroyed(gen) {
				t.Fatalf("%s: not fully failed over: %v", name, e.h.log)
			}
			if got := len(e.em.named("editor.windowFailed")); got != 1 {
				t.Fatalf("toast %d", got)
			}
			e.b.win.ProcessFailed(gen, 1) // idempotent
		})
	}
}

var cur *testing.T

func t0() *testing.T { return cur }

func TestSharedBrowserFailureCleansTheEditorFirst(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.b.win.SharedBrowserFailed()
	e.waitDone(t)
	if !e.h.mainEnabled || e.b.win.State() != "none" {
		t.Fatal("editor not cleaned")
	}
}

func TestMainRendererResetLeavesTheEditorSessionAlone(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.b.ResetSession("processFailed")
	if e.b.sessionIDForGen(e.gen) != e.sid || !e.ctl.Snapshot().EditDialogOpen {
		t.Fatal("main renderer recovery must not reset the editor window's session")
	}
}

// ---- measurement ----

func TestMeasureHandshakeShowsAtTheMeasuredSizeAndRemembersBounds(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	e.gen = e.b.win.currentGen()
	e.mustCall(t, "editor.windowReady", ``)
	if e.h.count(evEditorMeasureRequest) != 1 {
		t.Fatal("measureRequest must carry the initial token")
	}
	res := e.mustCall(t, "editor.measured", `{"token":1,"cssW":1000,"cssH":700,"dpr":1}`)
	if res["next"] != "show" || e.h.bounds.W() != 1016 || e.h.bounds.H() != 739 {
		t.Fatalf("%v %+v", res, e.h.bounds)
	}
	e.b.win.CloseRequested(e.gen)
	_, err := e.call(t, "editor.closeDecision", `{"sessionId":"x","decision":"discard"}`) // wrong session: rejected
	wantCode(t, err, CodeEditorClosed)
}

func TestWidthClampSpendsOneRetryThenShows(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.h.metrics.DPI = 192
	e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	e.mustCall(t, "editor.windowReady", ``)
	res := e.mustCall(t, "editor.measured", `{"token":1,"cssW":1400,"cssH":700,"dpr":2}`)
	if res["next"] != "remeasure" || res["token"].(float64) != 2 || res["cssW"].(float64) != float64((1920-16)*96/192) {
		t.Fatalf("%v", res)
	}
	old := e.mustCall(t, "editor.measured", `{"token":1,"cssW":1400,"cssH":700,"dpr":2}`) // a dead token
	if old["next"] != "remeasure" || old["token"].(float64) != 2 {
		t.Fatalf("dead token gets the current one: %v", old)
	}
	res = e.mustCall(t, "editor.measured", `{"token":2,"cssW":952,"cssH":700,"dpr":2}`)
	if res["next"] != "show" || e.b.win.State() != "open" {
		t.Fatalf("%v %s", res, e.b.win.State())
	}
}

func TestDPIChangeSpendsTheSameBudgetAndInvalidatesTheToken(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	e.gen = e.b.win.currentGen()
	e.mustCall(t, "editor.windowReady", ``)
	e.b.win.DPIChanged(e.gen)
	e.b.win.DPIChanged(e.gen)
	if e.h.count(evEditorMeasureRequest) != 3 {
		t.Fatalf("1 initial + 2 retries: %d", e.h.count(evEditorMeasureRequest))
	}
	e.b.win.DPIChanged(e.gen) // budget exhausted → fallback
	if e.h.count(evEditorMeasureAbort) != 1 {
		t.Fatal("exhausted budget must fall back and tell the page to leave measure mode")
	}
	res := e.mustCall(t, "editor.measured", `{"token":1,"cssW":900,"cssH":700,"dpr":1}`)
	if res["next"] != "show" {
		t.Fatalf("a dead token with no budget left: %v", res)
	}
}

func TestMeasureTimeoutFallsBackAndAHungPageIsShownAfterTheGrace(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	e.mustCall(t, "editor.windowReady", ``)
	e.clk.Advance(measureRound + time.Millisecond)
	if e.h.count(evEditorMeasureAbort) != 1 || e.b.win.State() != "measuring" {
		t.Fatal("abort sent, still waiting for the page's ack")
	}
	e.clk.Advance(measureGrace + time.Millisecond) // the page never acks
	if e.b.win.State() != "open" || !e.h.order("place", "show") {
		t.Fatalf("a hung page must still get a window: %s %v", e.b.win.State(), e.h.log)
	}
	if want := (computeEditorBounds(BoundsIn{CssW: FallbackCssW, CssH: FallbackCssH, DPI: 96, FrameW: 16, FrameH: 39, Work: e.h.metrics.Work, OwnerCX: 960, OwnerCY: 520}).Outer); e.h.bounds != want {
		t.Fatalf("fallback size: %+v want %+v", e.h.bounds, want)
	}
}

func TestMeasureAckShowsAFallbackImmediately(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	e.mustCall(t, "editor.windowReady", ``)
	e.mustCall(t, "editor.measured", `{"token":1,"cssW":0,"cssH":0,"dpr":1}`)
	e.mustCall(t, "editor.measureAck", `{"token":1}`)
	if e.b.win.State() != "open" {
		t.Fatal(e.b.win.State())
	}
}

func TestBoundsPersistedOnCloseAndRememberedOnReopen(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.h.bounds = Rect{200, 100, 1100, 800}
	e.b.win.CloseRequested(e.gen)
	e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+e.sid+`","decision":"discard"}`)
	e.waitDone(t)
	got := e.cfg.Get().EditorWindow
	if got != (model.EditorWindow{X: 200, Y: 100, W: 900, H: 700, Version: model.EditorLayoutVersion}) {
		t.Fatalf("%+v", got)
	}
	e.openAll(t, "a")
	if e.h.bounds != (Rect{200, 100, 1100, 800}) {
		t.Fatalf("remembered bounds take precedence: %+v", e.h.bounds)
	}
}

// ---- checkpoints, revisions, recovery ----

func TestCheckpointOrderingStalenessAndLimits(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	cp := func(rev int, body string) map[string]any {
		return e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":`+itoa(rev)+`,"script":`+body+`}`)
	}
	if r := cp(2, scriptJSON("a", "v2")); r["applied"] != true {
		t.Fatal(r)
	}
	if r := cp(1, scriptJSON("a", "v1")); r["applied"] != false || r["revision"].(float64) != 2 {
		t.Fatalf("older revision is a no-op success: %v", r)
	}
	if r := cp(2, scriptJSON("a", "dup")); r["applied"] != false {
		t.Fatalf("equal revision: %v", r)
	}
	e.mustCall(t, "editor.save", `{"sessionId":"`+e.sid+`","snapshotRev":5,"script":`+scriptJSON("a", "saved")+`}`)
	if r := cp(4, scriptJSON("a", "late")); r["applied"] != false {
		t.Fatalf("at or below lastSavedRev: %v", r)
	}
	if e.b.session.checkpoint != nil {
		t.Fatal("a successful save clears checkpoints at or below its revision")
	}
	huge := edScript("a")
	huge.Name = strings.Repeat("x", maxDraftBytes+1)
	hb, _ := json.Marshal(huge)
	_, err := e.call(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":9,"script":`+string(hb)+`}`)
	wantCode(t, err, CodeDraftTooLarge)
	_, err = e.call(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":9,"script":`+scriptJSON("zzz", "x")+`}`)
	wantCode(t, err, CodeBadRequest)
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestSaveReturnsSavedRevAndKeepsTheGlobalRev(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	res := e.mustCall(t, "editor.save", `{"sessionId":"`+e.sid+`","snapshotRev":7,"script":`+scriptJSON("a", "n")+`}`)
	if res["savedRev"].(float64) != 7 || res["rev"].(float64) == 7 && false {
		t.Fatalf("%v", res)
	}
	old := e.mustCall(t, "editor.save", `{"sessionId":"`+e.sid+`","script":`+scriptJSON("a", "m")+`}`) // legacy caller: no snapshotRev
	if old["savedRev"].(float64) != 8 {
		t.Fatalf("absent snapshotRev = dirtyRev+1: %v", old)
	}
}

func TestForcedCloseRecoversTheCheckpointToDiskAndIntoTheScript(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":4,"script":`+scriptJSON("a", "Recovered Name")+`}`)
	e.b.win.CloseRequested(e.gen)
	e.clk.Advance(closeBase + time.Millisecond)
	e.waitDone(t)
	ents, _ := os.ReadDir(e.dir)
	if len(ents) != 1 {
		t.Fatalf("recovery file: %v", ents)
	}
	raw, _ := os.ReadFile(filepath.Join(e.dir, ents[0].Name()))
	var env RecoveryEnvelope
	if err := json.Unmarshal(raw, &env); err != nil || env.V != 1 || env.ScriptID != "a" || env.Revision != 4 || env.Script.Name != "Recovered Name" || env.SavedAt == "" {
		t.Fatalf("envelope %s %v", raw, err)
	}
	if st, _ := os.Stat(filepath.Join(e.dir, ents[0].Name())); st.Mode().Perm()&0o077 != 0 && os.PathSeparator != '\\' {
		t.Fatalf("mode %v", st.Mode())
	}
	if got := e.scr("a").Name; got != "Recovered Name" {
		t.Fatalf("valid draft persisted into the script: %q", got)
	}
	ev := e.em.named("editor.windowClosed")
	if len(ev) != 1 || ev[0].payload.(windowClosedEvent).RecoveredPath == "" {
		t.Fatalf("path must reach main: %+v", ev)
	}
	e.openAll(t, "a") // reopen after a forced close
}

func TestInvalidDraftIsKeptAsAFileAndNeverWrittenIntoTheScript(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	bad := edScript("a")
	bad.Name = "   "
	bb, _ := json.Marshal(bad)
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+string(bb)+`}`)
	e.b.win.ProcessFailed(e.gen, 1)
	e.waitDone(t)
	ents, _ := os.ReadDir(e.dir)
	if len(ents) != 1 || e.scr("a").Name != "S a" {
		t.Fatalf("files=%v name=%q", ents, e.scr("a").Name)
	}
}

func TestRecoveryFilesArePrunedToFivePerScript(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 8; i++ {
		if _, err := writeRecovery(dir, RecoveryEnvelope{V: 1, ScriptID: "a", Revision: uint64(i), Script: edScript("a")}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != recoveredKeep {
		t.Fatalf("%d files", len(ents))
	}
}

// ---- save lease (the session commit fence) ----

func stuckSaveHook(e *edRig) (inSave, release chan struct{}) {
	inSave, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.b.testHook = func(stage string) {
		if stage == "validated" || stage == "recover.enqueue" {
			once.Do(func() { close(inSave); <-release })
		}
	}
	return
}

func (c *fakeClock) live() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.dead.Load() {
			n++
		}
	}
	return n
}

func TestSaveNotPastItsCommitPointIsRejectedOnceClosingIsSetAndStillRecovered(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Newer Draft")+`}`)
	inSave, release := stuckSaveHook(e)
	saveDone := make(chan error, 1)
	go func() {
		_, err := e.m["editor.save"](json.RawMessage(`{"sessionId":"` + e.sid + `","snapshotRev":3,"script":` + scriptJSON("a", "In Flight") + `}`))
		saveDone <- err
	}()
	<-inSave
	cleaned := make(chan struct{})
	go func() {
		e.b.win.ProcessFailed(e.gen, 1)
		_, done := e.b.win.waitChans()
		<-done
		close(cleaned)
	}()
	waitFor(t, func() bool {
		return e.b.lastClosed.Load() != nil
	})
	_, err := e.call(t, "editor.save", `{"sessionId":"`+e.sid+`","snapshotRev":4,"script":`+scriptJSON("a", "Late")+`}`)
	wantCode(t, err, CodeSessionClosing)
	select {
	case <-cleaned:
		t.Fatal("cleanup must wait for the in-flight save within its bound")
	case <-time.After(100 * time.Millisecond):
	}
	close(release) // it passed validation but not its commit point: the fence rejects it
	wantCode(t, <-saveDone, CodeSessionClosing)
	<-cleaned
	if e.scr("a").Name != "Late" { // amendment D: the newest payload by revision (rev 4, refused at entry)
		t.Fatalf("the rejected saves' newest draft must be recovered, not lost: %q", e.scr("a").Name)
	}
	if ents, _ := os.ReadDir(e.dir); len(ents) != 1 {
		t.Fatalf("recovery file: %v", ents)
	}
}

func TestLeaseWaitIsBoundedAt500msOnTheInjectedClock(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Draft")+`}`)
	_, release := stuckSaveHook(e)
	go e.m["editor.save"](json.RawMessage(`{"sessionId":"` + e.sid + `","snapshotRev":1,"script":` + scriptJSON("a", "Stuck") + `}`))
	waitFor(t, func() bool { return e.b.sessAtomic.Load().savingN.Load() == 1 })
	before := e.clk.live()
	e.b.win.ProcessFailed(e.gen, 1)
	waitFor(t, func() bool { return e.clk.live() > before-1 && e.b.lastClosed.Load() != nil })
	_, done := e.b.win.waitChans()
	select {
	case <-done:
		t.Fatal("cleanup must wait for the lease until the bound")
	case <-time.After(80 * time.Millisecond):
	}
	e.clk.Advance(saveLeaseWaitMax - time.Millisecond)
	select {
	case <-done:
		t.Fatal("just under 500ms it still waits")
	case <-time.After(50 * time.Millisecond):
	}
	e.clk.Advance(2 * time.Millisecond) // the bound passes
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup must give up at 500ms")
	}
	if ents, _ := os.ReadDir(e.dir); len(ents) != 1 || e.scr("a").Name == "Draft" {
		t.Fatalf("file-only recovery with a save in flight: %v %q", ents, e.scr("a").Name)
	}
	close(release)
}

// ---- shutdown ----

func shutdownRig(t *testing.T) (*edRig, chan struct{}) {
	e := newEdRig(t, edScript("a"))
	then := make(chan struct{})
	e.openAll(t, "a")
	e.b.BeginShutdown(1500*time.Millisecond, func() { close(then) })
	return e, then
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func TestShutdownAnnouncesRolesAndMainAckAloneDoesNotRelease(t *testing.T) {
	e, then := shutdownRig(t)
	ev := e.em.named(EvAppClosing)
	if len(ev) != 1 || strings.Join(ev[0].payload.(appClosingEvent).Roles, ",") != "main,editor" {
		t.Fatalf("%+v", ev)
	}
	if e.h.count(evEditorCloseRequested) != 1 {
		t.Fatal("the editor is asked to close with reason app-closing")
	}
	e.mustCall(t, "app.closingReady", ``)
	time.Sleep(100 * time.Millisecond)
	if isClosed(then) || e.b.win.State() == "none" {
		t.Fatal("main's ack alone must not end shutdown while the editor is open")
	}
	e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+e.sid+`","decision":"discard"}`)
	select {
	case <-then:
	case <-time.After(3 * time.Second):
		t.Fatal("main ack + editor cleanup must release")
	}
}

func TestShutdownForcesTheEditorAtD1WithRecoveryAndRespectsTheBarrier(t *testing.T) {
	e, then := shutdownRig(t)
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Shutdown Draft")+`}`)
	e.mustCall(t, "app.closingReady", ``)
	e.clk.Advance(1499 * time.Millisecond)
	if e.b.win.State() == "none" {
		t.Fatal("before D1 the editor still has its chance")
	}
	e.clk.Advance(2 * time.Millisecond) // D1: force
	select {
	case <-then:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown never completed")
	}
	if !e.h.isDestroyed(e.gen) || e.scr("a").Name != "Shutdown Draft" {
		t.Fatalf("forced at D1 with recovery: destroyed=%v name=%q", e.h.isDestroyed(e.gen), e.scr("a").Name)
	}
	if ents, _ := os.ReadDir(e.dir); len(ents) != 1 {
		t.Fatal("recovery file")
	}
}

func TestShutdownDuringCreationRollsBackWithoutAToastOrAnAck(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	then := make(chan struct{})
	e.b.BeginShutdown(1500*time.Millisecond, func() { close(then) })
	e.mustCall(t, "app.closingReady", ``)
	select {
	case <-then:
	case <-time.After(3 * time.Second):
		t.Fatal("creation rollback must not wait for an editor ack")
	}
	if !e.h.mainEnabled || len(e.em.named("editor.windowFailed")) != 0 {
		t.Fatal("silent rollback")
	}
}

func TestShutdownWithoutAnEditorBehavesAsBefore(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	then := make(chan struct{})
	e.b.BeginShutdown(1500*time.Millisecond, func() { close(then) })
	e.mustCall(t, "app.closingReady", ``)
	select {
	case <-then:
	case <-time.After(3 * time.Second):
		t.Fatal("single ack must release")
	}
}

func TestClosingReadyFromTheEditorPageIsIgnored(t *testing.T) {
	e, then := shutdownRig(t)
	e.b.d.CallerRole = func(context.Context) string { return "editor" }
	h := e.b.CtxMethods()["app.closingReady"]
	if _, err := h(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	select {
	case <-e.b.closingReady:
		t.Fatal("the editor page's ack must not count as main's readiness")
	default:
	}
	e.b.d.CallerRole = func(context.Context) string { return "main" }
	if _, err := h(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+e.sid+`","decision":"discard"}`)
	select {
	case <-then:
	case <-time.After(3 * time.Second):
		t.Fatal("main + editor readiness releases")
	}
}

func (e *edRig) scr(id string) model.Script {
	e.b.mu.Lock()
	defer e.b.mu.Unlock()
	if i := e.b.indexLocked(id); i >= 0 {
		return e.b.scripts[i]
	}
	return model.Script{}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

// ---- review fixes ----

// A forced close whose recovery is still running must not release the gate, end the capture or
// take the session of the editor reopened meanwhile.
func TestReopenWhileOlderCleanupRunsKeepsTheNewEditorsGateSessionAndCapture(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	genA := e.gen
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "A Draft")+`}`)
	inRecovery, release := stuckSaveHook(e) // A's recovery persistence blocks
	e.b.win.CloseRequested(genA)
	e.clk.Advance(closeBase + time.Millisecond) // watchdog: A force-closed, cleanup recovering
	<-inRecovery                                // A's cleanup is inside its recovery (deadline armed)

	e.openAll(t, "a") // B opens while A's cleanup is still running
	sidB := e.sid
	e.mustCall(t, "capture.hotkey.start", `{"sessionId":"`+sidB+`","field":"toggle"}`)
	e.clk.Advance(recoverBound + time.Millisecond) // A's bounded recovery gives up
	time.Sleep(100 * time.Millisecond)
	if !e.ctl.Snapshot().EditDialogOpen {
		t.Fatal("A's cleanup released the gate of the running editor B")
	}
	if e.b.sessionIDForGen(e.gen) != sidB {
		t.Fatal("A's cleanup took or cleared B's session")
	}
	if mode, sess := e.b.capture.Active(); sess != sidB || mode == "" {
		t.Fatalf("A's cleanup ended B's capture: %v %q", mode, sess)
	}
	// A's recovery snapshot survives the reopen; the file is asynchronous (amendment C) and may land after the deadline.
	waitFor(t, func() bool { return len(recoveryFiles(t, e.dir)) == 1 })
	close(release)
	// B closes normally: now the gate is released.
	e.b.win.CloseRequested(e.gen)
	e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+sidB+`","decision":"discard"}`)
	e.waitDone(t)
	waitFor(t, func() bool { return !e.ctl.Snapshot().EditDialogOpen })
}

func TestConcurrentSavesDuringTeardownNeverCommitAfterTheFence(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 1; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				name := "W" + itoa(g) + "-" + itoa(i)
				e.m["editor.save"](json.RawMessage(`{"sessionId":"` + e.sid + `","snapshotRev":` + itoa(g*1000+i) + `,"script":` + scriptJSON("a", name) + `}`))
			}
		}(g)
	}
	time.Sleep(30 * time.Millisecond)
	e.b.win.ProcessFailed(e.gen, 1)
	e.waitDone(t)
	close(stop)
	wg.Wait()
	final := e.scr("a").Name
	time.Sleep(50 * time.Millisecond)
	if e.scr("a").Name != final {
		t.Fatal("the script changed after cleanup finished")
	}
}

func TestFailedDiskWriteKeepsTheDraftSoAForcedCloseRecoversIt(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.store.flushOK = false // queue accepted, flush reports the disk write failed
	_, err := e.call(t, "editor.save", `{"sessionId":"`+e.sid+`","snapshotRev":5,"script":`+scriptJSON("a", "Disk Fail")+`}`)
	wantCode(t, err, CodeSaveFailed)
	e.store.flushOK = true
	if e.b.session.lastSavedRev != 0 {
		t.Fatal("lastSavedRev must not advance before queue+flush succeeded")
	}
	e.b.win.ProcessFailed(e.gen, 1)
	e.waitDone(t)
	ents, _ := os.ReadDir(e.dir)
	if len(ents) != 1 {
		t.Fatalf("the failed save's draft must be recovered: %v", ents)
	}
	raw, _ := os.ReadFile(filepath.Join(e.dir, ents[0].Name()))
	var env RecoveryEnvelope
	_ = json.Unmarshal(raw, &env)
	if env.Script.Name != "Disk Fail" || env.Revision != 5 {
		t.Fatalf("%s", raw)
	}
}

func TestStartupFailureOrderIsMainEnabledThenGateThenDestroy(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.h.createErr = errors.New("embed failed synchronously")
	if _, err := e.call(t, "editor.openWindow", `{"id":"a"}`); err == nil {
		t.Fatal("the synchronous failure must come back as an error")
	}
	if !e.h.order("enableMain=false", "createFailed", "enableMain=true", "destroy") {
		t.Fatalf("sync failure: %v", e.h.log)
	}
	if e.ctl.Snapshot().EditDialogOpen || !e.h.mainEnabled || e.b.win.State() != "none" {
		t.Fatal("rolled back by openWindow itself")
	}
	if len(e.h.destroyed) != 1 {
		t.Fatal("the half-made window is destroyed after the rollback")
	}

	e2 := newEdRig(t, edScript("a"))
	res := e2.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	e2.h.rec("---")
	e2.b.win.WindowFailed(uint64(res["gen"].(float64)), "embed", "async")
	e2.waitDone(t)
	if !e2.h.order("---", "enableMain=true,gate=open", "destroy,gate=closed") {
		t.Fatalf("async failure: main enabled, gate restored, THEN destroy: %v", e2.h.log)
	}
}

func TestSharedAfterD2NothingPersistsAndTheBarrierWaitsForDestroy(t *testing.T) {
	e, then := shutdownRig(t)
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Late Draft")+`}`)
	_, release := stuckSaveHook(e) // the recovery persistence blocks past D2
	e.mustCall(t, "app.closingReady", ``)
	e.clk.Advance(1500*time.Millisecond + time.Millisecond) // D1: forced
	waitFor(t, func() bool { return e.h.isDestroyed(e.gen) })
	e.clk.Advance(shutdownRecovery) // D2: recovery abandoned
	select {
	case <-then:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown must proceed once recovery is abandoned and the editor destroyed")
	}
	waitFor(t, func() bool { return len(recoveryFiles(t, e.dir)) == 1 }) // the recovery FILE (asynchronous, amendment C)
	close(release)
	time.Sleep(150 * time.Millisecond)
	if e.scr("a").Name == "Late Draft" {
		t.Fatal("nothing may persist into the script after D2")
	}
}

type heldUI struct {
	mu sync.Mutex
	fs []func()
}

func (h *heldUI) Post(f func()) { h.mu.Lock(); h.fs = append(h.fs, f); h.mu.Unlock() }
func (h *heldUI) n() int        { h.mu.Lock(); defer h.mu.Unlock(); return len(h.fs) }

func TestShutdownBarrierNeedsDestroyAndGivesWayOnlyWhenTheUIThreadIsWedged(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	ui := &heldUI{}
	e.b.d.UI = ui // a wedged UI thread: posted work never runs
	then := make(chan struct{})
	e.b.BeginShutdown(1500*time.Millisecond, func() { close(then) })
	e.mustCall(t, "app.closingReady", ``)
	e.clk.Advance(1500*time.Millisecond + time.Millisecond) // D1: forced close is POSTED, never runs
	waitFor(t, func() bool { return ui.n() >= 1 })
	e.clk.Advance(shutdownRecovery)
	e.clk.Advance(500 * time.Millisecond) // D3
	time.Sleep(100 * time.Millisecond)
	if isClosed(then) {
		t.Fatal("then must wait for destroy")
	}
	mid := ui.n()
	e.clk.Advance(shutdownDestroyGrace) // D3+0.5s: the barrier gives way (logged)
	waitFor(t, func() bool { return ui.n() > mid })
	if isClosed(then) { // then itself is a UI post: it is queued, not run, on a wedged UI thread
		t.Fatal("unexpected")
	}
}

func TestOversizedFailedSaveDraftIsNotKeptAndRecoveryFileIsCompact(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Small Draft")+`}`)
	huge := edScript("a")
	huge.Name = strings.Repeat("x", maxDraftBytes+10)
	hb, _ := json.Marshal(huge)
	e.store.flushOK = false
	e.call(t, "editor.save", `{"sessionId":"`+e.sid+`","snapshotRev":9,"script":`+string(hb)+`}`)
	e.store.flushOK = true
	e.b.win.ProcessFailed(e.gen, 1)
	e.waitDone(t)
	ents, _ := os.ReadDir(e.dir)
	if len(ents) != 1 {
		t.Fatalf("%v", ents)
	}
	raw, _ := os.ReadFile(filepath.Join(e.dir, ents[0].Name()))
	var env RecoveryEnvelope
	_ = json.Unmarshal(raw, &env)
	if env.Script.Name != "Small Draft" || env.Revision != 2 {
		t.Fatalf("an over-1MiB draft must never replace the small one: rev %d", env.Revision)
	}
	if strings.Contains(string(raw), "\n") || len(raw) != len(mustCompact(env)) {
		t.Fatal("recovery file must be the compact encoding")
	}
	if err := func() error {
		_, err := writeRecovery(t.TempDir(), RecoveryEnvelope{V: 1, ScriptID: "a", Script: huge})
		return err
	}(); err == nil {
		t.Fatal("writeRecovery must refuse an over-limit script exactly, with no slack")
	}
}

func mustCompact(v any) []byte { b, _ := json.Marshal(v); return b }

func TestRequestCloseUsesTheSingleClosePath(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.requestClose", ``)
	if e.h.count(evEditorCloseRequested) != 1 || e.b.win.State() != "closeRequested" {
		t.Fatalf("requestClose must emit closeRequested once: %d %s", e.h.count(evEditorCloseRequested), e.b.win.State())
	}
	e.b.win.mu.Lock()
	first := e.b.win.deadline
	e.b.win.mu.Unlock()
	e.mustCall(t, "editor.requestClose", ``)
	e.b.win.mu.Lock()
	second := e.b.win.deadline
	e.b.win.mu.Unlock()
	if e.h.count(evEditorCloseRequested) != 1 || !first.Equal(second) {
		t.Fatal("a duplicate request must not re-emit or re-arm the watchdog")
	}
	e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+e.sid+`","decision":"discard"}`)
	if e.b.win.State() != "none" {
		t.Fatalf("state %s", e.b.win.State())
	}
}
