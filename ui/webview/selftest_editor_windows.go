package webview

import (
	"fmt"
	"time"

	"github.com/lxn/win"
)

// editorFixture is the page the self-test loads into the second window. It calls four methods in
// sequence, each only after it saw the previous reply, so the number of calls the host received
// from the editor window proves which replies reached the page:
//
//	1 actions.catalog  (allowed for both roles)  → ok
//	2 state.get        (allowed for both roles)  → ok
//	3 scripts.list     (main only)               → FORBIDDEN_ROLE
//	4 actions.catalog                            → ok (sent only after the error in 3 was seen)
const editorFixture = `<!doctype html><meta charset="utf-8"><title>editor fixture</title><body>fixture
<script>
function call(id,m){chrome.webview.postMessage(JSON.stringify({id:id,method:m}))}
window.__emit=function(){};
window.__resolve=function(d){
  if(d.id===1&&d.ok)call(2,'state.get');
  else if(d.id===2&&d.ok)call(3,'scripts.list');
  else if(d.id===3&&!d.ok&&d.error&&d.error.code==='FORBIDDEN_ROLE')call(4,'actions.catalog');
};
call(1,'actions.catalog');
</script>`

var procIsWindow = user32.NewProc("IsWindow")

func isWindow(h win.HWND) bool {
	r, _, _ := procIsWindow.Call(uintptr(h))
	return r != 0
}

func (u *UI) onUIThread(f func()) bool {
	done := make(chan struct{})
	u.Post(func() { defer close(done); f() })
	select {
	case <-done:
		return true
	case <-time.After(10 * time.Second):
		return false
	}
}

func waitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// quiet waits until every page call of w has been answered at w (submitted == responses) for
// several consecutive samples.
func quiet(w *Window, timeout time.Duration) bool {
	stable := 0
	return waitUntil(timeout, func() bool {
		if w.submitted.Load() == w.respFrames.Load() {
			stable++
		} else {
			stable = 0
		}
		time.Sleep(30 * time.Millisecond)
		return stable >= 4
	})
}

