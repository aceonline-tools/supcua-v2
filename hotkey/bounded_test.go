package hotkey

import (
	"errors"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

func serve(ch chan regRequest, delay time.Duration, id int, err error) {
	go func() {
		for req := range ch {
			time.Sleep(delay)
			req.reply <- regReply{id, err}
		}
	}()
}

func TestRegisterBoundedAnswersWithinDeadline(t *testing.T) {
	ch := make(chan regRequest, 1)
	serve(ch, 0, 7, nil)
	id, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, time.Second, nil)
	if id != 7 || err != nil {
		t.Fatalf("got %d, %v", id, err)
	}
}

func TestRegisterBoundedPropagatesListenerError(t *testing.T) {
	ch := make(chan regRequest, 1)
	want := errors.New("conflict")
	serve(ch, 0, 0, want)
	if _, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, time.Second, nil); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestRegisterBoundedTimesOutAndReleasesLateRegistration(t *testing.T) {
	ch := make(chan regRequest, 1)
	serve(ch, 150*time.Millisecond, 9, nil)
	released := make(chan int, 1)
	t0 := time.Now()
	_, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, 30*time.Millisecond, func(id int) { released <- id })
	if !errors.Is(err, ErrRegisterTimeout) || time.Since(t0) > 120*time.Millisecond {
		t.Fatalf("err=%v after %v", err, time.Since(t0))
	}
	select {
	case id := <-released:
		if id != 9 {
			t.Fatalf("released %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("late registration was not released")
	}
}

func TestRegisterBoundedTimesOutWhenListenerNeverStarted(t *testing.T) {
	ch := make(chan regRequest) // unbuffered, nobody receiving: the send itself must be bounded
	_, err := registerBounded(ch, model.Hotkey{Key: "f5"}, nil, 30*time.Millisecond, nil)
	if !errors.Is(err, ErrRegisterTimeout) {
		t.Fatalf("err = %v", err)
	}
}
