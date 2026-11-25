package hotkey

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// listener fakes ListenForHotkeys: it executes queued requests through serve, like the real one.
func listener(ch chan *regRequest, id int, err error, executed *atomic.Int32, released chan<- int) {
	go func() {
		for req := range ch {
			req.serve(func(model.Hotkey, HotkeyCallback) regReply {
				executed.Add(1)
				return regReply{id, err}
			}, func(id int) { released <- id })
		}
	}()
}

func TestRegisterBoundedAnswersWithinDeadline(t *testing.T) {
	ch := make(chan *regRequest, 1)
	var n atomic.Int32
	listener(ch, 7, nil, &n, nil)
	if id, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, time.Second); id != 7 || err != nil {
		t.Fatalf("got %d, %v", id, err)
	}
}

func TestRegisterBoundedPropagatesListenerError(t *testing.T) {
	ch := make(chan *regRequest, 1)
	var n atomic.Int32
	want := errors.New("conflict")
	listener(ch, 0, want, &n, nil)
	if _, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, time.Second); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

// A request whose caller timed out before the listener started it is never executed.
func TestAbandonedRequestIsNotExecuted(t *testing.T) {
	ch := make(chan *regRequest, 1)
	var n atomic.Int32
	if _, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, 20*time.Millisecond); !errors.Is(err, ErrRegisterTimeout) {
		t.Fatalf("err = %v", err)
	}
	listener(ch, 9, nil, &n, nil) // the listener finally runs and drains the queue
	time.Sleep(100 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatalf("cancelled request was executed %d times", n.Load())
	}
}

// Register returns within its deadline even while the listener is stuck executing, and a
// registration that completes after the caller left is released by the listener itself.
func TestCallerReturnsWithinDeadlineWhileExecutionIsBlockedAndLateSuccessIsReleased(t *testing.T) {
	ch := make(chan *regRequest, 1)
	started, unblock := make(chan struct{}), make(chan struct{})
	released := make(chan int, 1)
	served := make(chan struct{})
	go func() {
		req := <-ch
		req.serve(func(model.Hotkey, HotkeyCallback) regReply {
			close(started)
			<-unblock
			return regReply{11, nil}
		}, func(id int) { released <- id })
		close(served)
	}()
	t0 := time.Now()
	_, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, 40*time.Millisecond)
	<-started
	if !errors.Is(err, ErrRegisterTimeout) || time.Since(t0) > 150*time.Millisecond {
		t.Fatalf("err=%v after %v with execution blocked", err, time.Since(t0))
	}
	close(unblock)
	<-served
	select {
	case id := <-released:
		if id != 11 {
			t.Fatalf("released %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("late success was not released by the listener")
	}
}

func TestLateFailureAfterAbandonmentReleasesNothing(t *testing.T) {
	called := false
	// the caller abandons while the listener is inside exec, and exec then fails
	r2 := newRegRequest(model.Hotkey{Key: "f5"}, nil)
	r2.serve(func(model.Hotkey, HotkeyCallback) regReply {
		r2.state.Store(reqAbandoned)
		return regReply{0, errors.New("conflict")}
	}, func(int) { called = true })
	if called {
		t.Fatal("released a failed registration")
	}
}

func TestRegisterBoundedTimesOutWhenListenerNeverStarted(t *testing.T) {
	ch := make(chan *regRequest) // unbuffered, nobody receiving: the send itself is bounded
	if _, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, 30*time.Millisecond); !errors.Is(err, ErrRegisterTimeout) {
		t.Fatalf("err = %v", err)
	}
}
