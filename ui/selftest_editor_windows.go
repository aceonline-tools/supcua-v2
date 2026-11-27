package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/bridge"
)

// The editor-window lifecycle self-test (milestone E1b): the fixture page plays the editor page,
// the real bridge controller, host adapter and native window do everything else.

const lifecycleFixture = `<!doctype html><meta charset="utf-8"><title>editor lifecycle fixture</title><body>fixture
<script>
var MODE='%s',n=0,pend={},sid=null;
function call(m,p){return new Promise(function(r){var id=++n;pend[id]=r;chrome.webview.postMessage(JSON.stringify({id:id,method:m,params:p||{}}))})}
window.__resolve=function(d){var r=pend[d.id];if(r){delete pend[d.id];r(d)}};
window.__emit=function(name,p){
  if(name==='editor.measureRequest'){
    call('editor.measured',{token:p.token,cssW:1000,cssH:700,dpr:window.devicePixelRatio}).then(function(d){
      if(d.ok&&d.result.next==='remeasure')call('editor.measured',{token:d.result.token,cssW:d.result.cssW,cssH:700,dpr:window.devicePixelRatio});
    });
  }
  if(name==='editor.closeRequested'){
    if(MODE==='hung')return; // an unresponsive renderer: it simply stops answering
    call('editor.closeDecision',{sessionId:sid,decision:'discard'});
  }
};
(async function(){
  await call('editor.windowReady');
  var o=await call('editor.open',{id:null});
  sid=o.result.sessionId;var s=o.result.script;s.name='Fixture Draft';
  await call('editor.checkpoint',{sessionId:sid,revision:3,script:s});
})();
</script>`

var procIsWindowUI = syscall.NewLazyDLL("user32.dll").NewProc("IsWindow")

func hwndAlive(h uintptr) bool { r, _, _ := procIsWindowUI.Call(h); return r != 0 }

