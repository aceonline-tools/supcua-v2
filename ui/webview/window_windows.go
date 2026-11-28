package webview

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/jchv/go-webview2/pkg/edge"
	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/ui/webview/rpc"
	"github.com/yanmad27/supcua-v2/ui/webview/serve"
)

// Window is one native top-level window with its own WebView2 controller. The process host (UI)
// owns the hwnd→Window registry, the shared environment and the ONE shared rpc.Dispatcher; every
// Window uses the same window class and hostProc. Per-window state: navigation gate, lockdown,
// resource filter, bootstrap script, process-failure handler, generation and reply sink.
type Window struct {
	host  *UI
	role  rpc.Role
	gen   uint64
	owner *Window

	// UI thread only.
	hwnd    win.HWND
	browser *edge.Chromium
	nav     navGate

	lifecycle // gone: delivery invalidated; torn: native teardown claimed

	blockedNav atomic.Int64
	deniedWin  atomic.Int64
	respFrames atomic.Int64 // response frames posted to this window (self-test evidence)
	submitted  atomic.Int64 // page calls submitted by this window
	presented  atomic.Int32 // presentSurface runs (WM_SHOWWINDOW Reassert)

	fixtureHTML     string // self-test only: served at fixturePath instead of the app
	onProcessFailed func(kind int32)
	onCloseRequest  func()
	onDPI           func()         // WM_DPICHANGED on the editor
	onBounds        func()         // end of a move/resize of the editor
	onActivate      func(bool)     // editor (de)activated
	hw              atomic.Uintptr // hwnd mirror readable from any goroutine (capture hooks); 0 once destroyed
	preparing       bool           // PrepareEditorWindow is moving the hidden window: ignore DPI changes
	wantMax         bool           // show maximised on first show
	shownOnce       bool
	normalRect      win.RECT // staged+maximised: the intended restore rectangle (screen coordinates)
	hasNormal       bool
	staged          bool // shown but fully transparent until revealed (editor)
}

var (
	winMu       sync.RWMutex
	winRegistry = map[win.HWND]*Window{} // hwnd → Window; replaces the single `current` UI pointer
)

func windowOf(h win.HWND) *Window {
	winMu.RLock()
	defer winMu.RUnlock()
	return winRegistry[h]
}

func (w *Window) register() {
	w.hw.Store(uintptr(w.hwnd))
	winMu.Lock()
	winRegistry[w.hwnd] = w
	winMu.Unlock()
}

func (w *Window) unregister() {
	w.hw.Store(0)
	winMu.Lock()
	if winRegistry[w.hwnd] == w {
		delete(winRegistry, w.hwnd)
	}
	winMu.Unlock()
}

// Caller is the identity the transport attaches to every call from this window.
func (w *Window) caller() rpc.Caller {
	return rpc.Caller{Role: w.role, Gen: w.gen, HWND: uintptr(w.hwnd)}
}

// submit is the WebView2 message callback: it only enqueues on the shared dispatcher.
func (w *Window) submit(raw string) {
	w.submitted.Add(1)
	w.host.disp.Submit(raw, w.caller(), w.reply)
}

// reply is this window's request-local sink. It runs on the worker (or in Submit); delivery is
// re-checked on the UI thread against the registry so a closed or superseded window gets nothing.
func (w *Window) reply(resp []byte) {
	if w.gone.Load() {
		return
	}
	w.postFrame(rpc.EncodeResolve(resp), true)
}

// postFrame posts one frame to this window's page. Safe from any goroutine.
func (w *Window) postFrame(frame []byte, isResponse bool) {
	s := string(frame)
	w.host.Post(func() {
		if w.gone.Load() || w.browser == nil || windowOf(w.hwnd) != w {
			return
		}
		if err := w.browser.PostWebMessageAsJSON(s); err != nil {
			log.Printf("webview: post (%s): %v", w.role, err)
			return
		}
		if isResponse {
			w.respFrames.Add(1)
		}
	})
}

// callerCurrent reports whether c still identifies a live window: the main window always, the
// editor only while its generation is the current one. Safe from any goroutine (dispatcher).
func (u *UI) callerCurrent(c rpc.Caller) bool {
	switch c.Role {
	case rpc.RoleMain:
		return true
	case rpc.RoleEditor:
		g := u.editorGen.Load()
		return g != 0 && c.Gen == g
	}
	return false
}

func (w *Window) allowNavigation(uri string) bool {
	originOK := serve.AllowNavigation(uri)
	if w.role == rpc.RoleEditor { // the editor may only ever load its own page (contract §2.1)
		originOK = editorNavAllowed(uri, w.gen, w.fixtureHTML != "")
	}
	if w.nav.allow(originOK) {
		return true
	}
	w.blockedNav.Add(1)
	return false
}

// serveRequest answers <origin>/* from the embedded assets (UI thread, COM event).
func (w *Window) serveRequest(req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
	uri, method, err := req.RequestInfo()
	if err != nil {
		return
	}
	var r serve.Response
	if w.fixtureHTML != "" && uri == serve.Origin+fixturePath && method == "GET" {
		r = serve.Response{Status: 200, Reason: "OK", Header: "Content-Type: text/html; charset=utf-8\r\nCache-Control: no-store", Body: []byte(w.fixtureHTML)}
	} else {
		r = w.host.server.Handle(method, uri)
	}
	if w.browser == nil {
		return
	}
	if err := w.browser.Respond(args, r.Status, r.Reason, r.Header, r.Body); err != nil {
		log.Printf("webview: respond %q: %v", uri, err)
	}
}

// bootstrapJS tells the page which window it is (injected before page scripts).
func (w *Window) bootstrapJS() string {
	return fmt.Sprintf("window.__role=%q;window.__gen=%d;", w.role.String(), w.gen)
}

