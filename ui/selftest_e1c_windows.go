package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/model"
)

// Milestone E1c: the integrated editor window with the REAL E2 page (no fixture). The real bridge
// controller, host adapter, native window and frontend do everything; the self-test only injects
// the same driver the main page uses and reads the page's own metrics.

const edShotsEnv = "SUPCUA_EDITOR_SHOTS"

// edMetricsJS reads the page's layout: the page's own __editorMetrics, the blank height at the bottom
// of every column (column box bottom minus its last child's bottom), the root and document scroll
// extents, and the font actually used.
const edMetricsJS = `(function(){var m=window.__editorMetrics?window.__editorMetrics():null;var root=document.getElementById('editor-root');
var cols=[].slice.call(document.querySelectorAll('.editor-col')).map(function(c){var r=c.getBoundingClientRect(),b=r.top;[].slice.call(c.children).forEach(function(k){b=Math.max(b,k.getBoundingClientRect().bottom)});return Math.round(r.bottom-b)});
var de=document.documentElement;
return {m:m,blank:cols,root:root?[root.scrollWidth,root.clientWidth,root.scrollHeight,root.clientHeight]:null,doc:[de.scrollWidth,de.clientWidth,de.scrollHeight,de.clientHeight],
font:getComputedStyle(document.body).fontFamily,segoe:document.fonts?document.fonts.check('12px "Segoe UI"'):null}})()`

type edMetrics struct {
	M *struct {
		Measuring    bool    `json:"measuring"`
		ViewportW    float64 `json:"viewportW"`
		ViewportH    float64 `json:"viewportH"`
		ActionsListH float64 `json:"actionsListH"`
		GapBottom    float64 `json:"gapBottom"`
		CssW         float64 `json:"cssW"`
		CssH         float64 `json:"cssH"`
	} `json:"m"`
	Blank []float64 `json:"blank"`
	Root  []float64 `json:"root"`
	Doc   []float64 `json:"doc"`
	Font  string    `json:"font"`
	Segoe *bool     `json:"segoe"`
}

func (m edMetrics) noScroll() bool {
	in := func(v []float64) bool { return len(v) == 4 && v[0] <= v[1]+1 && v[2] <= v[3]+1 }
	return in(m.Root) && in(m.Doc)
}

func (m edMetrics) maxBlank() float64 {
	x := 0.0
	for _, b := range m.Blank {
		if b > x {
			x = b
		}
	}
	return x
}

func (m edMetrics) String() string {
	if m.M == nil {
		return "no metrics"
	}
	return fmt.Sprintf("viewport=%.0fx%.0f content=%.0fx%.0f blankPerColumn=%v root[sw,cw,sh,ch]=%v doc=%v actionsListH=%.0f font=%q segoeAvailable=%v",
		m.M.ViewportW, m.M.ViewportH, m.M.CssW, m.M.CssH, m.Blank, m.Root, m.Doc, m.M.ActionsListH, m.Font, m.Segoe != nil && *m.Segoe)
}

// awaitReport waits for the driver report with this tag (main or editor page).
func (s *SelfTest) awaitReport(tag string, timeout time.Duration) (selfReport, error) {
	deadline := time.After(timeout)
	for {
		select {
		case r := <-s.reports:
			if r.Tag != tag {
				continue
			}
			if !r.OK {
				return r, errors.New(r.Err)
			}
			return r, nil
		case <-deadline:
			return selfReport{}, errors.New("timeout")
		}
	}
}

var edSeq int

// driveEd runs expr in the editor page through the same driver as the main page.
func (s *SelfTest) driveEd(expr string, timeout time.Duration) (json.RawMessage, error) {
	ed := s.UI.Editor()
	if ed == nil {
		return nil, errors.New("no editor window")
	}
	edSeq++
	tag := fmt.Sprintf("ed-%d", edSeq)
	ed.Eval(driverJS)
	ed.Eval(fmt.Sprintf("__st.run(%q,async function(){return %s})", tag, expr))
	r, err := s.awaitReport(tag, timeout)
	return r.V, err
}

func (s *SelfTest) edMetrics() (edMetrics, error) {
	var m edMetrics
	raw, err := s.driveEd(edMetricsJS, 5*time.Second)
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(raw, &m)
}

