//go:build windows

package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"time"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/ui/webview"
)

// SelfTestMethod is the driver's only channel back to Go. It is registered ONLY when the
// process runs with the restricted self-test Policy (policy.go) and never in production.
const SelfTestMethod = "__selftest.report"

const (
	warmupCycles       = 5
	warmupCaptureLoops = 5
	leakSettle         = 1500 * time.Millisecond
	dialogCycles       = 50
	captureLoops       = 30
	// UI-layer callbacks: host window proc + shared key hook + overlay window proc.
	maxUICallbacks = 3
)

// driverJS runs inside the real page and drives the REAL API (same transport as the app):
// it installs its own message listener for responses with ids >= 9e6, so it never disturbs the
// application client, and reports through SelfTestMethod.
const driverJS = `(function(){if(window.__st)return;var wv=window.chrome.webview,seq=9000000,pend={},ev={};
wv.addEventListener('message',function(e){var m=e.data;if(!m)return;
if(m.k==='r'&&m.d&&pend[m.d.id]){var p=pend[m.d.id];delete pend[m.d.id];m.d.ok?p.res(m.d.result):p.rej(m.d.error)}
else if(m.k==='e'){ev[m.n]=(ev[m.n]||0)+1}});
function call(method,params){return new Promise(function(res,rej){var id=++seq;pend[id]={res:res,rej:rej};
wv.postMessage(JSON.stringify({id:id,method:method,params:params===undefined?{}:params}))})}
function sleep(ms){return new Promise(function(r){setTimeout(r,ms)})}
window.__st={call:call,sleep:sleep,count:function(n){return ev[n]||0},
report:function(tag,v){return call('__selftest.report',{tag:tag,v:v})},
run:async function(tag,fn){try{var v=await fn();await call('__selftest.report',{tag:tag,ok:true,v:v})}
catch(e){await call('__selftest.report',{tag:tag,ok:false,err:String(e&&(e.message||e.code)||JSON.stringify(e))})}},
cycles:async function(n){for(var i=1;i<=n;i++){var m=document.createElement('div');m.setAttribute('data-selftest','1');
m.style.cssText='position:fixed;inset:0;background:#0006';document.body.appendChild(m);
var s=await call('editor.open',{id:null});
await call('capture.hotkey.start',{sessionId:s.sessionId,field:'toggle'});
await call('capture.stop',{sessionId:s.sessionId});
await call('editor.close',{sessionId:s.sessionId,autosave:false});
m.remove();await sleep(0)}return n}};})();`

type selfReport struct {
	Tag string          `json:"tag"`
	OK  bool            `json:"ok"`
	V   json.RawMessage `json:"v"`
	Err string          `json:"err"`
}

// SelfTest drives -selftest. Build it, call Install on the method table, then run Run in a
// goroutine once the UI exists.
type SelfTest struct {
	UI      *webview.UI
	Res     *webview.Result
	Overlay *OverlayController
	Tray    *TrayIcon
	// Ready is closed by the host when the page called app.ready.
	Ready <-chan struct{}
	// Exit runs the ordered application shutdown on the UI thread.
	Exit func()

	// Editor-window lifecycle test (E1b).
	Host         *EditorHost
	RecoveredDir string
	EditOpen     func() bool // script execution is blocked (the controller's edit-dialog gate)
	Methods      map[string]func(json.RawMessage) (any, error)

	fixture        string
	shutdownStart  time.Time
	shutdownHWND   uintptr
	shutdownBefore int
	shutdownName   string // E1c: the real page's draft name ("" = the fixture's)
	e1cScriptID    string
	shotsOK        int
	shotsWant      int
	shotErrs       []string

	reports chan selfReport
}

// Install registers the driver channel on methods.
func (s *SelfTest) Install(methods map[string]func(json.RawMessage) (any, error)) {
	s.Methods = methods
	s.reports = make(chan selfReport, 16)
	methods[SelfTestMethod] = func(p json.RawMessage) (any, error) {
		var r selfReport
		if err := json.Unmarshal(p, &r); err != nil {
			return nil, errors.New("bad report")
		}
		select {
		case s.reports <- r:
		default:
		}
		return struct{}{}, nil
	}
}

