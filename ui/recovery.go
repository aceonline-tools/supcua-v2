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

// Recovery is the renderer-failure policy shared by every WebView2 window of the host. Failed
// handles a ProcessFailed event; Navigated and Ready report the reloaded page's progress. The
// recovery deadline is completed only when the reloaded page navigated successfully AND called
// app.ready, so a reload that is acknowledged but never loads (or fails) expires into Fatal.
// Failed and Navigated run on the UI thread, Ready on the RPC worker.
type Recovery struct {
	Bound    time.Duration
	Arm      func()                // arm the exit watchdog (runs from the deadline timer: the UI thread may be stuck)
	Fatal    func(why string)      // UI thread: quarantine and ordered exit
	Post     func(func())          // run on the UI thread
	Shutting func() bool           // exit already began
	Enqueue  func(job func()) bool // run job on the RPC worker after every call already sent
	Reset    func()                // RPC worker: drop the dead page's session state; may be nil
	Reload   func() error          // UI thread
	Now      func() time.Time      // default time.Now
	Logf     func(format string, args ...any)

	failTimes []time.Time // UI thread only
	cur       *Deadline   // the in-progress recovery's deadline; UI thread only
	fatalOnce sync.Once

	mu     sync.Mutex
	dl     *Deadline
	navOK  bool
	isRead bool
}

func (r *Recovery) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Recovery) logf(f string, a ...any) {
	if r.Logf != nil {
		r.Logf(f, a...)
	}
}

// Failed handles one ProcessFailed event. UI thread, inside a WebView2 event: only arm, post
// and enqueue.
func (r *Recovery) Failed(kind int32) {
	if r.Shutting() {
		return
	}
	action := ProcessFailureAction(kind)
	r.logf("webview: process failed (kind %d, action %d)", kind, action)
	switch action {
	case FailureIgnore:
		return
	case FailureFatal:
		r.fatalNow("process failed")
		return
	}
	now := r.now()
	keep := r.failTimes[:0]
	for _, t := range r.failTimes {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	r.failTimes = append(keep, now)
	if len(r.failTimes) > 3 {
		r.fatalNow("repeated renderer failures")
		return
	}
	if r.cur != nil {
		r.cur.Done() // supersede: the previous attempt's deadline can never fire fatal later
	}
	deadline := StartDeadline(r.Bound, func() { r.fatalNow("renderer recovery did not finish") })
	r.cur = deadline
	queued := r.Enqueue(func() {
		if r.Reset != nil {
			r.Reset()
		}
		r.Post(func() {
			if r.Shutting() {
				deadline.Done()
				return
			}
			r.begin(deadline)
			if err := r.Reload(); err != nil {
				if deadline.Done() {
					r.fatalOnce.Do(func() { r.Fatal("reload: " + err.Error()) })
				}
			}
		})
	})
	if !queued {
		deadline.Done()
		r.logf("webview: renderer failed after the RPC dispatcher closed; not reloading")
	}
}

func (r *Recovery) fatalNow(why string) {
	r.Arm()
	r.Post(func() { r.fatalOnce.Do(func() { r.Fatal(why) }) })
}

func (r *Recovery) begin(dl *Deadline) {
	r.mu.Lock()
	r.dl, r.navOK, r.isRead = dl, false, false
	r.mu.Unlock()
}

// Navigated reports a NavigationCompleted of the window.
func (r *Recovery) Navigated(success bool) {
	if !success {
		return
	}
	r.mu.Lock()
	r.navOK = true
	r.completeLocked()
	r.mu.Unlock()
}

// Ready reports the page's app.ready call.
func (r *Recovery) Ready() {
	r.mu.Lock()
	r.isRead = true
	r.completeLocked()
	r.mu.Unlock()
}

func (r *Recovery) completeLocked() {
	if r.dl != nil && r.navOK && r.isRead {
		r.dl.Done()
		r.dl = nil
	}
}