// waitSettled waits until the page left measure mode and two consecutive reads agree.
func (s *SelfTest) waitSettled(timeout time.Duration) (edMetrics, bool) {
	var last edMetrics
	prev := ""
	ok := waitFor(timeout, func() bool {
		m, err := s.edMetrics()
		if err != nil || m.M == nil || m.M.Measuring || m.M.ViewportW == 0 {
			return false
		}
		last = m
		cur := m.String()
		same := cur == prev
		prev = cur
		time.Sleep(150 * time.Millisecond)
		return same
	})
	return last, ok
}

// openRealEditor opens the editor window for id ("" = New) with the real page and waits until it
// is visible and its page settled. It reports main's enabled state once the editor is visible.
func (s *SelfTest) openRealEditor(id string) (h uintptr, mainEnabledWhileOpen bool, m edMetrics, err error) {
	s.fixture = ""
	p := `{"id":null}`
	if id != "" {
		p = fmt.Sprintf(`{"id":%q}`, id)
	}
	if _, err = s.call("editor.openWindow", p); err != nil {
		return 0, true, m, err
	}
	if !waitFor(40*time.Second, func() bool {
		if ed := s.UI.Editor(); ed != nil {
			if hw := ed.HWND(); hw != 0 && win.IsWindowVisible(win.HWND(hw)) {
				h = hw
				return true
			}
		}
		return false
	}) {
		return 0, true, m, errors.New("editor window never became visible")
	}
	mainEnabledWhileOpen = s.UI.MainEnabled()
	var ok bool
	if m, ok = s.waitSettled(15 * time.Second); !ok {
		return h, mainEnabledWhileOpen, m, fmt.Errorf("the page never settled: %s", m)
	}
	return h, mainEnabledWhileOpen, m, nil
}

