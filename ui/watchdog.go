package ui

import (
	"log"
	"sync"
	"time"
)

// ExitWatchdog ends the process if an exit path does not finish in time: a stuck lock in a
// shutdown step can never leave the app alive with a hidden window. Safe from any goroutine.
type ExitWatchdog struct {
	mu    sync.Mutex
	timer *time.Timer
	code  int
	exit  func(int)

	armedUntil time.Time
}

// NewExitWatchdog uses exit (os.Exit in production) when an armed bound expires.
func NewExitWatchdog(exit func(int)) *ExitWatchdog { return &ExitWatchdog{exit: exit} }

// Arm starts the bound. A later Arm only ever tightens it: the earliest deadline wins and the
// exit code of the arm that set it is used (a fatal 5 s/5 overrides a normal 10 s/0, never
// the reverse). Arm after Disarm is ignored.
func (w *ExitWatchdog) Arm(d time.Duration, code int, why string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.exit == nil {
		return
	}
	deadline := time.Now().Add(d)
	if w.timer != nil {
		if !w.armedUntil.After(deadline) {
			return
		}
		w.timer.Stop()
	}
	w.code = code
	w.timer = time.AfterFunc(d, func() {
		w.mu.Lock()
		exit, c := w.exit, w.code
		w.mu.Unlock()
		if exit == nil {
			return
		}
		log.Printf("exit watchdog: %s did not finish within %v; forcing exit %d", why, d, c)
		exit(c)
	})
	w.armedUntil = deadline
}

// Disarm stops the watchdog for good (the exit path finished its bounded work, e.g. before a
// user-facing MessageBox).
func (w *ExitWatchdog) Disarm() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.exit = nil
}
