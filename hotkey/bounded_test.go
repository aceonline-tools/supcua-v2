package hotkey

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// listener fakes ListenForHotkeys: it executes queued requests through serve, like the real one.
func listener(ch chan *regRequest, delay time.Duration, id int, err error, executed *atomic.Int32) {
	go func() {
		for req := range ch {
			time.Sleep(delay)
			req.serve(func(model.Hotkey, HotkeyCallback) regReply {
				executed.Add(1)
				return regReply{id, err}
			})
		}
	}()
}

func TestRegisterBoundedAnswersWithinDeadline(t *testing.T) {
	ch := make(chan *regRequest, 1)
	var n atomic.Int32
	listener(ch, 0, 7, nil, &n)
	if id, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, time.Second); id != 7 || err != nil {
		t.Fatalf("got %d, %v", id, err)
	}
}

func TestRegisterBoundedPropagatesListenerError(t *testing.T) {
	ch := make(chan *regRequest, 1)
	var n atomic.Int32
	want := errors.New("conflict")
	listener(ch, 0, 0, want, &n)
	if _, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, time.Second); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

// A request whose caller timed out before the listener started it is never executed, so no OS
// registration can be left untracked.
func TestAbandonedRequestIsNotExecuted(t *testing.T) {
	ch := make(chan *regRequest, 1)
	var n atomic.Int32
	if _, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, 20*time.Millisecond); !errors.Is(err, ErrRegisterTimeout) {
		t.Fatalf("err = %v", err)
	}
	listener(ch, 0, 9, nil, &n) // the listener finally runs and drains the queue
	time.Sleep(100 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatalf("abandoned request was executed %d times", n.Load())
	}
}

// A request already executing when the caller's deadline passes is reported as success: the
// caller owns the id, nothing is orphaned.
func TestInFlightRequestAtDeadlineIsReturnedNotOrphaned(t *testing.T) {
	ch := make(chan *regRequest, 1)
	started := make(chan struct{})
	go func() {
		req := <-ch
		req.serve(func(model.Hotkey, HotkeyCallback) regReply {
			close(started)
			time.Sleep(80 * time.Millisecond)
			return regReply{11, nil}
		})
	}()
	id, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, 30*time.Millisecond)
	<-started
	if id != 11 || err != nil {
		t.Fatalf("got %d, %v", id, err)
	}
}

func TestRegisterBoundedTimesOutWhenListenerNeverStarted(t *testing.T) {
	ch := make(chan *regRequest) // unbuffered, nobody receiving: the send itself is bounded
	if _, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, 30*time.Millisecond); !errors.Is(err, ErrRegisterTimeout) {
		t.Fatalf("err = %v", err)
	}
}
