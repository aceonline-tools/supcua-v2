package hotkey

import (
	"errors"
	"sync"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// RegisterTimeout bounds one Register call. The listener normally answers in microseconds; a
// wait longer than this means its thread is stuck, and callers (startup, the UI thread) must
// not hang with it.
const RegisterTimeout = 3 * time.Second

// ErrRegisterTimeout is returned when the listener did not answer a registration in time.
var ErrRegisterTimeout = errors.New("hotkey registration timed out (listener not responding)")

type regReply struct {
	id  int
	err error
}

// regRequest is one registration. mu makes "the listener executes it" and "the caller gives up"
// mutually exclusive: a request is either executed (and its reply is then visible to the caller
// that timed out) or cancelled before the listener starts it. A timed-out caller can therefore
// never leave an untracked OS registration behind, and no helper goroutine is needed.
type regRequest struct {
	hotkey   model.Hotkey
	callback HotkeyCallback
	reply    chan regReply

	mu        sync.Mutex
	cancelled bool
}

func newRegRequest(hk model.Hotkey, cb HotkeyCallback) *regRequest {
	return &regRequest{hotkey: hk, callback: cb, reply: make(chan regReply, 1)}
}

// serve runs on the listener thread: it executes the request unless the caller already gave up.
func (r *regRequest) serve(exec func(model.Hotkey, HotkeyCallback) regReply) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelled {
		return
	}
	r.reply <- exec(r.hotkey, r.callback)
}

// registerBounded queues a registration for the listener and waits at most d for the answer.
func registerBounded(ch chan<- *regRequest, hk model.Hotkey, cb HotkeyCallback, d time.Duration) (int, error) {
	req := newRegRequest(hk, cb)
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case ch <- req:
	case <-timer.C:
		return 0, ErrRegisterTimeout
	}
	select {
	case r := <-req.reply:
		return r.id, r.err
	case <-timer.C:
	}
	req.mu.Lock() // waits out an execution already in flight (RegisterHotKey is fast)
	defer req.mu.Unlock()
	select {
	case r := <-req.reply:
		return r.id, r.err
	default:
		req.cancelled = true
		return 0, ErrRegisterTimeout
	}
}
