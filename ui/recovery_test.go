package ui

import (
	"errors"
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

type recFixture struct {
	rec     *Recovery
	fatal   chan string
	armed   chan struct{}
	reloads int
	reloadE error
	posts   []func()
}

func newRecFixture(bound time.Duration) *recFixture {
	f := &recFixture{fatal: make(chan string, 4), armed: make(chan struct{}, 4)}
	f.rec = &Recovery{
		Bound:    bound,
		Arm:      func() { f.armed <- struct{}{} },
		Fatal:    func(why string) { f.fatal <- why },
		Post:     func(fn func()) { fn() },
		Shutting: func() bool { return false },
		Enqueue:  func(job func()) bool { job(); return true },
		Reload:   func() error { f.reloads++; return f.reloadE },
	}
	return f
}

func (f *recFixture) expectFatal(t *testing.T, want bool) {
	t.Helper()
	select {
	case why := <-f.fatal:
		if !want {
			t.Fatalf("unexpected fatal: %s", why)
		}
	case <-time.After(300 * time.Millisecond):
		if want {
			t.Fatal("deadline did not expire into fatal")
		}
	}
}

func TestRecoveryReloadOKButNeverLoadsExpiresIntoFatal(t *testing.T) {
	f := newRecFixture(50 * time.Millisecond)
	f.rec.Failed(KindRenderProcessExited)
	if f.reloads != 1 {
		t.Fatalf("reloads = %d", f.reloads)
	}
	f.expectFatal(t, true)
}

func TestRecoveryFailedNavigationDoesNotComplete(t *testing.T) {
	f := newRecFixture(50 * time.Millisecond)
	f.rec.Failed(KindRenderProcessExited)
	f.rec.Navigated(false)
	f.rec.Ready()
	f.expectFatal(t, true)
}

func TestRecoveryNavigationWithoutReadyDoesNotComplete(t *testing.T) {
	f := newRecFixture(50 * time.Millisecond)
	f.rec.Failed(KindRenderProcessExited)
	f.rec.Navigated(true)
	f.expectFatal(t, true)
}

func TestRecoverySuccessfulLoadAndReadyCompletes(t *testing.T) {
	for _, readyFirst := range []bool{false, true} {
		f := newRecFixture(80 * time.Millisecond)
		f.rec.Failed(KindRenderProcessExited)
		if readyFirst {
			f.rec.Ready()
			f.rec.Navigated(true)
		} else {
			f.rec.Navigated(true)
			f.rec.Ready()
		}
		f.expectFatal(t, false)
	}
}

func TestRecoveryReloadErrorIsFatalAtOnce(t *testing.T) {
	f := newRecFixture(time.Minute)
	f.reloadE = errors.New("closed")
	f.rec.Failed(KindRenderProcessExited)
	f.expectFatal(t, true)
}

func TestRecoveryBrowserExitAndRepeatedFailuresAreFatal(t *testing.T) {
	f := newRecFixture(time.Minute)
	f.rec.Failed(KindBrowserProcessExited)
	f.expectFatal(t, true)
	f = newRecFixture(time.Minute)
	for i := 0; i < 4; i++ {
		f.rec.Failed(KindRenderProcessExited)
	}
	f.expectFatal(t, true)
	f = newRecFixture(time.Minute)
	f.rec.Failed(KindUtilityIgnored)
	f.expectFatal(t, false)
}

const KindUtilityIgnored int32 = 4