// SelftestEditorWindow is the self-test-only hook that exercises the second window (host/window
// routing, E1a.2): it creates a hidden owned editor window from the same environment, loads the
// fixture page, round-trips RPC through the shared dispatcher with replies arriving only at that
// window, then closes it and checks the late-reply suppression. Call from a non-UI goroutine
// after the main page is up. Returns whether every step passed.
func SelftestEditorWindow(u *UI, res *Result) bool {
	ok := true
	step := func(name string, pass bool, detail string) {
		if !res.Step("editor window: "+name, pass, detail) {
			ok = false
		}
	}
	cbBefore := CallbackCount()
	if !quiet(u.Window, 10*time.Second) {
		step("main window quiescent before the test", false, fmt.Sprintf("main submitted=%d responses=%d", u.submitted.Load(), u.respFrames.Load()))
		return false
	}
	mainSub0, mainResp0 := u.submitted.Load(), u.respFrames.Load()

	ready := make(chan struct{}, 1)
	failed := make(chan error, 1)
	var w *Window
	var cerr error
	t0 := time.Now()
	if !u.onUIThread(func() {
		w, cerr = u.CreateEditorWindow(EditorOptions{
			Title: "editor window selftest", Fixture: editorFixture,
			OnReady:  func(*Window) { ready <- struct{}{} },
			OnFailed: func(_ *Window, err error) { failed <- err },
		})
	}) || cerr != nil {
		step("created", false, fmt.Sprint("create: ", cerr))
		return false
	}
	var owned, hidden, enabled bool
	var genEditor, genMain uint64
	u.onUIThread(func() {
		owned = win.GetWindow(w.hwnd, win.GW_OWNER) == u.hwnd
		hidden = !win.IsWindowVisible(w.hwnd)
		enabled = win.IsWindowEnabled(u.hwnd)
		genEditor, genMain = w.gen, u.gen
	})
	step("created hidden and owned by main (non-blocking, call returned in "+time.Since(t0).Round(time.Millisecond).String()+")",
		owned && hidden && genEditor != genMain, fmt.Sprintf("owned=%v hidden=%v genEditor=%d genMain=%d mainEnabled=%v (modality is E1b)", owned, hidden, genEditor, genMain, enabled))

	select {
	case <-ready:
		var sameEnv bool
		u.onUIThread(func() { sameEnv = w.browser != nil && w.browser.Environment() == u.browser.Environment() })
		step("controller created from the same environment and set up on the UI thread", sameEnv, fmt.Sprintf("sameEnvironment=%v in %s", sameEnv, time.Since(t0).Round(time.Millisecond)))
	case err := <-failed:
		step("controller created from the same environment", false, fmt.Sprint(err))
		return false
	case <-time.After(30 * time.Second):
		step("controller created from the same environment", false, "timeout")
		u.onUIThread(func() { u.destroyEditor(w) })
		return false
	}

	mainPresented := u.presented.Load()
	u.onUIThread(func() { win.ShowWindow(w.hwnd, win.SW_SHOWNOACTIVATE) })
	step("WM_SHOWWINDOW Reassert runs for this window and not for main",
		waitUntil(5*time.Second, func() bool { return w.presented.Load() >= 1 }) && u.presented.Load() == mainPresented,
		fmt.Sprintf("editorPresented=%d mainPresentedDelta=%d", w.presented.Load(), u.presented.Load()-mainPresented))

	roundTrip := waitUntil(20*time.Second, func() bool { return w.submitted.Load() >= 4 && w.respFrames.Load() >= 4 })
	step("fixture page round-trips 4 calls (allowed, allowed, FORBIDDEN_ROLE seen by the page, allowed)", roundTrip,
		fmt.Sprintf("editor submitted=%d responses=%d", w.submitted.Load(), w.respFrames.Load()))
	mainQuiet := quiet(u.Window, 10*time.Second)
	dSub, dResp := u.submitted.Load()-mainSub0, u.respFrames.Load()-mainResp0
	step("every reply reached its own window (editor responses == editor calls, main responses == main calls)",
		mainQuiet && w.submitted.Load() == w.respFrames.Load() && dSub == dResp,
		fmt.Sprintf("editor %d/%d; main delta submitted=%d responses=%d", w.submitted.Load(), w.respFrames.Load(), dSub, dResp))

	hwnd := w.hwnd
	var gen uint64
	var closedOK bool
	u.onUIThread(func() {
		gen = w.gen
		u.DestroyEditorWindow(gen)
		closedOK = !isWindow(hwnd) && windowOf(hwnd) == nil && u.Editor() == nil && u.editorGen.Load() == 0
	})
	step("closed: HWND destroyed, registry entry removed, generation invalidated", closedOK, fmt.Sprintf("gen=%d", gen))

	jobRan := make(chan struct{})
	enq := u.EnqueueRPCJob(func() { close(jobRan) })
	var alive bool
	if enq {
		select {
		case <-jobRan:
			alive = true
		case <-time.After(5 * time.Second):
		}
	}
	step("closing the editor window leaves the shared dispatcher running", alive, fmt.Sprintf("enqueued=%v ran=%v", enq, alive))

	respBefore, mainBefore := w.respFrames.Load(), u.respFrames.Load()
	u.disp.Submit(`{"id":99,"method":"actions.catalog"}`, w.caller(), w.reply) // a late call from the closed window
	time.Sleep(400 * time.Millisecond)
	step("late call/reply for the closed editor window is suppressed (nothing delivered anywhere)",
		w.respFrames.Load() == respBefore && u.respFrames.Load() == mainBefore,
		fmt.Sprintf("editorFrames %d->%d mainFrames %d->%d", respBefore, w.respFrames.Load(), mainBefore, u.respFrames.Load()))

	ok = selftestEditorSecurityFailure(u, step) && ok

	cbAfter := CallbackCount()
	step("no new NewCallback sites (same class and hostProc)", cbBefore == cbAfter, fmt.Sprintf("count %d -> %d", cbBefore, cbAfter))
	return ok
}

// selftestEditorSecurityFailure fires the editor's FatalCallback (a security handler failure) and
// asserts the native teardown still runs: HWND destroyed, registry entry removed, controller
// closed (its environment reference released), generation invalidated, main window untouched.
func selftestEditorSecurityFailure(u *UI, step func(string, bool, string)) bool {
	ready := make(chan struct{}, 1)
	var w *Window
	var cerr error
	if !u.onUIThread(func() {
		w, cerr = u.CreateEditorWindow(EditorOptions{Title: "editor security failure", Fixture: editorFixture,
			OnReady: func(*Window) { ready <- struct{}{} }})
	}) || cerr != nil {
		step("security failure: created", false, fmt.Sprint(cerr))
		return false
	}
	select {
	case <-ready:
	case <-time.After(30 * time.Second):
		u.onUIThread(func() { u.destroyEditor(w) })
		step("security failure: controller ready", false, "timeout")
		return false
	}
	hwnd := w.hwnd
	mainHwnd := u.hwnd
	u.onUIThread(func() { w.browser.FatalCallback(fmt.Errorf("selftest: injected security failure")) })
	torn := waitUntil(10*time.Second, func() bool { return w.torn.Load() })
	var detail string
	var good bool
	u.onUIThread(func() {
		good = torn && !isWindow(hwnd) && windowOf(hwnd) == nil && u.Editor() == nil && u.editorGen.Load() == 0 &&
			w.browser == nil && w.gone.Load() && isWindow(mainHwnd) && u.browser != nil
		detail = fmt.Sprintf("torn=%v hwndAlive=%v registered=%v editorNil=%v genCleared=%v browserCleared=%v gone=%v mainAlive=%v",
			torn, isWindow(hwnd), windowOf(hwnd) != nil, u.Editor() == nil, u.editorGen.Load() == 0, w.browser == nil, w.gone.Load(), isWindow(mainHwnd))
	})
	step("security failure: native teardown still runs (HWND, registry, browser cleared, generation)", good, detail)
	return good
}