func (s *SelfTest) shot(name string, h uintptr) {
	dir := os.Getenv(edShotsEnv)
	if dir == "" || h == 0 {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	var r win.RECT
	win.GetWindowRect(win.HWND(h), &r)
	w, ht := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if w <= 0 || ht <= 0 {
		return
	}
	time.Sleep(200 * time.Millisecond) // the last frame is on screen
	if pix, err := captureScreen(int(r.Left), int(r.Top), w, ht); err == nil {
		savePNG(filepath.Join(dir, name+".png"), pix, w, ht, 1)
	}
}

func (s *SelfTest) scriptIDByName(name string) string {
	raw, err := s.call("scripts.list", "")
	if err != nil {
		return ""
	}
	var l struct {
		Scripts []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"scripts"`
	}
	_ = json.Unmarshal(raw, &l)
	for _, sc := range l.Scripts {
		if sc.Name == name {
			return sc.ID
		}
	}
	return ""
}

func (s *SelfTest) setEditorName(name string) error {
	_, err := s.driveEd(fmt.Sprintf(`(function(){var i=document.getElementById('ed-name');i.value=%q;i.dispatchEvent(new Event('input',{bubbles:true}));return i.value})()`, name), 5*time.Second)
	return err
}

// muteEditor makes the real page unresponsive to the host (every post is dropped): the page can no
// longer answer closeRequested, exactly like a hung renderer, without blocking the shared renderer.
func (s *SelfTest) muteEditor() error {
	_, err := s.driveEd(`(function(){var wv=window.chrome.webview;setTimeout(function(){wv.postMessage=function(){}},0);return true})()`, 5*time.Second)
	return err
}

func (s *SelfTest) draftsNamed(name string) []bridge.RecoveryEnvelope {
	ents, _ := os.ReadDir(s.RecoveredDir)
	var out []bridge.RecoveryEnvelope
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.RecoveredDir, e.Name()))
		if err != nil || len(raw) == 0 {
			continue
		}
		var env bridge.RecoveryEnvelope
		if json.Unmarshal(raw, &env) == nil && env.Script.Name == name {
			out = append(out, env)
		}
	}
	return out
}

const mainToastJS = `(function(){if(window.__e1c)return;window.__e1c={};window.chrome.webview.addEventListener('message',function(e){var m=e.data;
if(m&&m.k==='e'&&(m.n==='editor.windowClosed'||m.n==='editor.windowFailed'))window.__e1c[m.n]=m.p})})()`

func (s *SelfTest) estep(name string, ok bool, detail string) bool {
	return s.Res.Step("editor E1c: "+name, ok, detail)
}

func clientCSS(h uintptr) (w, ht float64, dpi uint32) {
	g := windowGeom(h)
	d := g.DPI
	if d == 0 {
		d = 96
	}
	return float64(g.Client[2]-g.Client[0]) * 96 / float64(d), float64(g.Client[3]-g.Client[1]) * 96 / float64(d), d
}

// realEditorSuite is E1c on the diagnostic self-test (contract §12/§13).
func (s *SelfTest) realEditorSuite() {
	s.UI.Eval(mainToastJS)

	// 1. New: open, natural size, then WM_CLOSE with a dirty session → guard → save.
	before := s.UI.MainEnabled()
	t0 := time.Now()
	h, enabledWhile, m, err := s.openRealEditor("")
	if !s.estep("New opens the real editor page", err == nil, fmt.Sprintf("in %s err=%v", time.Since(t0).Round(time.Millisecond), err)) {
		return
	}
	g := windowGeom(h)
	s.estep("main enabled before / disabled while open", before && !enabledWhile, fmt.Sprintf("before=%v while=%v", before, enabledWhile))
	s.estep("natural size: measured content, client/outer bounds, DPI, work area", m.M != nil,
		fmt.Sprintf("measured=%.0fx%.0f outer=%v client=%v dpi=%d work=%v", m.M.CssW, m.M.CssH, g.Outer, g.Client, g.DPI, g.Work))
	s.estep("natural size: no empty space (≤24px blank per column) and the root does not scroll", m.maxBlank() <= 24 && m.noScroll(), m.String())
	s.shot("1-new-natural-editor", h)
	s.shot("1-new-natural-main-disabled", s.UI.HWND())

	if err := s.setEditorName("E1c Created"); err != nil {
		s.estep("edit the name (dirty session)", false, err.Error())
		return
	}
	t0 = time.Now()
	win.PostMessage(win.HWND(h), win.WM_CLOSE, 0, 0)
	_, gone := s.waitEditorGone(h, 15*time.Second)
	id := ""
	waitFor(5*time.Second, func() bool { id = s.scriptIDByName("E1c Created"); return id != "" })
	s.estep("WM_CLOSE with a dirty session → guard → saved → closed", gone && id != "", fmt.Sprintf("closed in %s, saved id=%q", time.Since(t0).Round(time.Millisecond), id))
	s.estep("main enabled after close", waitFor(3*time.Second, s.UI.MainEnabled), "")
	if id == "" {
		return
	}
	waitFor(5*time.Second, func() bool { return !s.EditOpen() })

	// 2. Existing script: natural size, larger, the floor, capture targeting, in-page Close.
	h, enabledWhile, m, err = s.openRealEditor(id)
	if !s.estep("existing script opens the real editor page", err == nil && !enabledWhile, fmt.Sprintf("err=%v mainEnabledWhileOpen=%v %s", err, enabledWhile, m)) {
		return
	}
	nat := m
	s.shot("2-existing-natural-editor", h)
	g = windowGeom(h)
	ow, oh := g.Outer[2]-g.Outer[0], g.Outer[3]-g.Outer[1]
	dx, dy := min32(320, g.WorkW-ow), min32(220, g.WorkH-oh)
	win.SetWindowPos(win.HWND(h), 0, g.Work[0], g.Work[1], ow+dx, oh+dy, win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	m, _ = s.waitSettled(5 * time.Second)
	grew := nat.M != nil && m.M != nil && m.M.ActionsListH > nat.M.ActionsListH
	s.estep("larger: reflows, the root does not scroll and the action list takes the extra height", m.noScroll() && (grew || dy <= 0),
		fmt.Sprintf("grew outer by %dx%d; actionsListH %.0f -> %.0f; %s", dx, dy, nat.M.ActionsListH, m.M.ActionsListH, m))
	s.shot("3-existing-larger-editor", h)

	win.SetWindowPos(win.HWND(h), 0, 0, 0, 200, 150, win.SWP_NOMOVE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	m, _ = s.waitSettled(5 * time.Second)
	cw, ch, dpi := clientCSS(h)
	s.estep("smaller: clamped at the 720×480 floor and the root does not scroll", cw >= float64(bridge.MinCssW)-1 && ch >= float64(bridge.MinCssH)-1 && m.noScroll(),
		fmt.Sprintf("client css=%.0fx%.0f dpi=%d; %s", cw, ch, dpi, m))
	s.shot("4-existing-floor-editor", h)
	s.shot("4-existing-floor-main-disabled", s.UI.HWND())

	s.captureTargeting(h)

	t0 = time.Now()
	_, err = s.driveEd(`(function(){var b=document.querySelector('.editor-foot .primary');b.click();return !!b})()`, 5*time.Second)
	_, gone = s.waitEditorGone(h, 15*time.Second)
	s.estep("in-page Close (editor.requestClose) → guard → closed", err == nil && gone, fmt.Sprintf("closed in %s err=%v", time.Since(t0).Round(time.Millisecond), err))
	waitFor(5*time.Second, func() bool { return !s.EditOpen() })

	// 3. An unresponsive real page: the watchdog force-closes it; recovery file + the toast event to main.
	h, _, _, err = s.openRealEditor(id)
	if !s.estep("open for the forced close", err == nil, fmt.Sprint(err)) {
		return
	}
	if err = s.setEditorName("E1c Hung Draft"); err == nil {
		time.Sleep(2500 * time.Millisecond) // the page's checkpoint
		err = s.muteEditor()
	}
	if !s.estep("dirty the page and make it unresponsive", err == nil, fmt.Sprint(err)) {
		return
	}
	t0 = time.Now()
	win.PostMessage(win.HWND(h), win.WM_CLOSE, 0, 0)
	_, gone = s.waitEditorGone(h, 20*time.Second)
	el := time.Since(t0)
	s.estep("watchdog force-closed the unresponsive page at ~10 s, gone by T_f+1 s", gone && el >= 9500*time.Millisecond && el <= 11800*time.Millisecond, fmt.Sprintf("closed %s after WM_CLOSE", el.Round(time.Millisecond)))
	rec := waitFor(10*time.Second, func() bool { return len(s.draftsNamed("E1c Hung Draft")) > 0 })
	var toast struct {
		Forced        bool   `json:"forced"`
		RecoveredPath string `json:"recoveredPath"`
	}
	waitFor(5*time.Second, func() bool {
		r, e := s.drive("e1c-toast", `window.__e1c['editor.windowClosed']||null`, 3*time.Second)
		return e == nil && json.Unmarshal(r.V, &toast) == nil && toast.RecoveredPath != ""
	})
	s.estep("recovery file written and the toast event reached main", rec && toast.Forced && toast.RecoveredPath != "",
		fmt.Sprintf("files=%d windowClosed{forced:%v recoveredPath:%q}", len(s.draftsNamed("E1c Hung Draft")), toast.Forced, toast.RecoveredPath))
	s.estep("main enabled after the forced close", s.UI.MainEnabled(), "")
	waitFor(5*time.Second, func() bool { return !s.EditOpen() })

	// 4. Reopen after the forced close.
	h, enabledWhile, m, err = s.openRealEditor(id)
	s.estep("reopen after a forced close (new window, main disabled, page settled)", err == nil && !enabledWhile, fmt.Sprintf("err=%v %s", err, m))
	if err == nil {
		win.PostMessage(win.HWND(h), win.WM_CLOSE, 0, 0)
		_, gone = s.waitEditorGone(h, 15*time.Second)
		s.estep("and closed again", gone && waitFor(3*time.Second, s.UI.MainEnabled), "")
	}
	waitFor(5*time.Second, func() bool { return !s.EditOpen() })
	s.e1cScriptID = id
}

// captureTargeting checks a hotkey capture inside the editor with real input (SendInput). It needs
// the editor to be the foreground window: the capture is gated on the foreground (contract §8).
func (s *SelfTest) captureTargeting(h uintptr) {
	win.SetForegroundWindow(win.HWND(h))
	if !waitFor(2*time.Second, func() bool { return win.GetForegroundWindow() == win.HWND(h) }) {
		s.estep("capture targeting (SendInput into the editor)", true,
			fmt.Sprintf("NOT RUN: Windows did not grant the editor the foreground (foreground=%#x); SendInput cannot target it", win.GetForegroundWindow()))
		return
	}
	_, err := s.driveEd(`(function(){var b=document.querySelector('button[aria-label="Toggle hotkey"]');if(!b)return false;b.click();return true})()`, 5*time.Second)
	if err != nil {
		s.estep("capture targeting: start a hotkey capture in the editor", false, err.Error())
		return
	}
	time.Sleep(400 * time.Millisecond)
	if err := SendHotkey(model.Hotkey{Modifiers: []string{"ctrl", "alt"}, Key: "f8"}); err != nil {
		s.estep("capture targeting: SendInput", false, err.Error())
		return
	}
	var text string
	ok := waitFor(4*time.Second, func() bool {
		raw, e := s.driveEd(`document.querySelector('button[aria-label="Toggle hotkey"]').textContent`, 3*time.Second)
		if e != nil || json.Unmarshal(raw, &text) != nil {
			return false
		}
		return strings.Contains(strings.ToLower(text), "f8")
	})
	s.estep("capture targeting: Ctrl+Alt+F8 via SendInput is captured by the editor's hotkey field", ok, fmt.Sprintf("field=%q", strings.TrimSpace(text)))
}

// shutdownWithRealEditor leaves the real page open, dirty and unresponsive, then the ordered
// shutdown runs; AfterRun verifies.
func (s *SelfTest) shutdownWithRealEditor() bool {
	if s.e1cScriptID == "" {
		return false
	}
	h, _, _, err := s.openRealEditor(s.e1cScriptID)
	if err == nil {
		if err = s.setEditorName("E1c Shutdown Draft"); err == nil {
			time.Sleep(2500 * time.Millisecond)
			err = s.muteEditor()
		}
	}
	if !s.estep("shutdown with the editor open: open, dirty, unresponsive", err == nil, fmt.Sprint(err)) {
		return false
	}
	s.shutdownHWND, s.shutdownName = h, "E1c Shutdown Draft"
	s.shutdownBefore = len(s.draftsNamed(s.shutdownName))
	s.shutdownStart = time.Now()
	return true
}

// prodEditorGate (production self-test) opens the real editor and checks with real input that the
// script hotkey is blocked while the emergency (global) and overlay hotkeys keep working.
func (s *SelfTest) prodEditorGate(h ProductionHooks, step func(string, bool, string) bool) {
	eh, _, _, err := s.openRealEditor(h.ProbeID)
	if !step("editor open over the production sequence (real page)", err == nil && s.EditOpen(), fmt.Sprintf("err=%v editDialogOpen=%v", err, s.EditOpen())) {
		return
	}
	send := func(hk model.Hotkey) error { return SendHotkey(hk) }
	if err := send(h.ProbeHotkey); err != nil {
		step("script hotkey while the editor is open", false, err.Error())
	} else {
		time.Sleep(1500 * time.Millisecond)
		step("script hotkey (SendInput) is blocked while the editor is open", h.RunningScriptID() == "", fmt.Sprintf("running=%q", h.RunningScriptID()))
	}
	was := h.GlobalActive()
	if err := send(h.GlobalHotkey); err == nil {
		step("emergency/global hotkey (SendInput) still works while the editor is open", waitFor(4*time.Second, func() bool { return h.GlobalActive() != was }), fmt.Sprintf("globalActive %v -> %v", was, h.GlobalActive()))
		_ = send(h.GlobalHotkey)
		waitFor(4*time.Second, func() bool { return h.GlobalActive() == was })
	}
	if ow := s.Overlay.Window(); ow != nil && h.OverlayHotkey.Key != "" {
		v := ow.IsVisible()
		if err := send(h.OverlayHotkey); err == nil {
			step("overlay hotkey (SendInput) still toggles the overlay while the editor is open", waitFor(4*time.Second, func() bool { return ow.IsVisible() != v }), fmt.Sprintf("visible %v -> %v", v, ow.IsVisible()))
			_ = send(h.OverlayHotkey)
			waitFor(4*time.Second, func() bool { return ow.IsVisible() == v })
		}
	}
	win.PostMessage(win.HWND(eh), win.WM_CLOSE, 0, 0)
	_, gone := s.waitEditorGone(eh, 15*time.Second)
	step("editor closed again over the production sequence", gone && waitFor(5*time.Second, func() bool { return !s.EditOpen() }), "")
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}
