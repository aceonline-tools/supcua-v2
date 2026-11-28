package webview

import (
	"errors"
	"fmt"
	"github.com/yanmad27/supcua-v2/otrace"
	"log"
	"sync"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/ui/webview/rpc"
	"github.com/yanmad27/supcua-v2/ui/webview/serve"
)

// EditorOptions configures CreateEditorWindow. Callbacks run on the UI thread and must not block.
type EditorOptions struct {
	Title          string
	StartupTimeout time.Duration // controller creation + navigation start; default 10 s
	Fixture        string        // self-test only: HTML served instead of the app
	OnReady        func(w *Window)
	OnFailed       func(w *Window, err error)
	OnProcessFail  func(w *Window, kind int32)
	OnCloseRequest func(w *Window)
	OnDPIChanged   func(w *Window)
	OnBoundsDone   func(w *Window)
	OnActivate     func(w *Window, active bool)
}

// Gen is the window generation (contract §4.6).
func (w *Window) Gen() uint64 { return w.gen }

// HWND is the native handle (0 once destroyed). Safe from any goroutine.
func (w *Window) HWND() uintptr { return w.hw.Load() }

// Editor returns the live editor window, or nil. Safe from any goroutine.
func (u *UI) Editor() *Window { return u.editorWin.Load() }

// CreateEditorWindow creates the second window hidden and owned by main, with a controller from
// the SAME WebView2 environment, and returns without pumping messages: the controller completes
// asynchronously and setup finishes on the UI thread (OnReady, or OnFailed on any error or on the
// startup timeout, after the window has been rolled back). UI thread only. One editor at a time.
func (u *UI) CreateEditorWindow(o EditorOptions) (*Window, error) {
	if u.closed.Load() || u.nav.quarantined || u.browser == nil {
		return nil, errors.New("webview: host closed")
	}
	if u.editor != nil {
		return nil, errors.New("webview: editor window already exists")
	}
	title := o.Title
	if title == "" {
		title = "Editor"
	}
	otrace.At("createwindow-begin")
	hwnd, err := createHostWindow(title, Bounds{}, false, 0, 0, u.cfg.IconIDs, u.hwnd)
	if err != nil {
		return nil, err
	}
	w := &Window{host: u, role: rpc.RoleEditor, gen: u.genSeq.Add(1), owner: u.Window, hwnd: hwnd,
		fixtureHTML: o.Fixture}
	w.onCloseRequest = func() {
		if o.OnCloseRequest != nil {
			o.OnCloseRequest(w)
		}
	}
	w.onProcessFailed = func(kind int32) {
		if o.OnProcessFail != nil {
			o.OnProcessFail(w, kind)
			return
		}
		log.Printf("webview: editor process failed (kind %d)", kind)
	}
	w.onDPI = func() {
		if o.OnDPIChanged != nil && !w.preparing {
			o.OnDPIChanged(w)
		}
	}
	w.onBounds = func() {
		if o.OnBoundsDone != nil {
			o.OnBoundsDone(w)
		}
	}
	w.onActivate = func(a bool) {
		if o.OnActivate != nil {
			o.OnActivate(w, a)
		}
	}
	w.register()
	u.editor = w
	u.editorWin.Store(w)
	u.editorGen.Store(w.gen)

	var once sync.Once
	// fail handles an asynchronous startup failure. With an owner (OnFailed) the window is only
	// invalidated here: the owner re-enables main and restores its gate, THEN destroys it
	// (DestroyEditorWindow), so the order is always invalidate → main enabled → destroy.
	fail := func(err error) {
		once.Do(func() {
			log.Printf("webview: editor window gen=%d startup failed: %v", w.gen, err)
			if o.OnFailed == nil {
				u.destroyEditor(w)
				return
			}
			w.invalidate()
			u.editorGen.CompareAndSwap(w.gen, 0)
			o.OnFailed(w, err)
		})
	}
	otrace.At("createwindow-done")
	b := w.newBrowser(u.dataPath)
	b.ProcessFailedCallback = func(kind int32) { w.onProcessFailed(kind) }
	b.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
		success, status := args.NavigationResult()
		if !success {
			log.Printf("webview: editor navigation failed (web error status %d)", status)
		}
		otrace.At("nav-completed")
		w.nav.completed()
	}
	w.browser = b
	timeout := o.StartupTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	timer := time.AfterFunc(timeout, func() {
		u.Post(func() { fail(fmt.Errorf("%w: editor controller not ready within %s", ErrStartupTimeout, timeout)) })
	})
	b.InitDoneCallback = func(err error) {
		u.Post(func() {
			timer.Stop()
			otrace.At("controller-ready")
			if w.gone.Load() || u.editor != w {
				return // rolled back or superseded while the controller was being created
			}
			if err != nil {
				fail(fmt.Errorf("editor controller: %w", err))
				return
			}
			bgR, bgG, bgB := uint8(0xf3), uint8(0xf6), uint8(0xfb)
			if u.cfg.DarkTitleBar {
				bgR, bgG, bgB = 0x10, 0x13, 0x18
			}
			if err := w.finishBrowser(u.cfg.Debug, bgR, bgG, bgB); err != nil {
				fail(err)
				return
			}
			url := editorURL(w.gen)
			if o.Fixture != "" {
				url = serve.Origin + fixturePath
			}
			otrace.At("navigate")
			if err := w.browser.NavigateChecked(url); err != nil {
				fail(fmt.Errorf("editor navigate: %w", err))
				return
			}
			if o.OnReady != nil {
				o.OnReady(w)
			}
		})
	}
	if err := b.EmbedFromEnvironment(u.browser.Environment(), uintptr(hwnd)); err != nil {
		// Synchronous failure: no callback. The window is invalidated and RETURNED with the error so
		// the caller rolls back in order (main enabled, gate restored) and then destroys it.
		timer.Stop()
		once.Do(func() {})
		w.invalidate()
		u.editorGen.CompareAndSwap(w.gen, 0)
		return w, err
	}
	if o.Fixture == "" {
		w.stageEditor()
	}
	return w, nil
}

var procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")

const wsExLayered = 0x00080000

// stageEditor makes the still-empty editor "shown" but fully transparent right after creation. A
// hidden WebView2 host does not render (requestAnimationFrame never fires), so the page could not
// paint before the window was revealed and its first visible frame was blank. Staged, the page
// loads, lays out and paints while nothing is visible; ShowEditorWindow then reveals it. UI thread.
func (w *Window) stageEditor() {
	ex := win.GetWindowLong(w.hwnd, win.GWL_EXSTYLE)
	// NOACTIVATE keeps every show (a remembered-maximised one included) from taking focus while transparent.
	win.SetWindowLong(w.hwnd, win.GWL_EXSTYLE, ex|wsExLayered|wsExNoActivate)
	if r, _, _ := procSetLayeredWindowAttributes.Call(uintptr(w.hwnd), 0, 0, 2); r == 0 {
		win.SetWindowLong(w.hwnd, win.GWL_EXSTYLE, ex)
		return
	}
	w.staged = true
	win.ShowWindow(w.hwnd, win.SW_SHOWNA)
}

const wsExNoActivate = 0x08000000

// revealStaged ends the staging: the layered and no-activate styles are removed (so no layered style
// lingers on the WebView2 host once the window is opaque) and the result is read back. It reports
// whether the window is really opaque and activatable now. UI thread.
func (w *Window) revealStaged() bool {
	procSetLayeredWindowAttributes.Call(uintptr(w.hwnd), 0, 255, 2)
	ex := win.GetWindowLong(w.hwnd, win.GWL_EXSTYLE)
	win.SetWindowLong(w.hwnd, win.GWL_EXSTYLE, ex&^(wsExLayered|wsExNoActivate))
	return win.GetWindowLong(w.hwnd, win.GWL_EXSTYLE)&wsExLayered == 0
}

// EditorStaged reports whether the editor is shown-but-transparent and waits for ShowEditorWindow.
func (u *UI) EditorStaged(gen uint64) bool {
	w := u.editor
	return w != nil && w.gen == gen && w.hwnd != 0 && w.staged
}

// destroyEditor closes the editor window: late replies are suppressed first (gone), the
// generation is invalidated, then the controller and the HWND are released. Idempotent; UI thread.
// The dispatcher is never touched.
func (u *UI) destroyEditor(w *Window) {
	if w == nil || w.role != rpc.RoleEditor {
		return
	}
	first := w.beginTeardown()
	if u.editor == w {
		u.editor = nil
		u.editorWin.Store(nil)
	}
	u.editorGen.CompareAndSwap(w.gen, 0)
	if !first {
		return
	}
	u.retiredMu.Lock()
	u.retired.BlockedNavigations += w.blockedNav.Load()
	u.retired.DeniedPopups += w.deniedWin.Load()
	u.retiredMu.Unlock()
	if b := w.browser; b != nil {
		// Close() is POSTED, never called here: it is a COM call that can block, and destroy must
		// never wait on it. If it hangs, a dangling renderer lives until app exit (contract §4.5).
		b.Abandon()
		w.browser = nil
		u.Post(func() {
			if err := b.Close(); err != nil {
				log.Printf("webview: editor controller close: %v", err)
			}
		})
	}
	w.unregister()
	if w.hwnd != 0 {
		win.DestroyWindow(w.hwnd)
		w.hwnd = 0
	}
}

