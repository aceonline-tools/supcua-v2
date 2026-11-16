// Package rpc is the JS<->Go dispatcher of the WebView2 host. It is platform-neutral and
// reflection-free: methods are looked up by string key and receive the raw params JSON.
// It never imports the backend: handlers are plain func values (e.g. bridge.Methods()).
package rpc

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
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

// Dispatcher executes every call on ONE worker goroutine (serialized mutations, no
// goroutine per message). Submit never blocks; a full queue answers BUSY.
type Dispatcher struct {
	handlers map[string]Handler
	queue    chan request
	reply    func([]byte)
	done     chan struct{}
	stopped  chan struct{}
	once     sync.Once
	held     atomic.Int32
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
	return &Dispatcher{
		handlers: h,
		queue:    make(chan request, queueSize),
		done:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

// Start launches the single worker; reply receives each encoded response and is called
// from the worker goroutine (and from Submit for BAD_REQUEST/BUSY) — it must not block.
func (d *Dispatcher) Start(reply func([]byte)) {
	d.reply = reply
	go func() {
		defer close(d.stopped)
		for {
			select {
			case <-d.done:
				return
			case req := <-d.queue:
				d.reply(d.run(req))
			}
		}
	}()
}

// Submit decodes one raw message and enqueues it. Called on the UI thread from the
// WebView2 message callback: it does no I/O and never blocks.
func (d *Dispatcher) Submit(raw string) {
	var req request
	if err := json.Unmarshal([]byte(raw), &req); err != nil || req.Method == "" {
		d.reply(encode(response{Error: errObj(CodeBadRequest, "invalid request")}))
		return
	}
	if d.held.Load() > 0 { // a new document must not talk to the backend before the old page state is reset
		d.reply(encode(response{ID: req.ID, Error: errObj(CodeBusy, "call queue full")}))
		return
	}
	select {
	case <-d.done:
	case d.queue <- req:
	default:
		d.reply(encode(response{ID: req.ID, Error: errObj(CodeBusy, "call queue full")}))
	}
}

// Hold makes Submit answer BUSY until the matching Release (calls nest). The host holds across
// a page navigation/crash so requests of the NEW document cannot race the reset of the old
// document's backend state (editor session, capture).
func (d *Dispatcher) Hold() { d.held.Add(1) }

// Release undoes one Hold.
func (d *Dispatcher) Release() { d.held.Add(-1) }

// Close signals the worker to stop after the call in flight. It never blocks, so it is
// safe on the UI thread while a handler waits for the UI thread.
func (d *Dispatcher) Close() { d.once.Do(func() { close(d.done) }) }

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
