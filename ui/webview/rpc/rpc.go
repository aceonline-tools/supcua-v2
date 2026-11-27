// Package rpc is the JS<->Go dispatcher of the WebView2 host. It is platform-neutral and
// reflection-free: methods are looked up by string key and receive the raw params JSON.
// It never imports the backend: handlers are plain func values (e.g. bridge.Methods()).
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	CodeBadRequest    = "BAD_REQUEST"
	CodeUnknownMethod = "UNKNOWN_METHOD"
	CodeBusy          = "BUSY"
	CodeInternal      = "INTERNAL"
	CodeForbiddenRole = "FORBIDDEN_ROLE"
	CodeStaleGen      = "STALE_GEN"

	DefaultQueue = 64
)

// Handler matches bridge.Handler (an alias of this func type).
type Handler = func(json.RawMessage) (any, error)

// Role is the window a call came from; it is set by the transport, never by the page.
type Role uint8

const (
	RoleMain Role = iota + 1
	RoleEditor
)

func (r Role) String() string {
	switch r {
	case RoleMain:
		return "main"
	case RoleEditor:
		return "editor"
	}
	return "unknown"
}

// Caller identifies the window (and its generation) that submitted a call.
type Caller struct {
	Role Role
	Gen  uint64
	HWND uintptr
}

// Reply is a request-local sink for one encoded response. It must not block; it is called from
// the worker goroutine (and from Submit for BAD_REQUEST/BUSY/FORBIDDEN_ROLE) and decides on its
// own whether its window still exists.
type Reply func([]byte)

// HandlerCtx is a handler that needs the caller identity (rpc.CallerFrom).
type HandlerCtx = func(ctx context.Context, params json.RawMessage) (any, error)

type callerKey struct{}

// CallerFrom returns the caller attached by the transport to the handler's context.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// Deferred lets a handler complete its reply later without holding the worker: return it from a
// handler and call Resolve once from any goroutine. The reply goes through the same request-local
// sink and the same delivery checks as an immediate one; a Deferred never produces two replies.
type Deferred struct {
	mu   sync.Mutex
	done bool
	out  any
	err  error
	fin  func(any, error)
}

func NewDeferred() *Deferred { return &Deferred{} }

// Resolve completes the request; later calls are ignored.
func (d *Deferred) Resolve(out any, err error) {
	d.mu.Lock()
	if d.done {
		d.mu.Unlock()
		return
	}
	d.done, d.out, d.err = true, out, err
	fin := d.fin
	d.mu.Unlock()
	if fin != nil {
		fin(out, err)
	}
}

func (d *Deferred) bind(fin func(any, error)) {
	d.mu.Lock()
	if d.fin != nil {
		d.mu.Unlock()
		return
	}
	d.fin = fin
	done, out, err := d.done, d.out, d.err
	d.mu.Unlock()
	if done {
		fin(out, err)
	}
}

// Matrix is the role matrix (contract §3.2). The editor is default-deny: it may call only methods
// listed for it. The main window may call everything except the editor-only methods, minus the
// transitional list that main keeps until the in-main editor route is removed.
type Matrix struct {
	both, mainOnly, editorOnly, transitional map[string]struct{}
}

func set(l []string) map[string]struct{} {
	m := make(map[string]struct{}, len(l))
	for _, n := range l {
		m[n] = struct{}{}
	}
	return m
}

func NewMatrix(both, mainOnly, editorOnly, transitionalMain []string) *Matrix {
	return &Matrix{set(both), set(mainOnly), set(editorOnly), set(transitionalMain)}
}

// Allow reports whether role may call method.
func (m *Matrix) Allow(role Role, method string) bool {
	if m == nil {
		return true
	}
	_, both := m.both[method]
	switch role {
	case RoleMain:
		if _, ed := m.editorOnly[method]; ed {
			_, tr := m.transitional[method]
			return tr
		}
		return true
	case RoleEditor:
		if both {
			return true
		}
		_, ed := m.editorOnly[method]
		return ed
	}
	return false
}