// DestroyEditorWindow is destroyEditor for callers that hold the generation. UI thread only.
func (u *UI) DestroyEditorWindow(gen uint64) {
	if w := u.editor; w != nil && w.gen == gen {
		u.destroyEditor(w)
	}
}

// MonitorFacts are the target monitor's facts at its real DPI (editor-window contract §5.3).
type MonitorFacts struct {
	DPI                        int
	FrameW, FrameH             int
	WorkL, WorkT, WorkR, WorkB int
	OwnerCX, OwnerCY           int
}

var procAdjustWindowRectExForDpi = user32.NewProc("AdjustWindowRectExForDpi")

// PrepareEditorWindow positions the HIDDEN editor on the target monitor (the remembered rectangle's
// monitor when given, else the owner's), lets its DPI settle and returns that monitor's facts:
// DPI first, then frame and client maths (contract §5.3). UI thread only.
func (u *UI) PrepareEditorWindow(gen uint64, remembered *[4]int) (MonitorFacts, error) {
	w := u.editor
	if w == nil || w.gen != gen || w.hwnd == 0 {
		return MonitorFacts{}, errors.New("webview: no such editor window")
	}
	var mon win.HMONITOR
	if remembered != nil {
		r := win.RECT{Left: int32(remembered[0]), Top: int32(remembered[1]), Right: int32(remembered[2]), Bottom: int32(remembered[3])}
		h, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), 0)
		mon = win.HMONITOR(h)
	}
	if mon == 0 {
		mon = win.MonitorFromWindow(u.hwnd, win.MONITOR_DEFAULTTONEAREST)
	}
	mi := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if mon == 0 || !win.GetMonitorInfo(mon, &mi) {
		return MonitorFacts{}, errors.New("webview: no monitor")
	}
	f := MonitorFacts{WorkL: int(mi.RcWork.Left), WorkT: int(mi.RcWork.Top), WorkR: int(mi.RcWork.Right), WorkB: int(mi.RcWork.Bottom)}
	const probeW, probeH = 400, 300
	w.preparing = true
	win.SetWindowPos(w.hwnd, 0, int32(f.WorkL+(f.WorkR-f.WorkL-probeW)/2), int32(f.WorkT+(f.WorkB-f.WorkT-probeH)/2), probeW, probeH,
		win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	w.preparing = false
	f.DPI = windowDPI(w.hwnd)
	var fr win.RECT
	procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&fr)), uintptr(win.WS_OVERLAPPEDWINDOW), 0, 0, uintptr(f.DPI))
	f.FrameW, f.FrameH = int(fr.Right-fr.Left), int(fr.Bottom-fr.Top)
	var or win.RECT
	if win.GetWindowRect(u.hwnd, &or) && win.IsWindowVisible(u.hwnd) && !win.IsIconic(u.hwnd) {
		f.OwnerCX, f.OwnerCY = int(or.Left+or.Right)/2, int(or.Top+or.Bottom)/2
	} else {
		f.OwnerCX, f.OwnerCY = (f.WorkL+f.WorkR)/2, (f.WorkT+f.WorkB)/2
	}
	return f, nil
}

// PlaceEditorWindow sets the outer rectangle of the hidden editor. UI thread only.
func (u *UI) PlaceEditorWindow(gen uint64, l, t, r, b int, maximized bool) {
	if w := u.editor; w != nil && w.gen == gen && w.hwnd != 0 {
		win.SetWindowPos(w.hwnd, 0, int32(l), int32(t), int32(r-l), int32(b-t), win.SWP_NOZORDER|win.SWP_NOACTIVATE)
		w.wantMax = maximized
		if w.staged && maximized && !w.shownOnce {
			// Every way of maximising a window activates it. While transparent the window is instead
			// laid out at exactly the rectangle Windows gives a maximised window on this monitor, and
			// the real maximise runs at the reveal (same geometry, so no visible resize).
			var mmi win.MINMAXINFO
			win.SendMessage(w.hwnd, win.WM_GETMINMAXINFO, 0, uintptr(unsafe.Pointer(&mmi)))
			if mmi.PtMaxSize.X > 0 && mmi.PtMaxSize.Y > 0 {
				win.SetWindowPos(w.hwnd, 0, mmi.PtMaxPosition.X, mmi.PtMaxPosition.Y, mmi.PtMaxSize.X, mmi.PtMaxSize.Y, win.SWP_NOZORDER|win.SWP_NOACTIVATE)
			}
		}
	}
}

