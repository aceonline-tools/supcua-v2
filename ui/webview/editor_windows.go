package webview

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

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
}

// Gen is the window generation (contract §4.6).
func (w *Window) Gen() uint64 { return w.gen }

// HWND is the native handle (0 once destroyed).
func (w *Window) HWND() uintptr { return uintptr(w.hwnd) }

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
	w.register()
	u.editor = w
	u.editorWin.Store(w)
	u.editorGen.Store(w.gen)

	var once sync.Once
	fail := func(err error) {
		once.Do(func() {
			log.Printf("webview: editor window gen=%d startup failed: %v", w.gen, err)
			u.destroyEditor(w)
			if o.OnFailed != nil {
				o.OnFailed(w, err)
			}
		})
	}
	b := w.newBrowser(u.dataPath)
	b.ProcessFailedCallback = func(kind int32) { w.onProcessFailed(kind) }
	b.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
		success, status := args.NavigationResult()
		if !success {
			log.Printf("webview: editor navigation failed (web error status %d)", status)
		}
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
		timer.Stop()
		fail(err)
		return nil, err
	}
	return w, nil
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
	if w.browser != nil {
		w.browser.Abandon()
	}
	w.closeBrowser()
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
