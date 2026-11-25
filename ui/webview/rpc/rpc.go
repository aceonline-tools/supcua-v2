// Package rpc is the JS<->Go dispatcher of the WebView2 host. It is platform-neutral and
// reflection-free: methods are looked up by string key and receive the raw params JSON.
// It never imports the backend: handlers are plain func values (e.g. bridge.Methods()).
package rpc

import (
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

	DefaultQueue = 64
)

// Handler matches bridge.Handler (an alias of this func type).
type Handler = func(json.RawMessage) (any, error)

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
	req request
	job func()
}

// Dispatcher executes every page call and every host job on ONE worker goroutine, strictly in
// submission order (serialized mutations, no goroutine per message). Submit never blocks; when
// queueSize page requests are already waiting it answers BUSY. Host jobs are never refused for
// fullness. Close is terminal: queued work is discarded and nothing is accepted afterwards.
type Dispatcher struct {
	handlers map[string]Handler
	limit    int
	reply    func([]byte)
	stopped  chan struct{}

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
	d := &Dispatcher{handlers: h, limit: queueSize, stopped: make(chan struct{})}
	d.wake = sync.NewCond(&d.mu)
	return d
}

// Start launches the single worker; reply receives each encoded response and is called
// from the worker goroutine (and from Submit for BAD_REQUEST/BUSY) — it must not block.
// No reply is delivered once the dispatcher is closed.
func (d *Dispatcher) Start(reply func([]byte)) {
	d.reply = reply
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
			out := d.run(it.req)
			if !d.isClosed() {
				d.reply(out)
			}
		}
	}()
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
func (d *Dispatcher) Submit(raw string) {
	var req request
	if err := json.Unmarshal([]byte(raw), &req); err != nil || req.Method == "" {
		if !d.isClosed() {
			d.reply(encode(response{Error: errObj(CodeBadRequest, "invalid request")}))
		}
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	if d.nreq >= d.limit {
		d.mu.Unlock()
		d.reply(encode(response{ID: req.ID, Error: errObj(CodeBusy, "call queue full")}))
		return
	}
	d.items = append(d.items, item{req: req})
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

func (d *Dispatcher) run(req request) []byte {
	h, ok := d.handlers[req.Method]
	if !ok {
		return encode(response{ID: req.ID, Error: errObj(CodeUnknownMethod, "unknown method")})
	}
	out, err := safeCall(h, req.Params)
	if err != nil {
		return encode(response{ID: req.ID, Error: toWireError(err)})
	}
	raw, err := json.Marshal(out)
	if err != nil {
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