// ShowEditorWindow restores (iconic), shows (hidden) and activates the editor. UI thread only.
// It reports whether the window is visible afterwards; a staged window that could not be made
// opaque stays staged and reports false, so the owner rolls the editor back instead of leaving an
// invisible modal window.
func (u *UI) ShowEditorWindow(gen uint64) bool {
	w := u.editor
	if w == nil || w.gen != gen || w.hwnd == 0 {
		return false
	}
	wasStaged := w.staged
	if w.staged {
		if !w.revealStaged() {
			return false
		}
		w.staged = false
	}
	switch {
	case win.IsIconic(w.hwnd):
		win.ShowWindow(w.hwnd, win.SW_RESTORE)
	case wasStaged && w.wantMax && !w.shownOnce:
		win.ShowWindow(w.hwnd, win.SW_SHOWMAXIMIZED)
	case !win.IsWindowVisible(w.hwnd) && w.wantMax && !w.shownOnce:
		win.ShowWindow(w.hwnd, win.SW_SHOWMAXIMIZED)
	case !win.IsWindowVisible(w.hwnd):
		win.ShowWindow(w.hwnd, win.SW_SHOW)
	}
	w.shownOnce = true
	win.SetForegroundWindow(w.hwnd)
	return win.IsWindowVisible(w.hwnd)
}

// EditorWindowBounds is the restored (normal) rectangle and the maximised flag. UI thread only.
func (u *UI) EditorWindowBounds(gen uint64) (l, t, r, b int, maximized, ok bool) {
	w := u.editor
	if w == nil || w.gen != gen || w.hwnd == 0 {
		return
	}
	var wp win.WINDOWPLACEMENT
	wp.Length = uint32(unsafe.Sizeof(wp))
	if !win.GetWindowPlacement(w.hwnd, &wp) {
		return
	}
	n := wp.RcNormalPosition
	return int(n.Left), int(n.Top), int(n.Right), int(n.Bottom), wp.ShowCmd == win.SW_SHOWMAXIMIZED, true
}

// EmitEditor pushes an event to the editor window of generation gen (dropped when it is gone).
func (u *UI) EmitEditor(gen uint64, event string, payload any) {
	ed := u.editorWin.Load()
	if ed == nil || ed.gen != gen {
		return
	}
	b, err := rpc.EncodeEvent(event, payload)
	if err != nil {
		log.Printf("webview: emit %q: %v", event, err)
		return
	}
	ed.postFrame(b, false)
}

// RecoverEditorOnScreen moves the editor to the main window's monitor when no monitor shows enough
// of it any more (WM_DISPLAYCHANGE, DPI change). Reports whether it moved. UI thread only.
func (u *UI) RecoverEditorOnScreen() bool {
	w := u.editor
	if w == nil || w.hwnd == 0 || !win.IsWindowVisible(w.hwnd) || win.IsIconic(w.hwnd) {
		return false
	}
	var r win.RECT
	if !win.GetWindowRect(w.hwnd, &r) {
		return false
	}
	nearest := scRect{}
	if h, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), 2); h != 0 { // MONITOR_DEFAULTTONEAREST
		mi := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
		if win.GetMonitorInfo(win.HMONITOR(h), &mi) {
			nearest = scRect{int(mi.RcWork.Left), int(mi.RcWork.Top), int(mi.RcWork.Right), int(mi.RcWork.Bottom)}
		}
	}
	tw, ok := workArea(u.hwnd)
	if !ok {
		return false
	}
	out, moved := recoverRect(scRect{int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)}, nearest, scRect{tw.L, tw.T, tw.R, tw.B})
	if moved {
		win.SetWindowPos(w.hwnd, 0, int32(out.L), int32(out.T), int32(out.w()), int32(out.h()), win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	}
	return moved
}
