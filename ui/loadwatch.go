package ui

import (
	"sync"
	"time"
)

// LoadWatch is the initial-load deadline: Start arms it when the window is shown, Mounted
// (the frontend's app.mounted) disarms it. If it fires, OnTimeout gets the last stage reached
// so the failure is visible and attributable instead of a silent blank window. It runs on its
// own timer goroutine, so it works even when the UI thread is stalled. It never exits the
// process: a slow first WebView2 profile creation may still complete.
type LoadWatch struct {
	Timeout   time.Duration
	Stage     func() string
	OnTimeout func(stage string)
	// OnLate runs when the frontend mounts after the deadline fired (retract any visible warning).
	OnLate func(elapsed time.Duration)
	Logf   func(format string, args ...any)

	mu      sync.Mutex
	timer   *time.Timer
	started time.Time
	mounted bool
	fired   bool
}

func (w *LoadWatch) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil || w.mounted {
		return
	}
	w.started = time.Now()
	w.timer = time.AfterFunc(w.Timeout, w.fire)
}

func (w *LoadWatch) fire() {
	w.mu.Lock()
	if w.mounted || w.fired {
		w.mu.Unlock()
		return
	}
	w.fired = true
	w.mu.Unlock()
	stage := w.Stage()
	w.Logf("frontend did not load within %s; last stage reached: %s", w.Timeout, stage)
	if w.OnTimeout != nil {
		w.OnTimeout(stage)
	}
}

// IsMounted reports whether the frontend already mounted. OnTimeout must check it immediately
// before showing anything, so a mount that raced the deadline never produces a warning.
func (w *LoadWatch) IsMounted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.mounted
}

// Mounted disarms the deadline and reports whether the frontend arrived after it had fired.
func (w *LoadWatch) Mounted() (late bool) {
	w.mu.Lock()
	w.mounted = true
	if w.timer != nil {
		w.timer.Stop()
	}
	late, elapsed := w.fired, time.Since(w.started)
	w.mu.Unlock()
	if late {
		w.Logf("frontend mounted late after %s", elapsed.Round(100*time.Millisecond))
		if w.OnLate != nil {
			w.OnLate(elapsed)
		}
	}
	return late
}
