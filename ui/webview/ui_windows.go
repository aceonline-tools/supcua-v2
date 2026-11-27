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
	CtxMethods     map[string]rpc.HandlerCtx // handlers that need the caller identity (rpc.CallerFrom)
	Roles          *rpc.Matrix               // role matrix; nil = no policy
	Assets         fs.FS                     // frontend root served at https://app.invalid/
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
//
// UI is the process host: it owns the hwnd→Window registry, the shared WebView2 environment, the
// ONE serial rpc.Dispatcher and the optional editor window. The embedded *Window is the main
// window, so its hwnd/browser/nav stay reachable as u.hwnd, u.browser, u.nav.
type UI struct {
	*Window
	cfg      Config
	mw       *walk.MainWindow
	disp     *rpc.Dispatcher
	server   *serve.Server
	threadID uint32
	dataPath string

	closed     atomic.Bool
	loadedOnce sync.Once
	loaded     chan struct{}

	editor    *Window // UI thread only; nil when no editor window exists
	editorWin atomic.Pointer[Window]
	editorGen atomic.Uint64 // generation of the live editor, 0 when none (dispatcher validity check)
	genSeq    atomic.Uint64
	retired   Stats // counters of destroyed editor windows
	retiredMu sync.Mutex
}

var hostActive atomic.Bool // one host per process

// New builds the hidden walk pump owner, the host window and the WebView2 controller, and
// navigates to https://app.invalid/index.html. Call on the main goroutine before Run.
func New(cfg Config) (*UI, error) {
	if cfg.Assets == nil || cfg.Methods == nil {
		return nil, errors.New("webview: Assets and Methods are required")
	}
	u := &UI{cfg: cfg, loaded: make(chan struct{}), server: &serve.Server{FS: cfg.Assets, Logf: log.Printf}, threadID: win.GetCurrentThreadId()}
	u.Window = &Window{host: u, role: rpc.RoleMain, gen: u.genSeq.Add(1)}
	if !hostActive.CompareAndSwap(false, true) {
		return nil, errors.New("webview: only one UI per process")
	}
	ok := false
	defer func() {
		if !ok {
			u.teardown()
			if u.mw != nil {
				u.mw.Dispose()
			}
			hostActive.Store(false)
		}
	}()

	var err error
	if u.mw, err = walk.NewMainWindow(); err != nil { // hidden: message owner for tray + Synchronize
		return nil, fmt.Errorf("webview: pump window: %w", err)
	}
	if u.hwnd, err = createHostWindow(cfg.Title, cfg.Initial, cfg.ResetSize, cfg.MinW, cfg.MinH, cfg.IconIDs, 0); err != nil {
		return nil, err
	}
	u.register()
	if cfg.DarkTitleBar {
		_ = ApplyDarkTitleBar(uintptr(u.hwnd), true)
	}

	u.disp = rpc.New(cfg.Methods, cfg.QueueSize)
	for name, h := range cfg.CtxMethods {
		u.disp.HandleCtx(name, h)
	}
	if cfg.Roles != nil {
		u.disp.Authorize(func(c rpc.Caller, method string) bool { return cfg.Roles.Allow(c.Role, method) })
	}
	u.disp.Validate(u.callerCurrent)
	u.dataPath = cfg.DataDir
	if u.dataPath == "" {
		u.dataPath = filepath.Join(os.Getenv("LOCALAPPDATA"), "supcua-v2", "webview2")
	}
	b := u.newBrowser(u.dataPath)
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
	u.browser = b

	timeout := cfg.StartupTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	bgR, bgG, bgB := uint8(0xf3), uint8(0xf6), uint8(0xfb)
	if cfg.DarkTitleBar {
		bgR, bgG, bgB = 0x10, 0x13, 0x18
	}
	// Layer 1: read by WebView2 when the environment/controller is created, so the very first frame is themed.
	bgEnv := fmt.Sprintf("FF%02X%02X%02X", bgR, bgG, bgB)
	if err = os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", bgEnv); err != nil {
		log.Printf("webview: default background env: %v", err)
	} else {
		log.Printf("webview: default background %s (env WEBVIEW2_DEFAULT_BACKGROUND_COLOR)", bgEnv)
	}
	if err = b.EmbedTimeout(uintptr(u.hwnd), timeout); err != nil {
		u.browser = nil // never completed: nothing to Close
		return nil, fmt.Errorf("webview startup: %w", err)
	}
	// Fail closed: a host whose protections could not all be applied is never shown.
	if err = u.finishBrowser(cfg.Debug, bgR, bgG, bgB); err != nil {
		return nil, err
	}

	u.disp.Start()
	if err = b.NavigateChecked(serve.Origin + "/" + serve.Index); err != nil {
		return nil, fmt.Errorf("webview navigate: %w", err)
	}
	if !cfg.StartHidden {
		u.Show()
	}
	ok = true
	return u, nil
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
	u.gone.Store(true)
	if u.disp != nil {
		u.disp.Close()
	}
	if u.editor != nil {
		u.destroyEditor(u.editor)
	}
	if u.hwnd != 0 {
		win.ShowWindow(u.hwnd, win.SW_HIDE)
	}
	u.closeBrowser()
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

// Closed reports whether Quit ran; Post drops everything after that.
func (u *UI) Closed() bool { return u.closed.Load() }

// Post runs f on the UI thread. Safe from any goroutine; dropped after Quit.
func (u *UI) Post(f func()) {
	if u.closed.Load() {
		return
	}
	u.mw.Synchronize(f)
}

// Emit pushes an event to the page. Safe from any goroutine; non-blocking.
func (u *UI) Emit(event string, payload any) { u.EmitToWindows(true, false, event, payload) }

