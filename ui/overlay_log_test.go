package ui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestLog() (*overlayLogger, *[]string, *time.Time) {
	var lines []string
	now := time.Unix(1000, 0)
	return newOverlayLogger(func(s string) { lines = append(lines, s) }, func() time.Time { return now }), &lines, &now
}

func TestShownOneLinePerChange(t *testing.T) {
	l, lines, _ := newTestLog()
	green, gray := [3]uint8{0, 255, 0}, [3]uint8{128, 128, 128}
	l.Shown("IDLE", gray, true)
	l.Shown("IDLE", gray, true)
	l.Shown("RUN: farm", green, true)
	l.Shown("RUN: farm", green, true)
	l.Shown("IDLE", gray, true)
	want := []string{
		`overlay: shown "(none)" → "IDLE" colour #808080 (global=true)`,
		`overlay: shown "IDLE" → "RUN: farm" colour #00FF00 (global=true)`,
		`overlay: shown "RUN: farm" → "IDLE" colour #808080 (global=true)`,
	}
	if strings.Join(*lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q", *lines)
	}
}

func TestShownColourOrGlobalChangeIsLogged(t *testing.T) {
	l, lines, _ := newTestLog()
	l.Shown("IDLE", [3]uint8{1, 2, 3}, true)
	l.Shown("IDLE", [3]uint8{4, 5, 6}, true)
	l.Shown("IDLE", [3]uint8{4, 5, 6}, false)
	if len(*lines) != 3 {
		t.Fatalf("got %d lines %q", len(*lines), *lines)
	}
}

func TestFailureRateLimitedPerKindWithSummary(t *testing.T) {
	l, lines, now := newTestLog()
	for i := 0; i < 5; i++ {
		l.Failure("render", "render failed: boom")
	}
	l.Failure("other", "other failed")
	if len(*lines) != 2 {
		t.Fatalf("want 2 lines (one per kind), got %q", *lines)
	}
	*now = now.Add(overlayLogEvery)
	l.Failure("render", "render failed: boom")
	last := (*lines)[len(*lines)-1]
	if last != "overlay: render failed: boom (4 similar suppressed)" {
		t.Fatalf("got %q", last)
	}
	*now = now.Add(overlayLogEvery)
	l.Failure("render", "render failed: boom")
	if got := (*lines)[len(*lines)-1]; strings.Contains(got, "suppressed") {
		t.Fatalf("summary must reset: %q", got)
	}
}

func TestEveryLineHasOverlayPrefix(t *testing.T) {
	l, lines, _ := newTestLog()
	l.Shown("DISABLE", [3]uint8{255, 0, 0}, false)
	l.State(true, "x")
	l.Failure("a", "x %d", 1)
	for _, s := range *lines {
		if !strings.HasPrefix(s, "overlay: ") {
			t.Fatalf("missing prefix: %q", s)
		}
	}
}

func TestStateLinesDedupedAndRapidTransitionShowsBothStatesButOneShown(t *testing.T) {
	l, lines, _ := newTestLog()
	l.State(true, "")
	l.State(true, "")
	l.State(true, "farm")
	l.State(true, "")
	l.State(false, "")
	want := []string{
		`overlay: state "(none)" → "IDLE" (script="", global=true)`,
		`overlay: state "IDLE" → "RUN: farm" (script="farm", global=true)`,
		`overlay: state "RUN: farm" → "IDLE" (script="", global=true)`,
		`overlay: state "IDLE" → "DISABLE" (script="", global=false)`,
	}
	if strings.Join(*lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q", *lines)
	}
	// the UI only got to draw the final IDLE: published RUN was never shown
	*lines = nil
	l.Shown("IDLE", [3]uint8{128, 128, 128}, true)
	if len(*lines) != 1 {
		t.Fatalf("want one shown line, got %q", *lines)
	}
}

func TestUserTextCannotForgeOrSplitLines(t *testing.T) {
	l, lines, _ := newTestLog()
	l.State(true, "a\nb\noverlay: state forged")
	l.Shown("RUN: a\nfoo", [3]uint8{}, true)
	l.Failure("k", "bad %q and err %v", "x\ny", fmt.Errorf("e1\ne2"))
	for _, s := range *lines {
		if strings.ContainsAny(s, "\r\n") || !strings.HasPrefix(s, "overlay: ") {
			t.Fatalf("unsafe line %q", s)
		}
	}
	if len(*lines) != 3 {
		t.Fatalf("got %q", *lines)
	}
}

func TestTapMayCallBackIntoLogger(t *testing.T) {
	l, _, _ := newTestLog()
	done := make(chan struct{})
	var once atomic.Bool
	l.Tap(func(string) {
		if once.CompareAndSwap(false, true) {
			l.Failure("inner", "from tap")
			close(done)
		}
	})
	go l.Shown("IDLE", [3]uint8{}, true)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tap deadlocked on the logger lock")
	}
}

func collectSink(size int, gate <-chan struct{}) (*asyncSink, func() []string, chan struct{}) {
	var mu sync.Mutex
	var got []string
	started := make(chan struct{}, 1)
	a := newAsyncSink(size, func(s string) {
		select {
		case started <- struct{}{}:
		default:
		}
		if gate != nil {
			<-gate
		}
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	})
	return a, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), got...) }, started
}

func waitLines(t *testing.T, get func() []string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if g := get(); len(g) >= n {
			return g
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d lines, got %q", n, get())
	return nil
}

func TestAsyncSinkPreservesOrder(t *testing.T) {
	a, get, _ := collectSink(64, nil)
	for i := 0; i < 50; i++ {
		a.enqueue(fmt.Sprintf("overlay: %d", i))
	}
	got := waitLines(t, get, 50)
	for i, l := range got {
		if l != fmt.Sprintf("overlay: %d", i) {
			t.Fatalf("out of order at %d: %q", i, l)
		}
	}
}

func TestAsyncSinkBlockedWriterDoesNotBlockProducer(t *testing.T) {
	gate := make(chan struct{})
	a, _, _ := collectSink(4, gate)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			a.enqueue("overlay: x")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("producer blocked on a stuck writer")
	}
	close(gate)
}

func TestAsyncSinkFullBufferReportsDropCount(t *testing.T) {
	gate := make(chan struct{})
	a, get, started := collectSink(4, gate)
	a.enqueue("overlay: first")
	<-started // writer is now stuck inside the first line; the buffer is empty
	for i := 0; i < 4; i++ {
		a.enqueue(fmt.Sprintf("overlay: buffered %d", i))
	}
	for i := 0; i < 3; i++ {
		a.enqueue("overlay: lost")
	}
	close(gate)
	got := waitLines(t, get, 6)
	want := []string{"overlay: first", "overlay: 3 log lines dropped (log writer busy)", "overlay: buffered 0", "overlay: buffered 1", "overlay: buffered 2", "overlay: buffered 3"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}
