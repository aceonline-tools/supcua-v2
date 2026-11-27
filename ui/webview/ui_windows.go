package webview

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/ui/webview/rpc"
	"github.com/yanmad27/supcua-v2/ui/webview/serve"
)

// The UI thread is the main goroutine's OS thread (edge's init already locks it and enters
// an STA). Lock again so this package is correct even if edge changes.
func init() { runtime.LockOSThread() }

// ErrStartupTimeout: the WebView2 runtime is installed but never produced a controller.
var ErrStartupTimeout = edge.ErrEmbedTimeout

// Bounds is a window rectangle in screen pixels (restored/normal position, not minimized).
type Bounds struct{ X, Y, W, H int }

// Config describes the host. Methods is the backend's handler table (bridge.Methods());
// every callback runs on the UI thread and must not block or open a modal loop.
type Config struct {
	Title          string
	Methods        map[string]rpc.Handler
	Assets         fs.FS // frontend root served at https://app.local/
	DataDir        string
	Debug          bool // DevTools, context menu and browser accelerators on
	DarkTitleBar   bool
	Initial        Bounds // zero value = default size/position
	ResetSize      bool   // keep Initial's position but start at the DPI-scaled default size
	MinW, MinH     int
	StartHidden    bool
	IconIDs        []int // rsrc icon resource ids to try (default 1,2,3,7 as ui/tray.go)
	StartupTimeout time.Duration
	QueueSize      int

	OnClose      func()            // window X; default: Quit
	OnBounds     func(Bounds)      // after a move/resize ended and before close
	OnActivate   func(active bool) // host window (de)activated
	OnPageLoaded func()
	// OnNavigationCompleted runs on the UI thread for every NavigationCompleted, with its IsSuccess.
	OnNavigationCompleted func(success bool)
	// OnProcessFailed runs on the UI thread (inside a WebView2 event: post work, never block or
	// open a modal loop) when a browser/renderer process fails; kind is COREWEBVIEW2_PROCESS_FAILED_KIND.
	// The callee recovers with EnqueueRPCJob (reset) followed by a posted Reload.
	OnProcessFailed func(kind int32)
	// OnFatal runs on the UI thread when a security handler failed (navigation not cancelled,
	// popup not denied), BEFORE the host quarantines itself (see Quarantine): the callee must arm
	// its hard exit watchdog synchronously, then post the ordered exit; never block.
	OnFatal func(err error)
}

// UI is the single WebView2 host of the process. All methods are UI-thread only unless noted.
type UI struct {
	cfg      Config
	mw       *walk.MainWindow
	hwnd     win.HWND
	browser  *edge.Chromium
	disp     *rpc.Dispatcher
	server   *serve.Server
	threadID uint32

	closed     atomic.Bool
	nav        navGate // UI thread only
	loadedOnce sync.Once
	loaded     chan struct{}
	blockedNav atomic.Int64
	deniedWin  atomic.Int64
}

var current atomic.Pointer[UI]

