package ui

import (
	"sync"

	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/ui/webview"
)

// EditorHost implements bridge.EditorHost over the process WebView2 host: it translates the
// controller's window operations into native calls and the native callbacks into controller
// events. Every method runs on the UI thread (Post excepted).
type EditorHost struct {
	U      *webview.UI
	Events func() bridge.EditorEvents // the controller (bridge.EditorEvents)
	Blur   func()                     // the editor lost activation: ends an active capture
	// Fixture, when set (self-test only), is served as the editor page.
	Fixture func() string

	mu  sync.Mutex
	log []string
}

func (h *EditorHost) rec(s string) { h.mu.Lock(); h.log = append(h.log, s); h.mu.Unlock() }

// Log is the ordered record of native modality and destroy calls (self-test evidence).
func (h *EditorHost) Log() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.log...)
}

func (h *EditorHost) ResetLog() { h.mu.Lock(); h.log = nil; h.mu.Unlock() }

func (h *EditorHost) Post(f func())     { h.U.Post(f) }
func (h *EditorHost) MainVisible() bool { return h.U.Visible() }

func (h *EditorHost) EnableMain(enabled bool) {
	if enabled {
		h.rec("enableMain=true")
	} else {
		h.rec("enableMain=false")
	}
	h.U.SetMainEnabled(enabled)
}

func (h *EditorHost) RevealMain() { h.rec("revealMain"); h.U.Window.Show() }

func (h *EditorHost) options() webview.EditorOptions {
	ev := h.Events()
	o := webview.EditorOptions{
		Title:   "Script editor",
		OnReady: func(w *webview.Window) { ev.WindowReady(w.Gen()) },
		OnFailed: func(w *webview.Window, err error) {
			ev.WindowFailed(w.Gen(), "embed", "the editor window could not be created")
		},
		OnProcessFail:  func(w *webview.Window, kind int32) { ev.ProcessFailed(w.Gen(), kind) },
		OnCloseRequest: func(w *webview.Window) { ev.CloseRequested(w.Gen()) },
		OnDPIChanged:   func(w *webview.Window) { ev.DPIChanged(w.Gen()) },
		OnBoundsDone: func(w *webview.Window) {
			if l, t, r, b, max, ok := h.U.EditorWindowBounds(w.Gen()); ok {
				ev.BoundsChanged(w.Gen(), bridge.Rect{L: l, T: t, R: r, B: b}, max)
			}
		},
		OnActivate: func(_ *webview.Window, active bool) {
			if !active && h.Blur != nil {
				h.Blur()
			}
		},
	}
	if h.Fixture != nil {
		o.Fixture = h.Fixture()
	}
	return o
}

// Prewarm (EXPERIMENT) boots a spare editor page now and again after every editor close.
func (h *EditorHost) Prewarm() {
	h.U.SetPrewarm(func() { h.U.PrewarmEditorWindow(h.options()) })
	h.U.Post(func() { h.U.PrewarmEditorWindow(h.options()) })
}

func (h *EditorHost) CreateEditor() (uint64, error) {
	o := h.options()
	w, err := h.U.CreateEditorWindow(o)
	var gen uint64
	if w != nil {
		gen = w.Gen() // valid even on error: the caller destroys the half-made window after rollback
	}
	if err != nil {
		return gen, err
	}
	h.rec("create")
	return gen, nil
}

func (h *EditorHost) PrepareEditor(gen uint64, rem model.EditorWindow) (bridge.EditorMetrics, error) {
	var r *[4]int
	if rem.W > 0 && rem.H > 0 {
		r = &[4]int{rem.X, rem.Y, rem.X + rem.W, rem.Y + rem.H}
	}
	f, err := h.U.PrepareEditorWindow(gen, r)
	if err != nil {
		return bridge.EditorMetrics{}, err
	}
	return bridge.EditorMetrics{DPI: f.DPI, FrameW: f.FrameW, FrameH: f.FrameH,
		Work: bridge.Rect{L: f.WorkL, T: f.WorkT, R: f.WorkR, B: f.WorkB}, OwnerCX: f.OwnerCX, OwnerCY: f.OwnerCY}, nil
}

func (h *EditorHost) PlaceEditor(gen uint64, r bridge.Rect, maximized bool) {
	h.U.PlaceEditorWindow(gen, r.L, r.T, r.R, r.B, maximized)
}

func (h *EditorHost) EditorStaged(gen uint64) bool { return h.U.EditorStaged(gen) }
func (h *EditorHost) ShowEditor(gen uint64)        { h.rec("show"); h.U.ShowEditorWindow(gen) }

func (h *EditorHost) EditorBounds(gen uint64) (bridge.Rect, bool, bool) {
	l, t, r, b, max, ok := h.U.EditorWindowBounds(gen)
	return bridge.Rect{L: l, T: t, R: r, B: b}, max, ok
}

func (h *EditorHost) DestroyEditor(gen uint64) { h.rec("destroy"); h.U.DestroyEditorWindow(gen) }

func (h *EditorHost) EmitEditor(gen uint64, event string, payload any) {
	h.U.EmitEditor(gen, event, payload)
}

var _ bridge.EditorHost = (*EditorHost)(nil)
