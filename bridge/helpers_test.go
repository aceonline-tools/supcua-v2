package bridge

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/model"
)

type event struct {
	name    string
	payload any
}

type recEmitter struct {
	mu     sync.Mutex
	events []event
}

func (r *recEmitter) Emit(name string, payload any) {
	r.mu.Lock()
	r.events = append(r.events, event{name, payload})
	r.mu.Unlock()
}

func (r *recEmitter) named(name string) []event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []event
	for _, e := range r.events {
		if e.name == name {
			out = append(out, e)
		}
	}
	return out
}

type fakeStore struct {
	mu       sync.Mutex
	initial  []model.Script
	queued   [][]model.Script
	failIDs  map[string]bool
	hook     func(model.Script, error)
	deleted  []string
	flushed  int
	taken    map[string]bool
	flushOK  bool
	deleteEr error
}

func newFakeStore(initial ...model.Script) *fakeStore {
	return &fakeStore{initial: initial, failIDs: map[string]bool{}, taken: map[string]bool{}, flushOK: true}
}

func (f *fakeStore) List() ([]model.Script, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.Script(nil), f.initial...), nil
}
func (f *fakeStore) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return f.deleteEr
}
func (f *fakeStore) NameOrFileTaken(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.taken[name]
}
func (f *fakeStore) QueueSave(scripts ...model.Script) {
	f.mu.Lock()
	f.queued = append(f.queued, append([]model.Script(nil), scripts...))
	hook := f.hook
	var failed []model.Script
	for _, s := range scripts {
		if f.failIDs[s.ID] {
			failed = append(failed, s)
		}
	}
	f.mu.Unlock()
	for _, s := range failed {
		if hook != nil {
			hook(s, errors.New("disk full"))
		}
	}
}
func (f *fakeStore) FlushSaves(time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushed++
	return f.flushOK
}
func (f *fakeStore) SetOnSaveError(h func(model.Script, error)) {
	f.mu.Lock()
	f.hook = h
	f.mu.Unlock()
}
func (f *fakeStore) calls() [][]model.Script {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]model.Script(nil), f.queued...)
}

type memPersister struct {
	mu   sync.Mutex
	cfg  model.AppConfig
	fail bool
	n    int
}

func (m *memPersister) Load() (model.AppConfig, error) { return m.cfg, nil }
func (m *memPersister) Save(c model.AppConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("readonly")
	}
	m.cfg = c
	m.n++
	return nil
}

type fakeHotkeys struct {
	mu    sync.Mutex
	lists [][]model.Script
}

func (h *fakeHotkeys) SetScripts(s []model.Script) {
	h.mu.Lock()
	h.lists = append(h.lists, s)
	h.mu.Unlock()
}

type fakeAuto struct {
	mu    sync.Mutex
	log   []string
	names []string
}

func (a *fakeAuto) Start() { a.mu.Lock(); a.log = append(a.log, "start"); a.mu.Unlock() }
func (a *fakeAuto) Stop()  { a.mu.Lock(); a.log = append(a.log, "stop"); a.mu.Unlock() }
func (a *fakeAuto) SetProcessName(n string) {
	a.mu.Lock()
	a.names = append(a.names, n)
	a.mu.Unlock()
}

type fakeLocks struct{ mouse, kb bool }

func (l fakeLocks) MouseLocked() bool    { return l.mouse }
func (l fakeLocks) KeyboardLocked() bool { return l.kb }

type fakeOverlay struct {
	mu    sync.Mutex
	state OverlayDTO
	err   error
}

func (o *fakeOverlay) State() OverlayDTO { o.mu.Lock(); defer o.mu.Unlock(); return o.state }
func (o *fakeOverlay) SetEnabled(v bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return o.err
	}
	o.state.Enabled = v
	return nil
}
func (o *fakeOverlay) SetVisible(v bool)   { o.mu.Lock(); o.state.Visible = v; o.mu.Unlock() }
func (o *fakeOverlay) SetDraggable(v bool) { o.mu.Lock(); o.state.Draggable = v; o.mu.Unlock() }

type fakeHost struct {
	mu        sync.Mutex
	rebinds   []string
	failKind  string
	clip      string
	hidden    int
	exited    int
	updateErr error
	progress  []int
	release   chan struct{}
}