// New builds the hidden walk pump owner, the host window and the WebView2 controller, and
// navigates to https://app.local/index.html. Call on the main goroutine before Run.
func New(cfg Config) (*UI, error) {
	if cfg.Assets == nil || cfg.Methods == nil {
		return nil, errors.New("webview: Assets and Methods are required")
	}
	u := &UI{cfg: cfg, loaded: make(chan struct{}), server: &serve.Server{FS: cfg.Assets, Logf: log.Printf}, threadID: win.GetCurrentThreadId()}
	if !current.CompareAndSwap(nil, u) {
		return nil, errors.New("webview: only one UI per process")
	}
	ok := false
	defer func() {
		if !ok {
			u.teardown()
			if u.mw != nil {
				u.mw.Dispose()
			}
			current.Store(nil)
		}
	}()

	var err error
	if u.mw, err = walk.NewMainWindow(); err != nil { // hidden: message owner for tray + Synchronize
		return nil, fmt.Errorf("webview: pump window: %w", err)
	}
	if u.hwnd, err = createHostWindow(cfg.Title, cfg.Initial, cfg.ResetSize, cfg.MinW, cfg.MinH, cfg.IconIDs); err != nil {
		return nil, err
	}
	if cfg.DarkTitleBar {
		_ = ApplyDarkTitleBar(uintptr(u.hwnd), true)
	}

	u.disp = rpc.New(cfg.Methods, cfg.QueueSize)
	b := edge.NewChromium()
	b.DataPath = cfg.DataDir
	if b.DataPath == "" {
		b.DataPath = filepath.Join(os.Getenv("LOCALAPPDATA"), "supcua-v2", "webview2")
	}
	b.MessageCallback = u.disp.Submit // enqueue only
	b.NavigationStartingCallback = u.allowNavigation
	b.FatalCallback = func(err error) {
		if cfg.OnFatal != nil {
			cfg.OnFatal(err) // first: arms the exit watchdog before any step that could hang
		}
		log.Printf("webview: security handler failed: %v", err)
		u.Quarantine() // synchronous: no further page input reaches the backend, the page is gone
	}
	b.NewWindowDeniedCallback = func(string) { u.deniedWin.Add(1) }
	b.ProcessFailedCallback = func(kind int32) {
		if cfg.OnProcessFailed != nil {
			cfg.OnProcessFailed(kind)
			return
		}
		// Never ignore a process failure silently: without a handler a dead or hung browser or
		// renderer (kinds 0-2) leaves an unusable window, so take the fatal path. Auxiliary
		// processes recover on their own and are only logged.
		if kind >= 0 && kind <= 2 {
			b.FatalCallback(fmt.Errorf("process failed (kind %d) with no handler", kind))
			return
		}
		log.Printf("webview: auxiliary process failed (kind %d)", kind)
	}
	b.WebResourceRequestedCallback = u.serveRequest
	b.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
		success, status := args.NavigationResult()
		if !success {
			log.Printf("webview: navigation failed (web error status %d)", status)
		}
		u.nav.completed()
		if cfg.OnNavigationCompleted != nil {
			cfg.OnNavigationCompleted(success)
		}
		u.loadedOnce.Do(func() {
			close(u.loaded)
			if cfg.OnPageLoaded != nil {
				cfg.OnPageLoaded()
			}
		})
	}
	b.SetGlobalPermission(edge.CoreWebView2PermissionStateDeny)
	u.browser = b

	timeout := cfg.StartupTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if err = b.EmbedTimeout(uintptr(u.hwnd), timeout); err != nil {
		u.browser = nil // never completed: nothing to Close
		return nil, fmt.Errorf("webview startup: %w", err)
	}
	// Fail closed: a host whose protections could not all be applied is never shown.
	if err = b.Lockdown(cfg.Debug); err != nil {
		return nil, fmt.Errorf("webview lockdown: %w", err)
	}
	bgR, bgG, bgB := uint8(0xf3), uint8(0xf6), uint8(0xfb)
	if cfg.DarkTitleBar {
		bgR, bgG, bgB = 0x10, 0x13, 0x18
	}
	if err := b.SetDefaultBackground(bgR, bgG, bgB); err != nil {
		log.Printf("webview: default background: %v", err)
	}
	if err = b.AddResourceFilter(serve.Filter); err != nil {
		return nil, fmt.Errorf("webview resource filter: %w", err)
	}
	if err = b.AddDocumentCreatedScript(rpc.BootstrapJS); err != nil {
		return nil, fmt.Errorf("webview bootstrap: %w", err)
	}
	b.Resize()

	u.disp.Start(func(resp []byte) { u.postFrame(rpc.EncodeResolve(resp)) })
	if err = b.NavigateChecked(serve.Origin + "/" + serve.Index); err != nil {
		return nil, fmt.Errorf("webview navigate: %w", err)
	}
	if !cfg.StartHidden {
		u.Show()
	}
	ok = true
	return u, nil
}

// serveRequest answers https://app.local/* from the embedded assets (UI thread, COM event).
func (u *UI) serveRequest(req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
	uri, method, err := req.RequestInfo()
	if err != nil {
		return
	}
	r := u.server.Handle(method, uri)
	if u.browser == nil {
		return
	}
	if err := u.browser.Respond(args, r.Status, r.Reason, r.Header, r.Body); err != nil {
		log.Printf("webview: respond %q: %v", uri, err)
	}
}

func (u *UI) allowNavigation(uri string) bool {
	if u.nav.allow(serve.AllowNavigation(uri)) {
		return true
	}
	u.blockedNav.Add(1)
	return false
}

// EnqueueRPCJob runs job on the RPC worker strictly after every page call already queued or in
// flight and before every call submitted later (the renderer-crash reset). Reports false once
// the dispatcher is quarantined/closed. Safe from any goroutine.
func (u *UI) EnqueueRPCJob(job func()) bool {
	return u.disp != nil && u.disp.Enqueue(job)
}

