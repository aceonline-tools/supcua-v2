package hotkey

import (
	"errors"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// RegisterTimeout bounds one Register call. The listener normally answers in microseconds; a
// wait longer than this means its thread is stuck, and callers (startup, the UI thread) must
// not hang with it.
const RegisterTimeout = 3 * time.Second

// ErrRegisterTimeout is returned when the listener did not answer a registration in time.
var ErrRegisterTimeout = errors.New("hotkey registration timed out (listener not responding)")

type regRequest struct {
	hotkey   model.Hotkey
	callback HotkeyCallback
	reply    chan regReply
}

type regReply struct {
	id  int
	err error
}

// registerBounded sends a registration to the listener's channel and waits at most d for the
// answer. A request the listener answers after the deadline is released through late, so a
// timed-out registration can never leave an untracked OS hotkey behind.
func registerBounded(ch chan<- regRequest, hk model.Hotkey, cb HotkeyCallback, d time.Duration, late func(id int)) (int, error) {
	reply := make(chan regReply, 1)
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case ch <- regRequest{hotkey: hk, callback: cb, reply: reply}:
	case <-timer.C:
		return 0, ErrRegisterTimeout
	}
	select {
	case r := <-reply:
		return r.id, r.err
	case <-timer.C:
		go func() {
			if r := <-reply; r.err == nil && r.id != 0 && late != nil {
				late(r.id)
			}
		}()
		return 0, ErrRegisterTimeout
	}
}