func (w *Window) closeBrowser() {
	if w.browser == nil {
		return
	}
	if err := w.browser.Close(); err != nil {
		log.Printf("webview: controller close (%s): %v", w.role, err)
	}
	w.browser = nil
}

// newBrowser builds the controller object with this window's own callbacks; the caller starts
// the embed (blocking for main, non-blocking from the shared environment for the editor).
func (w *Window) newBrowser(dataPath string) *edge.Chromium {
	u := w.host
	b := edge.NewChromium()
	b.DataPath = dataPath
	b.MessageCallback = w.submit // enqueue only
	b.NavigationStartingCallback = w.allowNavigation
	b.FatalCallback = func(err error) {
		if w.role == rpc.RoleMain {
			if u.cfg.OnFatal != nil {
				u.cfg.OnFatal(err) // first: arms the exit watchdog before any step that could hang
			}
			log.Printf("webview: security handler failed: %v", err)
			u.Quarantine() // synchronous: no further page input reaches the backend, the page is gone
			return
		}
		log.Printf("webview: editor security handler failed: %v", err)
		w.invalidate() // cut delivery now; the teardown below must still run (lifecycle)
		w.nav.quarantined = true
		u.Post(func() { u.destroyEditor(w) }) // never tear a controller down inside its own event; main is unaffected
	}
	b.NewWindowDeniedCallback = func(string) { w.deniedWin.Add(1) }
	b.WebResourceRequestedCallback = w.serveRequest
	b.SetGlobalPermission(edge.CoreWebView2PermissionStateDeny)
	return b
}

// finishBrowser applies the per-window protections after the controller exists. Fail closed: a
// window whose protections could not all be applied is never shown.
func (w *Window) finishBrowser(debug bool, bgR, bgG, bgB uint8) error {
	b := w.browser
	if err := b.Lockdown(debug); err != nil {
		return fmt.Errorf("webview lockdown: %w", err)
	}
	if err := b.SetDefaultBackground(bgR, bgG, bgB); err != nil {
		log.Printf("webview: default background (%s): %v", w.role, err)
	}
	if err := b.AddResourceFilter(serve.Filter); err != nil {
		return fmt.Errorf("webview resource filter: %w", err)
	}
	if err := b.AddDocumentCreatedScript(rpc.BootstrapJS); err != nil {
		return fmt.Errorf("webview bootstrap: %w", err)
	}
	if err := b.AddDocumentCreatedScript(w.bootstrapJS()); err != nil {
		return fmt.Errorf("webview role bootstrap: %w", err)
	}
	b.Resize()
	return nil
}

// presentSurface makes the controller present after the window is shown: the controller is
// created while the window is still hidden and must be told to present once it is visible,
// otherwise the client area keeps the WebView2 default (white) background. It logs the
// controller's own IsVisible/Bounds next to the client rect so a blank window is diagnosable
// from the log. UI thread only.
func (w *Window) presentSurface(reason string) {
	b := w.browser
	if b == nil {
		return
	}
	w.presented.Add(1)
	if err := b.Reassert(); err != nil {
		log.Printf("webview: surface %s (%s): reassert failed: %v", reason, w.role, err)
	}
	var cr win.RECT
	win.GetClientRect(w.hwnd, &cr)
	dpi, _, _ := procGetDpiForWindow.Call(uintptr(w.hwnd))
	st, err := b.Surface()
	if err != nil {
		log.Printf("webview: surface %s (%s): state unavailable: %v", reason, w.role, err)
		return
	}
	log.Printf("webview: surface %s (%s): client=%dx%d controller=%dx%d@(%d,%d) visible=%v hostVisible=%v dpi=%d",
		reason, w.role, cr.Right-cr.Left, cr.Bottom-cr.Top, st.Bounds.Right-st.Bounds.Left, st.Bounds.Bottom-st.Bounds.Top,
		st.Bounds.Left, st.Bounds.Top, st.Visible, win.IsWindowVisible(w.hwnd), dpi)
}

// Show restores (when iconic) and activates the window. UI thread only.
func (w *Window) Show() {
	if win.IsIconic(w.hwnd) {
		win.ShowWindow(w.hwnd, win.SW_RESTORE)
	}
	win.ShowWindow(w.hwnd, win.SW_SHOW)
	win.SetForegroundWindow(w.hwnd)
}

// Hide hides the window (tray state for main). UI thread only.
func (w *Window) Hide() { win.ShowWindow(w.hwnd, win.SW_HIDE) }

// Eval runs a script in this window's page (self-test driver only; production traffic uses frames).
func (w *Window) Eval(js string) {
	w.host.Post(func() {
		if !w.gone.Load() && w.browser != nil {
			w.browser.Eval(js)
		}
	})
}

// Visible reports whether the window is shown and not minimized. UI thread only.
func (w *Window) Visible() bool { return win.IsWindowVisible(w.hwnd) && !win.IsIconic(w.hwnd) }

// Show restores and activates the main window and, when an editor window exists, restores
// (iconic) or shows (hidden with its owner) and activates it too: the tray Show path. The editor,
// not the disabled main window, ends up focused. UI thread only.
func (u *UI) Show() {
	u.Window.Show()
	if ed := u.editor; ed != nil && ed.hwnd != 0 && ed.shownOnce {
		ed.Show()
	}
}

// SetMainEnabled enables or disables the main window (editor modality). UI thread only.
func (u *UI) SetMainEnabled(enabled bool) { win.EnableWindow(u.hwnd, enabled) }

// MainEnabled reports whether the main window accepts input. Safe from any goroutine.
func (u *UI) MainEnabled() bool { return win.IsWindowEnabled(u.hwnd) }
