package ui

import (
	"fmt"
	"log"
	"sync"
	"time"
)

const overlayLogEvery = 10 * time.Second

type overlayErrKind struct {
	last       time.Time
	suppressed int
}

// overlayLogger writes every overlay diagnostic with the "overlay:" prefix: one line per status
// change, and rate-limited (1 per overlayLogEvery per kind) failure lines.
type overlayLogger struct {
	mu         sync.Mutex
	out        func(string)
	now        func() time.Time
	lastStatus string
	lastKey    string
	kinds      map[string]*overlayErrKind
}

func newOverlayLogger(out func(string), now func() time.Time) *overlayLogger {
	return &overlayLogger{out: out, now: now, kinds: map[string]*overlayErrKind{}}
}

var overlayLog = newOverlayLogger(func(s string) { log.Print(s) }, time.Now)

// Status logs a change of the displayed status; an identical text/colour/global is not logged.
func (l *overlayLogger) Status(text string, rgb [3]uint8, global bool) {
	key := fmt.Sprintf("%s|%v|%t", text, rgb, global)
	l.mu.Lock()
	defer l.mu.Unlock()
	if key == l.lastKey {
		return
	}
	old := l.lastStatus
	if old == "" {
		old = "(none)"
	}
	l.lastKey, l.lastStatus = key, text
	l.out(fmt.Sprintf("overlay: status %s → %s colour #%02X%02X%02X (global=%t)", old, text, rgb[0], rgb[1], rgb[2], global))
}

// Failure logs a swallowed or reported failure, at most once per overlayLogEvery per kind; the
// next line after a quiet period carries the number of suppressed repeats.
func (l *overlayLogger) Failure(kind, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := l.kinds[kind]
	now := l.now()
	if k == nil {
		k = &overlayErrKind{}
		l.kinds[kind] = k
	} else if now.Sub(k.last) < overlayLogEvery {
		k.suppressed++
		return
	}
	msg := fmt.Sprintf("overlay: "+format, args...)
	if k.suppressed > 0 {
		msg += fmt.Sprintf(" (%d similar suppressed)", k.suppressed)
	}
	k.last, k.suppressed = now, 0
	l.out(msg)
}

// LogOverlayDropped records a status refresh that could not be posted to the UI thread.
func LogOverlayDropped(reason string) {
	overlayLog.Failure("refresh-dropped", "status refresh dropped: %s", reason)
}

// Tap makes every line also reach f (self-test).
func (l *overlayLogger) Tap(f func(string)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	prev := l.out
	l.out = func(s string) { prev(s); f(s) }
}
