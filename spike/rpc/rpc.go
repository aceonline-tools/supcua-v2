// Package rpc is a reflection-free JS<->Go dispatcher: methods are looked up by
// string key in a map and decoded into explicit, json-tagged parameter types.
// Nothing depends on Go identifier names, so garble renaming cannot break it.
package rpc

import (
	"encoding/json"
	"errors"
	"sync"
)

const (
	CodeBadRequest    = "BAD_REQUEST"
	CodeUnknownMethod = "UNKNOWN_METHOD"
	CodeBusy          = "BUSY"
	CodeInternal      = "INTERNAL"
)

type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type Response struct {
	ID     int64           `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type handler func(json.RawMessage) (any, error)

// Dispatcher runs every call on ONE worker goroutine, so state mutations are
// serialized and no goroutine is spawned per call.
type Dispatcher struct {
	handlers map[string]handler
	queue    chan Request
	reply    func([]byte)
	done     chan struct{}
	once     sync.Once
}

func NewDispatcher(queueSize int) *Dispatcher {
	return &Dispatcher{
		handlers: map[string]handler{},
		queue:    make(chan Request, queueSize),
		done:     make(chan struct{}),
	}
}

// Register binds method to fn. Call before Start; the map is read-only afterwards.
func Register[P any, R any](d *Dispatcher, method string, fn func(P) (R, error)) {
	d.handlers[method] = func(raw json.RawMessage) (any, error) {
		var p P
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, &Error{Code: CodeBadRequest, Message: "invalid params"}
			}
		}
		return fn(p)
	}
}

// Start launches the single worker. reply receives encoded Response JSON and
// must be safe to call from the worker goroutine (it should marshal to the UI thread).
func (d *Dispatcher) Start(reply func([]byte)) {
	d.reply = reply
	go func() {
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

// Submit decodes one raw message and enqueues it without blocking.
func (d *Dispatcher) Submit(raw string) {
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		d.reply(encode(Response{Error: &Error{Code: CodeBadRequest, Message: "invalid request"}}))
		return
	}
	select {
	case d.queue <- req:
	default:
		d.reply(encode(Response{ID: req.ID, Error: &Error{Code: CodeBusy, Message: "call queue full"}}))
	}
}

func (d *Dispatcher) Close() { d.once.Do(func() { close(d.done) }) }

func (d *Dispatcher) run(req Request) []byte {
	h, ok := d.handlers[req.Method]
	if !ok {
		return encode(Response{ID: req.ID, Error: &Error{Code: CodeUnknownMethod, Message: req.Method}})
	}
	out, err := h(req.Params)
	if err != nil {
		var e *Error
		if !errors.As(err, &e) {
			e = &Error{Code: CodeInternal, Message: err.Error()}
		}
		return encode(Response{ID: req.ID, Error: e})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return encode(Response{ID: req.ID, Error: &Error{Code: CodeInternal, Message: err.Error()}})
	}
	return encode(Response{ID: req.ID, OK: true, Result: raw})
}

func encode(r Response) []byte {
	b, err := json.Marshal(r)
	if err != nil {
		return []byte(`{"id":0,"ok":false,"error":{"code":"INTERNAL","message":"encode"}}`)
	}
	return b
}
