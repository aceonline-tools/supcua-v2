package ui

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const overlayLogEvery = 10 * time.Second

type overlayErrKind struct {
	last       time.Time
	suppressed int
}

// overlayLogger writes every overlay diagnostic with the "overlay:" prefix on a single line:
//   - state: a published status change (from the controller snapshot, deduplicated);
//   - shown: what the overlay actually rendered (deduplicated);
//   - failures: rate-limited (1 per overlayLogEvery per kind) with a suppressed count.
//
// User-derived text (script names) is always quoted, and every line is flattened to one line.
type overlayLogger struct {
	mu        sync.Mutex
	out       func(string)
	taps      []func(string)
	now       func() time.Time
	lastState string
	lastShown string
	shownDesc string
	kinds     map[string]*overlayErrKind
}

func newOverlayLogger(out func(string), now func() time.Time) *overlayLogger {
	return &overlayLogger{out: out, now: now, kinds: map[string]*overlayErrKind{}}
}

const overlayLogBuffer = 256

// overlayLog writes through an async sink so a producer (the status hub subscriber, the UI
// thread) never waits on the log writer.
var overlayLog = newOverlayLogger(newAsyncSink(overlayLogBuffer, func(s string) { log.Print(s) }).enqueue, time.Now)

// asyncSink hands lines to one goroutine that calls write in order. enqueue never blocks: when
// the buffer is full the line is dropped and counted, and the count is reported before the next
// line that gets through.
type asyncSink struct {
	ch      chan string
	dropped atomic.Int64
}

func newAsyncSink(size int, write func(string)) *asyncSink {
	a := &asyncSink{ch: make(chan string, size)}
	go func() {
		for line := range a.ch {
			if n := a.dropped.Swap(0); n > 0 {
				write(fmt.Sprintf("overlay: %d log lines dropped (log writer busy)", n))
			}
			write(line)
		}
	}()
	return a
}

func (a *asyncSink) enqueue(line string) {
	select {
	case a.ch <- line:
	default:
		a.dropped.Add(1)
	}
}

// oneLine keeps a line from being split by control characters.
func oneLine(s string) string {
	if !strings.ContainsAny(s, "\r\n\x00") {
		return s
	}
	return strings.NewReplacer("\r", `\r`, "\n", `\n`, "\x00", `\0`).Replace(s)
}

// emit delivers a line outside the lock (taps may call back into the logger).
func (l *overlayLogger) emit(line string) {
	line = oneLine(line)
	l.mu.Lock()
	out, taps := l.out, append([]func(string){}, l.taps...)
	l.mu.Unlock()
	out(line)
	for _, t := range taps {
		t(line)
	}
}

// statusDesc is the status as published: DISABLE when global is off, RUN: <script>, else IDLE.
func statusDesc(global bool, script string) string {
	switch {
	case !global:
		return "DISABLE"
	case script != "":
		return "RUN: " + script
	}
	return "IDLE"
}

// State logs a published controller change (not yet drawn); an identical state is not logged.
func (l *overlayLogger) State(global bool, script string) {
	desc := statusDesc(global, script)
	key := fmt.Sprintf("%s|%t", desc, global)
	l.mu.Lock()
	if key == l.lastState {
		l.mu.Unlock()
		return
	}
	old := l.lastState
	l.lastState = key
	oldDesc := "(none)"
	if old != "" {
		oldDesc = old[:strings.LastIndexByte(old, '|')]
	}
	l.mu.Unlock()
	l.emit(fmt.Sprintf("overlay: state %s → %s (script=%q, global=%t)", strconv.Quote(oldDesc), strconv.Quote(desc), script, global))
}

// Shown logs what the overlay rendered; an identical text/colour/global is not logged.
func (l *overlayLogger) Shown(text string, rgb [3]uint8, global bool) {
	key := fmt.Sprintf("%s|%v|%t", text, rgb, global)
	l.mu.Lock()
	if key == l.lastShown {
		l.mu.Unlock()
		return
	}
	old := l.shownDesc
	if old == "" {
		old = "(none)"
	}
	l.lastShown, l.shownDesc = key, text
	l.mu.Unlock()
	l.emit(fmt.Sprintf("overlay: shown %s → %s colour #%02X%02X%02X (global=%t)", strconv.Quote(old), strconv.Quote(text), rgb[0], rgb[1], rgb[2], global))
}

// Failure logs a swallowed or reported failure, at most once per overlayLogEvery per kind; the
// next line after a quiet period carries the number of suppressed repeats.
func (l *overlayLogger) Failure(kind, format string, args ...any) {
	l.mu.Lock()
	k := l.kinds[kind]
	now := l.now()
	if k == nil {
		k = &overlayErrKind{}
		l.kinds[kind] = k
	} else if now.Sub(k.last) < overlayLogEvery {
		k.suppressed++
		l.mu.Unlock()
		return
	}
	msg := fmt.Sprintf("overlay: "+format, args...)
	if k.suppressed > 0 {
		msg += fmt.Sprintf(" (%d similar suppressed)", k.suppressed)
	}
	k.last, k.suppressed = now, 0
	l.mu.Unlock()
	l.emit(msg)
}

// Tap makes every line also reach f (self-test).
func (l *overlayLogger) Tap(f func(string)) {
	l.mu.Lock()
	l.taps = append(l.taps, f)
	l.mu.Unlock()
}

// LogOverlayDropped records a status refresh that could not be posted to the UI thread.
func LogOverlayDropped(reason string) {
	overlayLog.Failure("refresh-dropped", "status refresh dropped: %s", reason)
}

// LogOverlayState records a published status change (call from the status subscriber).
func LogOverlayState(global bool, script string) { overlayLog.State(global, script) }

// LogOverlayStartup records a failure to create the overlay.
func LogOverlayStartup(err error) {
	overlayLog.Failure("startup", "failed to create the overlay: %v", err)
}