// EmitToWindows pushes an event to the main window and/or the editor window. An editor-only
// event while no editor window exists goes to main: the in-main editor route (removed in E2)
// still hosts the editor, and its capture/reset events must keep reaching it. Safe from any
// goroutine; non-blocking.
func (u *UI) EmitToWindows(toMain, toEditor bool, event string, payload any) {
	b, err := rpc.EncodeEvent(event, payload)
	if err != nil {
		log.Printf("webview: emit %q: %v", event, err)
		return
	}
	ed := u.editorWin.Load()
	if toEditor && ed == nil && !toMain {
		toMain = true
	}
	if toMain {
		u.postFrame(b, false)
	}
	if toEditor && ed != nil {
		ed.postFrame(b, false)
	}
}

// Eval runs a script in the page (self-test driver only; production traffic uses Emit).
func (u *UI) Eval(js string) {
	u.Post(func() {
		if u.browser != nil {
			u.browser.Eval(js)
		}
	})
}

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
	u.gone.Store(true)
	if u.disp != nil {
		u.disp.Close() // non-blocking: a handler may be waiting for this very thread
	}
	if u.editor != nil {
		u.destroyEditor(u.editor) // the owned window goes before its owner
	}
	u.closeBrowser()
	if u.hwnd != 0 {
		u.unregister()
		win.DestroyWindow(u.hwnd)
		u.hwnd = 0
	}
}

// Stats are counters for self-tests.
type Stats struct{ BlockedNavigations, DeniedPopups int64 }

func (u *UI) Stats() Stats {
	s := Stats{u.blockedNav.Load(), u.deniedWin.Load()}
	if ed := u.editorWin.Load(); ed != nil {
		s.BlockedNavigations += ed.blockedNav.Load()
		s.DeniedPopups += ed.deniedWin.Load()
	}
	u.retiredMu.Lock()
	s.BlockedNavigations += u.retired.BlockedNavigations
	s.DeniedPopups += u.retired.DeniedPopups
	u.retiredMu.Unlock()
	return s
}

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
	wmDpiChanged    = 0x02E0
	wmExitSizeMove  = 0x0232
	wmDisplayChange = 0x007E
)

func hostProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	w := windowOf(hwnd)
	if w != nil && !w.host.closed.Load() && !w.gone.Load() && w.browser != nil {
		u := w.host
		switch msg {
		case win.WM_SIZE:
			w.browser.Resize()
		case win.WM_SHOWWINDOW:
			if wp != 0 {
				w.presentSurface("shown")
			}
		case win.WM_MOVE, win.WM_MOVING:
			_ = w.browser.NotifyParentWindowPositionChanged()
		case win.WM_SETFOCUS:
			w.browser.Focus()
		case wmDpiChanged:
			r := (*win.RECT)(ptrFrom(lp))
			win.SetWindowPos(hwnd, 0, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top, win.SWP_NOZORDER|win.SWP_NOACTIVATE)
			if w.onDPI != nil {
				w.onDPI()
			}
			if w.role == rpc.RoleEditor {
				u.RecoverEditorOnScreen() // a DPI change can land it off-screen
			}
			return 0
		case win.WM_CLOSE:
			if w.role == rpc.RoleEditor {
				// The editor is never destroyed by X/Alt-F4: the owner decides (contract §4.3).
				if w.onCloseRequest != nil {
					w.onCloseRequest()
				}
				return 0
			}
			if u.cfg.OnClose != nil { // X exits for real (ui/main_window.go:308)
				u.cfg.OnClose()
			} else {
				u.Quit()
			}
			return 0
		}
		if w.role == rpc.RoleEditor {
			switch msg {
			case wmDisplayChange: // a monitor was removed or rearranged
				u.RecoverEditorOnScreen()
			case wmExitSizeMove:
				if w.onBounds != nil {
					w.onBounds()
				}
			case win.WM_ACTIVATE:
				if w.onActivate != nil {
					w.onActivate(uint16(wp&0xFFFF) != win.WA_INACTIVE)
				}
			case win.WM_GETMINMAXINFO:
				mm := (*win.MINMAXINFO)(ptrFrom(lp))
				aw, ah := 0, 0
				if wk, ok := workArea(hwnd); ok {
					aw, ah = wk.R-wk.L, wk.B-wk.T
				}
				mw, mh := scaledMin(windowDPI(hwnd), editorMinCssW, editorMinCssH, aw, ah)
				mm.PtMinTrackSize.X, mm.PtMinTrackSize.Y = int32(mw), int32(mh)
				return 0
			}
		}
		if w.role == rpc.RoleMain {
			switch msg {
			case win.WM_ACTIVATE:
				if u.cfg.OnActivate != nil {
					u.cfg.OnActivate(uint16(wp&0xFFFF) != win.WA_INACTIVE)
				}
			case wmExitSizeMove:
				u.reportBounds()
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
					if ed := u.editor; ed != nil && ed.hwnd != 0 {
						win.ShowWindow(ed.hwnd, win.SW_HIDE) // the owned editor goes with its owner
					}
					win.ShowWindow(hwnd, win.SW_HIDE)
					return 0
				}
			}
		}
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

// createHostWindow registers the class once and creates a native-frame top-level window.
// A non-zero owner makes an owned window (stays above its owner, minimises with it, no taskbar
// button); it is created hidden either way.
func createHostWindow(title string, b Bounds, resetSize bool, minW, minH int, iconIDs []int, owner win.HWND) (win.HWND, error) {
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
	hwnd := win.CreateWindowEx(0, hostClassName, t, win.WS_OVERLAPPEDWINDOW, x, y, w, h, owner, 0, inst, nil)
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