// Quarantine cuts the page off for good: the dispatcher is closed terminally (later calls are
// rejected, queued calls and jobs discarded, the worker exits after the call in flight), the
// window is hidden and the WebView2 controller closed. A handler already running finishes on
// its own; the caller's exit watchdog bounds it. UI thread only; idempotent.
func (u *UI) Quarantine() {
	if u.nav.quarantined {
		return
	}
	u.nav.quarantined = true
	if u.disp != nil {
		u.disp.Close()
	}
	if u.hwnd != 0 {
		win.ShowWindow(u.hwnd, win.SW_HIDE)
	}
	u.closeBrowser()
}

func (u *UI) closeBrowser() {
	if u.browser == nil {
		return
	}
	if err := u.browser.Close(); err != nil {
		log.Printf("webview: controller close: %v", err)
	}
	u.browser = nil
}

// Reload reloads the app page: the only post-load navigation the host permits. UI thread only;
// call it after the reset job has completed.
func (u *UI) Reload() error {
	if u.browser == nil || u.nav.quarantined {
		return errors.New("webview closed")
	}
	u.nav.permit = true
	err := u.browser.Reload()
	if err != nil {
		u.nav.permit = false
	}
	return err
}

// Run executes the walk message loop until Quit. Returns walk's exit code.
func (u *UI) Run() int { return u.mw.Run() }

// MainWindow is the hidden walk window, the owner for walk.NewNotifyIcon (tray).
func (u *UI) MainWindow() *walk.MainWindow { return u.mw }

// HWND is the host window handle.
func (u *UI) HWND() uintptr { return uintptr(u.hwnd) }

// ThreadID is the UI thread id.
func (u *UI) ThreadID() uint32 { return u.threadID }

// Loaded is closed after the first completed navigation.
func (u *UI) Loaded() <-chan struct{} { return u.loaded }

// Post runs f on the UI thread. Safe from any goroutine; dropped after Quit.
func (u *UI) Post(f func()) {
	if u.closed.Load() {
		return
	}
	u.mw.Synchronize(f)
}

// Emit pushes an event to the page. Safe from any goroutine; non-blocking.
func (u *UI) Emit(event string, payload any) {
	b, err := rpc.EncodeEvent(event, payload)
	if err != nil {
		log.Printf("webview: emit %q: %v", event, err)
		return
	}
	u.postFrame(b)
}

func (u *UI) postFrame(frame []byte) {
	s := string(frame)
	u.Post(func() {
		if u.browser == nil {
			return
		}
		if err := u.browser.PostWebMessageAsJSON(s); err != nil {
			log.Printf("webview: post: %v", err)
		}
	})
}

// Eval runs a script in the page (self-test driver only; production traffic uses Emit).
func (u *UI) Eval(js string) {
	u.Post(func() {
		if u.browser != nil {
			u.browser.Eval(js)
		}
	})
}

// Show restores and activates the host window.
func (u *UI) Show() {
	if win.IsIconic(u.hwnd) {
		win.ShowWindow(u.hwnd, win.SW_RESTORE)
	}
	win.ShowWindow(u.hwnd, win.SW_SHOW)
	win.SetForegroundWindow(u.hwnd)
}

// Hide hides the host window (tray state).
func (u *UI) Hide() { win.ShowWindow(u.hwnd, win.SW_HIDE) }

// Visible reports whether the host window is shown and not minimized.
func (u *UI) Visible() bool { return win.IsWindowVisible(u.hwnd) && !win.IsIconic(u.hwnd) }

// SetDarkTitleBar follows the app theme; safe from any goroutine.
func (u *UI) SetDarkTitleBar(dark bool) {
	u.Post(func() { _ = ApplyDarkTitleBar(uintptr(u.hwnd), dark) })
}

// Bounds returns the restored (normal) rectangle of the host window.
func (u *UI) Bounds() Bounds {
	var wp win.WINDOWPLACEMENT
	wp.Length = uint32(unsafe.Sizeof(wp))
	if !win.GetWindowPlacement(u.hwnd, &wp) {
		return Bounds{}
	}
	r := wp.RcNormalPosition
	return Bounds{int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)}
}

func (u *UI) reportBounds() {
	if u.cfg.OnBounds == nil {
		return
	}
	b := u.Bounds()
	if b.W > 0 && b.H > 0 && b.X > -1000 && b.Y > -1000 { // -32000 = minimized sentinel
		u.cfg.OnBounds(b)
	}
}

// Quit tears the UI down in order and ends Run. UI thread only; use RequestQuit elsewhere.
func (u *UI) Quit() {
	if !u.closed.CompareAndSwap(false, true) {
		return
	}
	u.reportBounds()
	u.teardown()
	u.mw.Close()
}