func (h *fakeHost) Hide() { h.mu.Lock(); h.hidden++; h.mu.Unlock() }
func (h *fakeHost) Show() {}
func (h *fakeHost) Exit() { h.mu.Lock(); h.exited++; h.mu.Unlock() }
func (h *fakeHost) WriteClipboard(t string) error {
	h.mu.Lock()
	h.clip = t
	h.mu.Unlock()
	return nil
}
func (h *fakeHost) BootInfo() BootState {
	return BootState{Mode: "main", Version: "v1", RuntimeVersion: "130"}
}
func (h *fakeHost) RebindHotkey(kind string, hk model.Hotkey) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rebinds = append(h.rebinds, kind+"="+DisplayHotkey(hk))
	if kind == h.failKind && hk.Key != "keep" {
		return errors.New("in use")
	}
	return nil
}
func (h *fakeHost) ApplyUpdate(progress func(int)) error {
	progress(40)
	if h.release != nil {
		<-h.release
	}
	return h.updateErr
}
func (h *fakeHost) SkipUpdate() {}

type fakeBackend struct {
	mu      sync.Mutex
	starts  []CaptureMode
	stops   int
	sink    CaptureSink
	pickKey model.Hotkey
	err     error
}

func (f *fakeBackend) Start(mode CaptureMode, pick model.Hotkey, s CaptureSink) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.starts = append(f.starts, mode)
	f.sink, f.pickKey = s, pick
	return nil
}
func (f *fakeBackend) Stop() { f.mu.Lock(); f.stops++; f.mu.Unlock() }

type syncUI struct{}

func (syncUI) Post(fn func()) { fn() }

type rig struct {
	b     *Bridge
	ctl   *app.Controller
	store *fakeStore
	cfg   *ConfigService
	per   *memPersister
	em    *recEmitter
	hk    *fakeHotkeys
	auto  *fakeAuto
	ov    *fakeOverlay
	host  *fakeHost
	cap   *fakeBackend
	m     map[string]Handler
}

func newRig(t *testing.T, scripts ...model.Script) *rig {
	t.Helper()
	r := &rig{
		ctl: app.NewController(nil, nil, nil, nil), store: newFakeStore(scripts...), per: &memPersister{cfg: model.DefaultConfig()},
		em: &recEmitter{}, hk: &fakeHotkeys{}, auto: &fakeAuto{}, ov: &fakeOverlay{state: OverlayDTO{Enabled: true, Visible: true, X: 10, Y: 10}},
		host: &fakeHost{}, cap: &fakeBackend{},
	}
	var err error
	if r.cfg, err = NewConfigService(r.per); err != nil {
		t.Fatal(err)
	}
	r.b = New(Deps{
		Controller: r.ctl, Store: r.store, Config: r.cfg, Hotkeys: r.hk, AutoActive: r.auto, Locks: fakeLocks{mouse: true},
		Overlay: r.ov, UI: syncUI{}, Capture: r.cap, Host: r.host,
	}, r.em)
	r.m = r.b.Methods()
	return r
}

// call invokes a handler with JSON params and returns the result re-decoded as a generic value.
func (r *rig) call(t *testing.T, method, params string) (map[string]any, error) {
	t.Helper()
	h, ok := r.m[method]
	if !ok {
		t.Fatalf("method %s not registered", method)
	}
	var raw json.RawMessage
	if params != "" {
		raw = json.RawMessage(params)
	}
	out, err := h(raw)
	if err != nil {
		return nil, err
	}
	data, merr := json.Marshal(out)
	if merr != nil {
		t.Fatalf("marshal result of %s: %v", method, merr)
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return map[string]any{"_raw": string(data)}, nil
	}
	return m, nil
}

func (r *rig) mustCall(t *testing.T, method, params string) map[string]any {
	t.Helper()
	m, err := r.call(t, method, params)
	if err != nil {
		t.Fatalf("%s(%s): %v", method, params, err)
	}
	return m
}

func wantCode(t *testing.T, err error, code string) *Error {
	t.Helper()
	var e *Error
	if err == nil || !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want error %s, got %v", code, err)
	}
	return e
}

func script(id, name, group string, enabled bool) model.Script {
	return model.Script{ID: id, Name: name, Group: group, Enabled: enabled, RunMode: "once", HotkeyMode: "toggle", Actions: []model.Action{}}
}