func (s *SelfTest) call(method, params string) (json.RawMessage, error) {
	h, ok := s.Methods[method]
	if !ok {
		return nil, fmt.Errorf("method %s not registered", method)
	}
	out, err := h(json.RawMessage(params))
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (s *SelfTest) step(name string, ok bool, detail string) bool {
	return s.Res.Step("editor lifecycle: "+name, ok, detail)
}

// openFixtureEditor opens the editor window with the fixture in the given mode and waits until it
// is visible. It reports main's enabled state at that moment.
func (s *SelfTest) openFixtureEditor(mode string) (hwnd uintptr, mainEnabledWhenVisible bool, err error) {
	s.fixture = fmt.Sprintf(lifecycleFixture, mode)
	if _, err = s.call("editor.openWindow", `{"id":null}`); err != nil {
		return 0, true, err
	}
	var h uintptr
	if !waitFor(40*time.Second, func() bool {
		if ed := s.UI.Editor(); ed != nil {
			if hw := ed.HWND(); hw != 0 && win.IsWindowVisible(win.HWND(hw)) {
				h = hw
				return true
			}
		}
		return false
	}) {
		return 0, true, fmt.Errorf("editor window never became visible")
	}
	return h, s.UI.MainEnabled(), nil
}

func (s *SelfTest) waitEditorGone(h uintptr, timeout time.Duration) (time.Duration, bool) {
	t0 := time.Now()
	ok := waitFor(timeout, func() bool { return s.UI.Editor() == nil && !hwndAlive(h) })
	return time.Since(t0), ok
}

func orderedIn(log []string, items ...string) bool {
	i := 0
	for _, l := range log {
		if i < len(items) && l == items[i] {
			i++
		}
	}
	return i == len(items)
}

// recoveredDrafts returns the recovery envelopes on disk whose script is the fixture's draft.
func (s *SelfTest) recoveredDrafts() []bridge.RecoveryEnvelope {
	ents, _ := os.ReadDir(s.RecoveredDir)
	var out []bridge.RecoveryEnvelope
	for _, e := range ents {
		raw, err := os.ReadFile(filepath.Join(s.RecoveredDir, e.Name()))
		if err != nil || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		var env bridge.RecoveryEnvelope
		if json.Unmarshal(raw, &env) == nil && env.Script.Name == "Fixture Draft" {
			out = append(out, env)
		}
	}
	return out
}

type geom struct {
	Outer, Client      [4]int32
	DPI                uint32
	Work               [4]int32
	WorkW, WorkH, CliW int32
}

func windowGeom(h uintptr) geom {
	var g geom
	var r, c win.RECT
	win.GetWindowRect(win.HWND(h), &r)
	win.GetClientRect(win.HWND(h), &c)
	g.Outer = [4]int32{r.Left, r.Top, r.Right, r.Bottom}
	g.Client = [4]int32{c.Left, c.Top, c.Right, c.Bottom}
	g.CliW = c.Right - c.Left
	if d, _, _ := syscall.NewLazyDLL("user32.dll").NewProc("GetDpiForWindow").Call(h); d != 0 {
		g.DPI = uint32(d)
	}
	mon := win.MonitorFromWindow(win.HWND(h), win.MONITOR_DEFAULTTONEAREST)
	mi := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if win.GetMonitorInfo(mon, &mi) {
		g.Work = [4]int32{mi.RcWork.Left, mi.RcWork.Top, mi.RcWork.Right, mi.RcWork.Bottom}
		g.WorkW, g.WorkH = mi.RcWork.Right-mi.RcWork.Left, mi.RcWork.Bottom-mi.RcWork.Top
	}
	return g
}

// editorLifecycle runs open/close, forced close with recovery, and reopen.
func (s *SelfTest) editorLifecycle() {
	s.Host.Fixture = func() string { return s.fixture }

	// A. open → main disabled, size fits and is clamped; WM_CLOSE → page decision → re-enable before destroy.
	s.Host.ResetLog()
	if !s.step("main enabled before open", s.UI.MainEnabled(), "") {
		return
	}
	h, mainEnabled, err := s.openFixtureEditor("responsive")
	if err != nil {
		s.step("open", false, err.Error())
		return
	}
	s.step("script hotkeys blocked and main disabled before the editor was interactive", !mainEnabled && s.EditOpen(),
		fmt.Sprintf("mainEnabledWhenVisible=%v editDialogOpen=%v", mainEnabled, s.EditOpen()))
	g := windowGeom(h)
	wantCli := int32(1000 * int(g.DPI) / 96)
	if wantCli > g.WorkW {
		wantCli = g.WorkW
	}
	outerW, outerH := g.Outer[2]-g.Outer[0], g.Outer[3]-g.Outer[1]
	fits := outerW <= g.WorkW && outerH <= g.WorkH && g.Outer[0] >= g.Work[0] && g.Outer[1] >= g.Work[1] && g.Outer[2] <= g.Work[2] && g.Outer[3] <= g.Work[3]
	s.step("opened at the measured size converted by the real DPI, clamped to the work area", fits && absInt32(g.CliW-wantCli) <= 24,
		fmt.Sprintf("outer=%v client=%v dpi=%d work=%v wantClientW~%d", g.Outer, g.Client, g.DPI, g.Work, wantCli))
	s.step("editor is owned by main and main is still disabled while it is open", win.GetWindow(win.HWND(h), win.GW_OWNER) == win.HWND(s.UI.HWND()) && !s.UI.MainEnabled(), "")

	// Display change: park the window far off-screen (as a removed monitor would leave it), send
	// WM_DISPLAYCHANGE, and it must come back onto a monitor.
	win.SetWindowPos(win.HWND(h), 0, 20000, 20000, 0, 0, win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	win.SendMessage(win.HWND(h), 0x007E, 32, 0)
	g2 := windowGeom(h)
	s.step("WM_DISPLAYCHANGE moves an off-screen editor back onto the main monitor", g2.Outer[0] >= g2.Work[0] && g2.Outer[1] >= g2.Work[1] && g2.Outer[0] < g2.Work[2] && g2.Outer[1] < g2.Work[3],
		fmt.Sprintf("outer=%v work=%v", g2.Outer, g2.Work))

	win.PostMessage(win.HWND(h), win.WM_CLOSE, 0, 0) // the X / Alt-F4 path
	d, gone := s.waitEditorGone(h, 15*time.Second)
	log := s.Host.Log()
	s.step("WM_CLOSE → closeRequested → page decision → host cleanup closes it", gone, fmt.Sprintf("closed in %s", d.Round(time.Millisecond)))
	s.step("main re-enabled BEFORE the editor was destroyed", orderedIn(log, "enableMain=false", "create", "enableMain=true", "destroy") && s.UI.MainEnabled(), fmt.Sprint(log))
	s.step("session cleanup released script execution", waitFor(5*time.Second, func() bool { return !s.EditOpen() }), "")

	// B. unresponsive renderer: the watchdog force-closes it and the checkpoint is recovered on disk.
	before := len(s.recoveredDrafts())
	h, mainEnabled, err = s.openFixtureEditor("hung")
	if err != nil {
		s.step("open (hung fixture)", false, err.Error())
		return
	}
	waitFor(5*time.Second, func() bool { return true })
	time.Sleep(1500 * time.Millisecond) // let the fixture post its checkpoint
	t0 := time.Now()
	win.PostMessage(win.HWND(h), win.WM_CLOSE, 0, 0)
	d, gone = s.waitEditorGone(h, 20*time.Second)
	el := time.Since(t0)
	s.step("watchdog fired at ~10 s and the editor was gone by T_f+1 s", gone && el >= 9500*time.Millisecond && el <= 11800*time.Millisecond,
		fmt.Sprintf("closed %s after the close request", el.Round(time.Millisecond)))
	s.step("main re-enabled after a forced close", s.UI.MainEnabled(), "")
	ok := waitFor(10*time.Second, func() bool { return len(s.recoveredDrafts()) > before && !s.EditOpen() })
	drafts := s.recoveredDrafts()
	var rev uint64
	if len(drafts) > 0 {
		rev = drafts[len(drafts)-1].Revision
	}
	s.step("forced close recovered the checkpoint into recovered/ (envelope v1, revision 3)", ok && rev == 3 && drafts[len(drafts)-1].V == 1,
		fmt.Sprintf("files with the draft: %d (was %d) revision=%d dir=%s", len(drafts), before, rev, s.RecoveredDir))

	// C. reopen after a forced close.
	h, mainEnabled, err = s.openFixtureEditor("responsive")
	if err != nil {
		s.step("reopen after a forced close", false, err.Error())
		return
	}
	s.step("reopened after a forced close (new window, main disabled)", !mainEnabled, "")
	win.PostMessage(win.HWND(h), win.WM_CLOSE, 0, 0)
	_, gone = s.waitEditorGone(h, 15*time.Second)
	s.step("and closed normally again", gone && s.UI.MainEnabled(), "")
	waitFor(5*time.Second, func() bool { return !s.EditOpen() })
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// editorShutdownCase leaves an unresponsive editor open and starts the ordered shutdown; AfterRun
// verifies the outcome once the message loop ended.
func (s *SelfTest) editorShutdownCase() bool {
	s.shutdownBefore = len(s.recoveredDrafts())
	h, _, err := s.openFixtureEditor("hung")
	if err != nil {
		s.step("shutdown with the editor open: open", false, err.Error())
		return false
	}
	s.shutdownHWND = h
	time.Sleep(1500 * time.Millisecond) // the fixture's checkpoint
	s.shutdownStart = time.Now()
	return true
}

// AfterRun records the shutdown-with-editor-open checks. Call it after the UI message loop returned.
func (s *SelfTest) AfterRun() {
	if s.shutdownStart.IsZero() {
		return
	}
	el := time.Since(s.shutdownStart)
	gone := s.UI.Editor() == nil && !hwndAlive(s.shutdownHWND)
	n := len(s.recoveredDrafts())
	if s.shutdownName != "" {
		n = len(s.draftsNamed(s.shutdownName))
	}
	s.step("shutdown with an unresponsive (real or fixture) editor open completes within the worst-case bound and destroys it", gone && el <= 7*time.Second,
		fmt.Sprintf("shutdown %s (bound 5.5 s + teardown), editor destroyed=%v", el.Round(time.Millisecond), gone))
	s.step("and its checkpoint was recovered to disk", n > s.shutdownBefore, fmt.Sprintf("files with the draft %d -> %d", s.shutdownBefore, n))
}