func (s *SelfTest) drive(tag, expr string, timeout time.Duration) (selfReport, error) {
	s.UI.Eval(fmt.Sprintf("__st.run(%q,async function(){return %s})", tag, expr))
	return s.awaitReport(tag, timeout)
}

// Run executes the checks and always ends by requesting the ordered shutdown.
func (s *SelfTest) Run() {
	res := s.Res
	done := func() { s.UI.Post(s.Exit) }
	fail := func(name string, err error) { res.Step(name, false, err.Error()); done() }
	post := func(f func()) { s.UI.Post(f) }

	select {
	case <-s.Ready:
		res.Step("page loaded over https://app.invalid under CSP, app.ready (JS->Go)", true, "")
	case <-time.After(20 * time.Second):
		fail("page loaded + app.ready", errors.New("timeout"))
		return
	}
	s.UI.Eval(driverJS)
	time.Sleep(200 * time.Millisecond)

	r, err := s.drive("boot", `await __st.call('app.getBootState')`, 10*time.Second)
	if err != nil {
		fail("JS->Go call returns value", err)
		return
	}
	var boot struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(r.V, &boot)
	res.Step("JS->Go call returns value (getBootState)", boot.Mode == "main", "mode="+boot.Mode)

	paintsBefore := int64(0)
	if w := s.Overlay.Window(); w != nil {
		paintsBefore = w.Paints()
	}
	r, err = s.drive("events", `(async()=>{var b=__st.count('state');
await __st.call('app.setGlobalActive',{active:false});await __st.call('app.setGlobalActive',{active:true});
await __st.sleep(400);return __st.count('state')-b})()`, 15*time.Second)
	if err != nil {
		fail("Go->JS events", err)
		return
	}
	var n int
	_ = json.Unmarshal(r.V, &n)
	res.Step("Go->JS events (state) received", n >= 2, fmt.Sprintf("%d state events", n))

	// Leak check (handles/GDI/USER): warm up the WHOLE measured sequence (dialog cycles AND the
	// capture loop), settle, then two identical batches. A per-iteration leak grows in both
	// batches; a bounded one-time step (runtime/thread-pool growth) appears in one only.
	batch := func(tag string, cycles, loops int) error {
		if _, err := s.drive(tag, fmt.Sprintf("await __st.cycles(%d)", cycles), 90*time.Second); err != nil {
			return err
		}
		return captureLoop(loops)
	}
	settle := func() webview.Resources {
		runtime.GC()
		time.Sleep(leakSettle)
		return webview.SampleResources()
	}
	if err = batch("warmup", warmupCycles, warmupCaptureLoops); err != nil {
		fail("warm-up (dialog cycles + capture loop)", err)
		return
	}
	cbBefore := webview.CallbackCount()
	b0 := settle()
	if err = batch("cycles", dialogCycles, captureLoops); err != nil {
		fail(fmt.Sprintf("%d real dialog cycles (editor.open/capture/editor.close) + capture loop, batch 1", dialogCycles), err)
		return
	}
	b1 := settle()
	if err = batch("cycles2", dialogCycles, captureLoops); err != nil {
		fail(fmt.Sprintf("%d real dialog cycles + capture loop, batch 2", dialogCycles), err)
		return
	}
	b2 := settle()
	res.Step(fmt.Sprintf("2×%d real dialog cycles (editor.open/capture/editor.close) and 2×%d capture start/stop", dialogCycles, captureLoops), true, "")
	cbAfter := webview.CallbackCount()
	res.CheckSlope(b0, b1, b2)
	res.Step("NewCallback count constant", cbBefore == cbAfter && cbAfter <= maxUICallbacks,
		fmt.Sprintf("ui-layer calls %d -> %d (max %d: host proc, key hook, overlay proc)", cbBefore, cbAfter, maxUICallbacks))
	res.Callbacks = []int32{cbBefore, cbAfter}

	res.Step("navigation locked to app.invalid", s.UI.ProbeNavigationLock(5*time.Second), fmt.Sprintf("stats=%+v", s.UI.Stats()))

	overlayDone := make(chan struct{})
	post(func() {
		defer close(overlayDone)
		w := s.Overlay.Window()
		if w == nil {
			res.Step("overlay on UI thread", false, "overlay not created")
			return
		}
		hwnd, tid := w.Handle()
		ex := win.GetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE)
		want := int32(win.WS_EX_TOPMOST | win.WS_EX_LAYERED)
		res.Step("overlay on UI thread, layered+topmost", tid == s.UI.ThreadID() && ex&want == want,
			fmt.Sprintf("thread %d==%d exstyle=%#x", tid, s.UI.ThreadID(), ex))
		res.Step("overlay repainted on status change", w.Paints() > paintsBefore, fmt.Sprintf("%d -> %d WM_PAINT", paintsBefore, w.Paints()))
	})
	select {
	case <-overlayDone:
	case <-time.After(5 * time.Second):
		res.Step("overlay on UI thread", false, "UI thread did not respond")
	}

	s.overlayVisual(res)
	webview.SelftestEditorWindow(s.UI, res)
	if s.Host != nil {
		s.editorLifecycle()
		s.realEditorSuite()
		s.settingsHotkeySuite()
	}

	trayDone := make(chan bool, 1)
	post(func() {
		s.Tray.Dispose()
		s.Tray.Dispose()
		trayDone <- s.Tray.Disposed()
	})
	select {
	case ok := <-trayDone:
		res.Step("tray created and removed", ok, "")
	case <-time.After(5 * time.Second):
		res.Step("tray created and removed", false, "UI thread did not respond")
	}
	if s.Host != nil && !s.shutdownWithRealEditor() {
		s.editorShutdownCase()
	}
	done()
}

