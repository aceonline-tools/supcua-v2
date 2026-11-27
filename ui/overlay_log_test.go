package ui

import (
	"strings"
	"testing"
	"time"
)

func newTestLog() (*overlayLogger, *[]string, *time.Time) {
	var lines []string
	now := time.Unix(1000, 0)
	return newOverlayLogger(func(s string) { lines = append(lines, s) }, func() time.Time { return now }), &lines, &now
}

func TestStatusOneLinePerChange(t *testing.T) {
	l, lines, _ := newTestLog()
	green, gray := [3]uint8{0, 255, 0}, [3]uint8{128, 128, 128}
	l.Status("IDLE", gray, true)
	l.Status("IDLE", gray, true)
	l.Status("RUN: farm", green, true)
	l.Status("RUN: farm", green, true)
	l.Status("IDLE", gray, true)
	want := []string{
		"overlay: status (none) → IDLE colour #808080 (global=true)",
		"overlay: status IDLE → RUN: farm colour #00FF00 (global=true)",
		"overlay: status RUN: farm → IDLE colour #808080 (global=true)",
	}
	if strings.Join(*lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q", *lines)
	}
}

func TestStatusColourOrGlobalChangeIsLogged(t *testing.T) {
	l, lines, _ := newTestLog()
	l.Status("IDLE", [3]uint8{1, 2, 3}, true)
	l.Status("IDLE", [3]uint8{4, 5, 6}, true)
	l.Status("IDLE", [3]uint8{4, 5, 6}, false)
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
	l.Status("DISABLE", [3]uint8{255, 0, 0}, false)
	l.Failure("a", "x %d", 1)
	for _, s := range *lines {
		if !strings.HasPrefix(s, "overlay: ") {
			t.Fatalf("missing prefix: %q", s)
		}
	}
}