// RequestQuit asks the UI thread to Quit; safe from any goroutine.
func (u *UI) RequestQuit() {
	if u.closed.Load() {
		return
	}
	u.mw.Synchronize(u.Quit)
}

func (u *UI) teardown() {
	u.closed.Store(true)
	if u.disp != nil {
		u.disp.Close() // non-blocking: a handler may be waiting for this very thread
	}
	u.closeBrowser()
	if u.hwnd != 0 {
		win.DestroyWindow(u.hwnd)
		u.hwnd = 0
	}
}

// Stats are counters for self-tests.
type Stats struct{ BlockedNavigations, DeniedPopups int64 }

func (u *UI) Stats() Stats { return Stats{u.blockedNav.Load(), u.deniedWin.Load()} }

// ProbeNavigationLock navigates to a foreign origin and reports whether the lock cancelled
// it within timeout. Self-test only.
func (u *UI) ProbeNavigationLock(timeout time.Duration) bool {
	before := u.blockedNav.Load()
	u.Post(func() {
		if u.browser != nil {
			u.browser.Navigate("https://example.invalid/")
		}
	})
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if u.blockedNav.Load() > before {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// ---- host window ----

var (
	hostClassOnce sync.Once
	hostClassErr  error
	hostClassName *uint16
)

const (
	wmDpiChanged   = 0x02E0
	wmExitSizeMove = 0x0232
)

func hostProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	u := current.Load()
	if u != nil && !u.closed.Load() && u.browser != nil {
		switch msg {
		case win.WM_SIZE:
			u.browser.Resize()
		case win.WM_SHOWWINDOW:
			if wp != 0 {
				u.presentSurface("shown")
			}
		case win.WM_MOVE, win.WM_MOVING:
			_ = u.browser.NotifyParentWindowPositionChanged()
		case win.WM_SETFOCUS:
			u.browser.Focus()
		case win.WM_ACTIVATE:
			if u.cfg.OnActivate != nil {
				u.cfg.OnActivate(uint16(wp&0xFFFF) != win.WA_INACTIVE)
			}
		case wmExitSizeMove:
			u.reportBounds()
		case wmDpiChanged:
			r := (*win.RECT)(ptrFrom(lp))
			win.SetWindowPos(hwnd, 0, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top, win.SWP_NOZORDER|win.SWP_NOACTIVATE)
			return 0
		case win.WM_GETMINMAXINFO:
			mm := (*win.MINMAXINFO)(ptrFrom(lp))
			aw, ah := 0, 0
			if wk, ok := workArea(hwnd); ok {
				aw, ah = wk.R-wk.L, wk.B-wk.T
			}
			mw, mh := scaledMin(windowDPI(hwnd), u.cfg.MinW, u.cfg.MinH, aw, ah)
			if u.cfg.MinW > 0 {
				mm.PtMinTrackSize.X = int32(mw)
			}
			if u.cfg.MinH > 0 {
				mm.PtMinTrackSize.Y = int32(mh)
			}
			return 0
		case win.WM_SYSCOMMAND:
			if wp&0xFFF0 == win.SC_MINIMIZE { // minimize hides to tray (ui/main_window.go:299)
				win.ShowWindow(hwnd, win.SW_HIDE)
				return 0
			}
		case win.WM_CLOSE: // X exits for real (ui/main_window.go:308)
			if u.cfg.OnClose != nil {
				u.cfg.OnClose()
			} else {
				u.Quit()
			}
			return 0
		}
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

// createHostWindow registers the class once and creates a native-frame top-level window.
func createHostWindow(title string, b Bounds, resetSize bool, minW, minH int, iconIDs []int) (win.HWND, error) {
	inst := win.GetModuleHandle(nil)
	hostClassOnce.Do(func() {
		hostClassName, _ = syscall.UTF16PtrFromString("supcua_webview_host")
		wc := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			LpfnWndProc:   newCallback(hostProc),
			HInstance:     inst,
			LpszClassName: hostClassName,
			HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)),
		}
		if len(iconIDs) == 0 {
			iconIDs = []int{1, 2, 3, 7}
		}
		for _, id := range iconIDs {
			if ic := win.LoadIcon(inst, win.MAKEINTRESOURCE(uintptr(id))); ic != 0 {
				wc.HIcon, wc.HIconSm = ic, ic
				break
			}
		}
		if win.RegisterClassEx(&wc) == 0 {
			hostClassErr = errors.New("webview: register host window class failed")
		}
	})
	if hostClassErr != nil {
		return 0, hostClassErr
	}
	x, y, w, h := int32(win.CW_USEDEFAULT), int32(win.CW_USEDEFAULT), int32(defaultW), int32(defaultH)
	useDefault := useDefaultSize(b.W, b.H, resetSize)
	if b.W > 0 && b.H > 0 {
		if !useDefault {
			w, h = int32(b.W), int32(b.H)
		}
		r := win.RECT{Left: int32(b.X), Top: int32(b.Y), Right: int32(b.X + b.W), Bottom: int32(b.Y + b.H)}
		if b.X > -1000 && b.Y > -1000 && onSomeMonitor(&r) {
			x, y = int32(b.X), int32(b.Y)
		}
	}
	t, _ := syscall.UTF16PtrFromString(title)
	hwnd := win.CreateWindowEx(0, hostClassName, t, win.WS_OVERLAPPEDWINDOW, x, y, w, h, 0, 0, inst, nil)
	if hwnd == 0 {
		return 0, errors.New("webview: create host window failed")
	}
	if useDefault {
		fitDefaultSize(hwnd, minW, minH)
	}
	return hwnd, nil
}

