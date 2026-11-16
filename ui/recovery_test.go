package ui

import (
	"testing"
	"time"
)

func TestProcessFailureAction(t *testing.T) {
	for kind, want := range map[int32]FailureAction{
		0:  FailureFatal,   // BROWSER_PROCESS_EXITED
		1:  FailureRecover, // RENDER_PROCESS_EXITED
		2:  FailureFatal,   // RENDER_PROCESS_UNRESPONSIVE
		3:  FailureIgnore,  // FRAME_RENDER_PROCESS_EXITED
		4:  FailureIgnore,  // UTILITY_PROCESS_EXITED
		5:  FailureIgnore,  // SANDBOX_HELPER_PROCESS_EXITED
		6:  FailureIgnore,  // GPU_PROCESS_EXITED
		7:  FailureIgnore,  // PPAPI_PLUGIN_PROCESS_EXITED
		8:  FailureIgnore,  // PPAPI_BROKER_PROCESS_EXITED
		9:  FailureIgnore,  // UNKNOWN_PROCESS_EXITED
		-1: FailureIgnore,
		99: FailureIgnore,
	} {
		if got := ProcessFailureAction(kind); got != want {
			t.Errorf("kind %d: got %d want %d", kind, got, want)
		}
	}
}

func TestDeadlineDoneInTimeSuppressesExpiry(t *testing.T) {
	fired := make(chan struct{}, 1)
	dl := StartDeadline(50*time.Millisecond, func() { fired <- struct{}{} })
	if !dl.Done() {
		t.Fatal("Done in time must win")
	}
	select {
	case <-fired:
		t.Fatal("expiry fired after Done")
	case <-time.After(120 * time.Millisecond):
	}
}

func TestDeadlineExpiresWhenWorkIsStuck(t *testing.T) {
	fired := make(chan struct{}, 2)
	dl := StartDeadline(30*time.Millisecond, func() { fired <- struct{}{} })
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("expiry never fired")
	}
	if dl.Done() {
		t.Fatal("late Done must report it lost (the fatal path is already taken)")
	}
	if len(fired) != 0 {
		t.Fatal("expiry fired twice")
	}
}