type request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type response struct {
	ID     int64           `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

// item is one unit of worker work: a page request, or a host job when job != nil.
type item struct {
	req    request
	caller Caller
	reply  Reply
	job    func()
}

// Dispatcher executes every page call and every host job on ONE worker goroutine, strictly in
// submission order (serialized mutations, no goroutine per message). Submit never blocks; when
// queueSize page requests are already waiting it answers BUSY. Host jobs are never refused for
// fullness. Close is terminal: queued work is discarded and nothing is accepted afterwards.
type Dispatcher struct {
	handlers map[string]Handler
	ctxH     map[string]HandlerCtx
	limit    int
	stopped  chan struct{}

	authorize func(Caller, string) bool // set before Start; nil = allow all
	valid     func(Caller) bool         // set before Start; nil = every caller current

	mu     sync.Mutex
	wake   *sync.Cond
	items  []item
	nreq   int  // page requests in items
	closed bool // terminal
}

// New copies methods; queueSize <= 0 selects DefaultQueue.
func New(methods map[string]Handler, queueSize int) *Dispatcher {
	if queueSize <= 0 {
		queueSize = DefaultQueue
	}
	h := make(map[string]Handler, len(methods))
	for k, v := range methods {
		h[k] = v
	}
	d := &Dispatcher{handlers: h, ctxH: map[string]HandlerCtx{}, limit: queueSize, stopped: make(chan struct{})}
	d.wake = sync.NewCond(&d.mu)
	return d
}

// HandleCtx registers a handler that receives the caller identity. Call before Start.
func (d *Dispatcher) HandleCtx(name string, h HandlerCtx) { d.ctxH[name] = h }

// Authorize installs the role check (called in Submit: a denied call is answered FORBIDDEN_ROLE
// to its own sink and never takes a queue slot). Call before Start.
func (d *Dispatcher) Authorize(f func(Caller, string) bool) { d.authorize = f }

// Validate installs the generation check, evaluated at dequeue (a stale caller's handler is not
// run; its sink gets STALE_GEN) and again at delivery (a stale caller's result is dropped). It
// must be safe from any goroutine. Call before Start.
func (d *Dispatcher) Validate(f func(Caller) bool) { d.valid = f }

// Start launches the single worker shared by every window. Replies go to each request's own
// Reply; nothing is delivered once the dispatcher is closed.
func (d *Dispatcher) Start() {
	go func() {
		defer close(d.stopped)
		for {
			it, ok := d.take()
			if !ok {
				return
			}
			if it.job != nil {
				it.job()
				continue
			}
			d.serve(it)
		}
	}()
}

func (d *Dispatcher) current(c Caller) bool { return d.valid == nil || d.valid(c) }

// serve runs one request on the worker.
func (d *Dispatcher) serve(it item) {
	if !d.current(it.caller) {
		d.send(it, encode(response{ID: it.req.ID, Error: errObj(CodeStaleGen, "window superseded")}), false)
		return
	}
	// TODO(E1b, editor-window §6.5): invalidate(gen) does not stop a handler that already passed
	// this check. Commit fencing for in-flight editor calls is the bridge's per-session `closing`
	// state + save lease; it must land with the session/watchdog milestone.
	out, deferred := d.run(it)
	if deferred != nil {
		deferred.bind(func(v any, err error) { d.send(it, d.finish(it.req, v, err), true) })
		return
	}
	d.send(it, out, true)
}

// send delivers one encoded response to the request's sink. checkCurrent drops it when the
// caller's window was closed or superseded meanwhile.
func (d *Dispatcher) send(it item, out []byte, checkCurrent bool) {
	if it.reply == nil || d.isClosed() || (checkCurrent && !d.current(it.caller)) {
		return
	}
	it.reply(out)
}

func (d *Dispatcher) take() (item, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for len(d.items) == 0 && !d.closed {
		d.wake.Wait()
	}
	if d.closed {
		return item{}, false
	}
	it := d.items[0]
	d.items[0] = item{}
	d.items = d.items[1:]
	if it.job == nil {
		d.nreq--
	}
	return it, true
}

func (d *Dispatcher) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

// Submit decodes one raw message and enqueues it. Called on the UI thread from the
// WebView2 message callback: it does no I/O and never blocks. Dropped silently once closed.
// reply is this request's own sink, used on every path (malformed, forbidden, BUSY, handler
// error, success, deferred).
func (d *Dispatcher) Submit(raw string, caller Caller, reply Reply) {
	it := item{caller: caller, reply: reply}
	var req request
	if err := json.Unmarshal([]byte(raw), &req); err != nil || req.Method == "" {
		d.send(it, encode(response{Error: errObj(CodeBadRequest, "invalid request")}), false)
		return
	}
	it.req = req
	if d.authorize != nil && !d.authorize(caller, req.Method) {
		d.send(it, encode(response{ID: req.ID, Error: errObj(CodeForbiddenRole, "method not allowed for this window")}), false)
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	if d.nreq >= d.limit {
		d.mu.Unlock()
		d.send(it, encode(response{ID: req.ID, Error: errObj(CodeBusy, "call queue full")}), false)
		return
	}
	d.items = append(d.items, it)
	d.nreq++
	d.wake.Signal()
	d.mu.Unlock()
}

// Enqueue appends a host job to the worker queue: it runs after every request submitted
// before it (queued or in flight) has finished and before any request submitted after it.
// Reports false (job not queued) once the dispatcher is closed. Never blocks.
func (d *Dispatcher) Enqueue(job func()) bool {
	if job == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	d.items = append(d.items, item{job: job})
	d.wake.Signal()
	return true
}

// Close is the terminal quarantine: Submit and Enqueue reject from now on, queued requests
// and jobs are discarded unrun, and the worker exits after the item in flight (whose reply is
// suppressed). It never blocks, so it is safe on the UI thread while a handler waits for the
// UI thread. Idempotent.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	d.items, d.nreq = nil, 0
	d.wake.Broadcast()
	d.mu.Unlock()
}

// Wait blocks until the worker has exited or timeout elapses.
func (d *Dispatcher) Wait(timeout time.Duration) bool {
	select {
	case <-d.stopped:
		return true
	case <-time.After(timeout):
		return false
	}
}

// run executes the handler; a *Deferred result is returned for later completion.
func (d *Dispatcher) run(it item) ([]byte, *Deferred) {
	req := it.req
	var out any
	var err error
	if h, ok := d.ctxH[req.Method]; ok {
		out, err = safeCall(func(p json.RawMessage) (any, error) {
			return h(context.WithValue(context.Background(), callerKey{}, it.caller), p)
		}, req.Params)
	} else if h, ok := d.handlers[req.Method]; ok {
		out, err = safeCall(h, req.Params)
	} else {
		return encode(response{ID: req.ID, Error: errObj(CodeUnknownMethod, "unknown method")}), nil
	}
	if df, ok := out.(*Deferred); ok && err == nil && df != nil {
		return nil, df
	}
	return d.finish(req, out, err), nil
}

func (d *Dispatcher) finish(req request, out any, err error) []byte {
	if err != nil {
		return encode(response{ID: req.ID, Error: toWireError(err)})
	}
	raw, merr := json.Marshal(out)
	if merr != nil {
		return encode(response{ID: req.ID, Error: errObj(CodeInternal, "result not encodable")})
	}
	return encode(response{ID: req.ID, OK: true, Result: raw})
}

func safeCall(h Handler, params json.RawMessage) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, errors.New("handler panic")
		}
	}()
	return h(params)
}

// coded is satisfied by errors exposing a stable code/message pair.
type coded interface {
	error
	ErrorCode() string
	ErrorMessage() string
}

// toWireError maps a handler error to the wire form WITHOUT leaking Go error text or type
// names (garble renames types, and messages must be stable): coded errors keep their
// code/message; a *bridge.Error (json-tagged struct with code/message and optional extra
// fields such as conflict details) is passed through in its JSON form; anything else
// becomes INTERNAL.
func toWireError(err error) json.RawMessage {
	var c coded
	if errors.As(err, &c) {
		return errObj(c.ErrorCode(), c.ErrorMessage())
	}
	if b, merr := json.Marshal(err); merr == nil {
		var probe struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(b, &probe) == nil && probe.Code != "" && len(b) > 0 && b[0] == '{' {
			return b
		}
	}
	return errObj(CodeInternal, "internal error")
}

func errObj(code, message string) json.RawMessage {
	b, _ := json.Marshal(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{code, message})
	return b
}

func encode(r response) []byte {
	b, err := json.Marshal(r)
	if err != nil {
		return []byte(`{"id":0,"ok":false,"error":{"code":"INTERNAL","message":"encode"}}`)
	}
	return b
}
