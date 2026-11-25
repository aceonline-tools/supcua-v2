package hotkey

import (
	"errors"
	"sync/atomic"
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

// Request states. The caller and the listener race only through compare-and-swap; no lock is
// held while the listener executes, so the caller's deadline is always honoured.
const (
	reqPending   int32 = iota // queued, not started
	reqExecuting              // the listener is running RegisterHotKey
	reqDone                   // finished and result published (res)
	reqCancelled              // caller gave up before the listener started: never executed
	reqAbandoned              // caller gave up while executing: the listener releases the result
)

type regRequest struct {
	hotkey   model.Hotkey
	callback HotkeyCallback

	state atomic.Int32
	res   regReply      // written by the listener before the CAS to reqDone
	done  chan struct{} // closed by the listener on reqDone
}

func newRegRequest(hk model.Hotkey, cb HotkeyCallback) *regRequest {
	return &regRequest{hotkey: hk, callback: cb, done: make(chan struct{})}
}

// serve runs on the listener thread. A cancelled request is skipped. If the caller abandoned the
// request while it executed, the listener itself releases a successful registration (it owns it
// and drops its callback), so nothing is orphaned and no helper goroutine exists.
func (r *regRequest) serve(exec func(model.Hotkey, HotkeyCallback) regReply, release func(id int)) {
	if !r.state.CompareAndSwap(reqPending, reqExecuting) {
		return
	}
	res := exec(r.hotkey, r.callback)
	r.res = res
	if r.state.CompareAndSwap(reqExecuting, reqDone) {
		close(r.done)
		return
	}
	if res.err == nil && res.id != 0 && release != nil {
		release(res.id)
	}
}

// registerBounded queues a registration for the listener and returns within d in all cases.
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
	case <-req.done:
		return req.res.id, req.res.err
	case <-timer.C:
	}
	if req.state.CompareAndSwap(reqPending, reqCancelled) || req.state.CompareAndSwap(reqExecuting, reqAbandoned) {
		return 0, ErrRegisterTimeout
	}
	<-req.done // state is reqDone: the listener closes it immediately after the CAS
	return req.res.id, req.res.err
}
