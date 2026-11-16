package ui

import (
	"sync"
	"time"
)

// FailureAction is the host's answer to a WebView2 ProcessFailed event.
type FailureAction int

const (
	FailureIgnore  FailureAction = iota // auxiliary process; WebView2 recovers it itself: log only
	FailureRecover                      // the renderer is gone: FIFO reset job, then host Reload
	FailureFatal                        // ordered exit with the startup-failure message
)

// COREWEBVIEW2_PROCESS_FAILED_KIND values.
const (
	KindBrowserProcessExited      int32 = 0
	KindRenderProcessExited       int32 = 1
	KindRenderProcessUnresponsive int32 = 2
)

// ProcessFailureAction maps a COREWEBVIEW2_PROCESS_FAILED_KIND to the host's action. Only a
// render-process exit is recoverable: its renderer can send nothing more, so the reset job
// fences all of its calls. An unresponsive renderer is still alive and may yet send calls
// after any reset, so it is fatal, as is a browser-process exit. Every other kind (frame
// renderer, utility, GPU, sandbox helper, plugin, unknown) is recovered by WebView2 itself.
func ProcessFailureAction(kind int32) FailureAction {
	switch kind {
	case KindRenderProcessExited:
		return FailureRecover
	case KindBrowserProcessExited, KindRenderProcessUnresponsive:
		return FailureFatal
	default:
		return FailureIgnore
	}
}

// Deadline runs onExpire once if Done has not been called within d. Done reports whether it
// won the race (false: onExpire has fired or will fire). Safe from any goroutine.
type Deadline struct {
	once  sync.Once
	won   bool
	timer *time.Timer
}

// StartDeadline arms a Deadline.
func StartDeadline(d time.Duration, onExpire func()) *Deadline {
	dl := &Deadline{}
	dl.timer = time.AfterFunc(d, func() { dl.once.Do(onExpire) })
	return dl
}

// Done marks the guarded work complete.
func (dl *Deadline) Done() bool {
	dl.once.Do(func() { dl.won = true; dl.timer.Stop() })
	return dl.won
}