// ProductionHooks is what -selftest-production observes of the production startup sequence.
type ProductionHooks struct {
	Mounted     <-chan struct{} // closed on the frontend's app.mounted
	HotkeysDone <-chan struct{} // closed when startup hotkey registration finished

	FixedRegistered   func() int // global + overlay hotkeys actually registered
	ScriptHotkeyCount func() int
	GlobalActive      func() bool
	RunningScriptID   func() string

	GlobalHotkey      model.Hotkey
	ProbeHotkey       model.Hotkey // toggle hotkey of the fixture's input-free probe script
	ProbeID           string
	ExpectFixed       int
	AutoActiveEnabled bool   // the LOADED config's auto_active_enabled
	ExpectRow         string // a known script name in the list
	OverlayHotkey     model.Hotkey
}

func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

// RunProduction asserts that the production startup sequence produced a rendered, responsive
// window with working hotkeys, then requests the ordered shutdown (which unregisters them).
func (s *SelfTest) RunProduction(h ProductionHooks) {
	res := s.Res
	done := func() { s.UI.Post(s.Exit) }
	step := func(name string, ok bool, detail string) bool { return res.Step(name, ok, detail) }

	select {
	case <-h.Mounted:
		step("frontend mounted (app.mounted)", true, "")
	case <-time.After(20 * time.Second):
		step("frontend mounted (app.mounted)", false, "timeout 20s")
		done()
		return
	}

	pumped := make(chan struct{})
	s.UI.Post(func() { close(pumped) })
	select {
	case <-pumped:
		step("UI pump round-trip", true, "")
	case <-time.After(2 * time.Second):
		step("UI pump round-trip", false, "no response within 2s")
		done()
		return
	}

	s.UI.Eval(driverJS)
	time.Sleep(200 * time.Millisecond)
	expr := fmt.Sprintf(`await (async()=>{var want=%q;for(var i=0;i<80;i++){var a=document.querySelector('#app');
var rows=[].slice.call(document.querySelectorAll('[role=row]')).filter(function(r){return r.textContent.indexOf(want)>=0}).length;
if(a&&a.children.length>0&&rows>0)return{children:a.children.length,rows:rows,theme:document.documentElement.getAttribute('data-theme')};
await __st.sleep(100)}return null})()`, h.ExpectRow)
	r, err := s.drive("dom", expr, 15*time.Second)
	var dom struct {
		Children int    `json:"children"`
		Rows     int    `json:"rows"`
		Theme    string `json:"theme"`
	}
	if err == nil {
		_ = json.Unmarshal(r.V, &dom)
	}
	step("DOM rendered with the seeded script list", err == nil && dom.Children > 0 && dom.Rows > 0, fmt.Sprintf("err=%v %+v", err, dom))
	var smp SurfaceSample
	var smpErr error
	painted := waitFor(10*time.Second, func() bool {
		smp, smpErr = SampleClient(s.UI.HWND())
		return smpErr == nil && !smp.WhiteRun && smp.Distinct >= 4
	})
	step("window client area is painted (screen pixels, not a blank/white surface)", painted,
		fmt.Sprintf("err=%v size=%dx%d distinct=%d dominant=#%06X", smpErr, smp.W, smp.H, smp.Distinct, smp.Dominant))
	// Fail loudly if the fixture config was not the one loaded (e.g. missing from the checkout):
	// every expectation below is a literal from testdata/prod-fixture/config.json.
	themeOK := step("seeded theme applied (fixture: light)", dom.Theme == "light", "theme="+dom.Theme)
	want := model.Hotkey{Modifiers: []string{"ctrl", "alt", "shift"}, Key: "f9"}
	hkOK := step("seeded global hotkey loaded (fixture: ctrl+alt+shift+f9)", reflect.DeepEqual(h.GlobalHotkey, want), fmt.Sprintf("%+v", h.GlobalHotkey))
	aaOK := step("seeded auto-active enabled loaded (fixture: true)", h.AutoActiveEnabled, "")
	if !themeOK || !hkOK || !aaOK {
		step("fixture config loaded", false, "the production selftest ran on the default config; aborting before the hotkey steps")
		done()
		return
	}
	step("overlay window created", s.Overlay.Window() != nil, "")

	select {
	case <-h.HotkeysDone:
		step("startup hotkey registration finished", true, "")
	case <-time.After(15 * time.Second):
		step("startup hotkey registration finished", false, "timeout 15s")
		done()
		return
	}
	step("fixed hotkeys registered", h.FixedRegistered() == h.ExpectFixed, fmt.Sprintf("%d of %d", h.FixedRegistered(), h.ExpectFixed))

	fire := func(hk model.Hotkey, name string, cond func() bool) bool {
		if err := SendHotkey(hk); err != nil {
			return step(name, false, "SendInput: "+err.Error())
		}
		return step(name, waitFor(4*time.Second, cond), "")
	}
	if fire(h.GlobalHotkey, "global hotkey (SendInput) turns global active on", h.GlobalActive) {
		step("script hotkeys registered once active", waitFor(4*time.Second, func() bool { return h.ScriptHotkeyCount() > 0 }), fmt.Sprintf("count=%d", h.ScriptHotkeyCount()))
		for _, oc := range []struct {
			name, colour string
		}{{"status colours", ""}, {"custom colour", "#FF00FF"}} {
			s.overlayStatusE2E(res, h, oc.name, oc.colour, fire)
		}
		if fire(h.ProbeHotkey, "script toggle hotkey (SendInput) starts the script", func() bool { return h.RunningScriptID() == h.ProbeID }) {
			fire(h.ProbeHotkey, "script toggle hotkey (SendInput) stops the script", func() bool { return h.RunningScriptID() == "" })
		}
		if s.Host != nil {
			s.prodEditorGate(h, step)
		}
		fire(h.GlobalHotkey, "global hotkey (SendInput) turns global active off", func() bool { return !h.GlobalActive() })
		step("script hotkeys unregistered when inactive", waitFor(4*time.Second, func() bool { return h.ScriptHotkeyCount() == 0 }), fmt.Sprintf("count=%d", h.ScriptHotkeyCount()))
	}
	done()
}

// captureLoop starts and stops the key-capture hook thread n times (a second concurrent start must
// be rejected; stop is idempotent).
func captureLoop(n int) error {
	for i := 0; i < n; i++ {
		stop, err := webview.StartKeyCapture(func() bool { return false }, func(webview.KeyEvent) bool { return false })
		if err != nil {
			return err
		}
		if i == 0 {
			if _, err2 := webview.StartKeyCapture(func() bool { return false }, func(webview.KeyEvent) bool { return false }); err2 != webview.ErrCaptureActive {
				stop()
				return errors.New("second concurrent capture was not rejected")
			}
		}
		stop()
		stop() // idempotent
	}
	return nil
}