// windowDPI is the window's DPI (96 when unavailable).
func windowDPI(hwnd win.HWND) int {
	d, _, _ := procGetDpiForWindow.Call(uintptr(hwnd))
	if d == 0 {
		return baseDPI
	}
	return int(d)
}

// fitDefaultSize resizes a default-sized window to the DPI of the monitor it was created on:
// WebView2 lays out in CSS px = physical px * 96/DPI, so a fixed physical size leaves a UI
// designed for ~1000x700 CSS px too small at 125-200% scaling.
func fitDefaultSize(hwnd win.HWND, minW, minH int) {
	dpi := windowDPI(hwnd)
	var r win.RECT
	win.GetWindowRect(hwnd, &r)
	work, _ := workArea(hwnd)
	x, y, w, h := fitRect(dpi, work, int(r.Left), int(r.Top), minW, minH)
	if int(r.Left) == x && int(r.Top) == y && int(r.Right-r.Left) == w && int(r.Bottom-r.Top) == h {
		return
	}
	win.SetWindowPos(hwnd, 0, int32(x), int32(y), int32(w), int32(h), win.SWP_NOZORDER|win.SWP_NOACTIVATE)
}

// workArea is the work area of the monitor nearest to the window (zero rect when unavailable).
func workArea(hwnd win.HWND) (rect, bool) {
	m := win.MonitorFromWindow(hwnd, win.MONITOR_DEFAULTTONEAREST)
	if m == 0 {
		return rect{}, false
	}
	mi := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if !win.GetMonitorInfo(m, &mi) {
		return rect{}, false
	}
	return rect{int(mi.RcWork.Left), int(mi.RcWork.Top), int(mi.RcWork.Right), int(mi.RcWork.Bottom)}, true
}

var procMonitorFromRect = user32.NewProc("MonitorFromRect")

// onSomeMonitor reports whether r intersects any attached monitor (saved positions can
// point at a monitor that is gone).
func onSomeMonitor(r *win.RECT) bool {
	h, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(r)), 0) // MONITOR_DEFAULTTONULL
	return h != 0
}

var procGetDpiForWindow = user32.NewProc("GetDpiForWindow")

// presentSurface makes the controller present after the host window is shown: the controller
// is created while the host is still hidden and must be told to present once it is visible,
// otherwise the client area keeps the WebView2 default (white) background. It logs the
// controller's own IsVisible/Bounds next to the host client rect so a blank window is
// diagnosable from the log. UI thread only.
func (u *UI) presentSurface(reason string) {
	b := u.browser
	if b == nil {
		return
	}
	if err := b.Reassert(); err != nil {
		log.Printf("webview: surface %s: reassert failed: %v", reason, err)
	}
	var cr win.RECT
	win.GetClientRect(u.hwnd, &cr)
	dpi, _, _ := procGetDpiForWindow.Call(uintptr(u.hwnd))
	st, err := b.Surface()
	if err != nil {
		log.Printf("webview: surface %s: state unavailable: %v", reason, err)
		return
	}
	log.Printf("webview: surface %s: client=%dx%d controller=%dx%d@(%d,%d) visible=%v hostVisible=%v dpi=%d",
		reason, cr.Right-cr.Left, cr.Bottom-cr.Top, st.Bounds.Right-st.Bounds.Left, st.Bounds.Bottom-st.Bounds.Top,
		st.Bounds.Left, st.Bounds.Top, st.Visible, win.IsWindowVisible(u.hwnd), dpi)
}
