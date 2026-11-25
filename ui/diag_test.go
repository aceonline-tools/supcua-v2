package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) Logf(f string, a ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(f, a...))
	l.mu.Unlock()
}

func (l *logSink) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func call(t *testing.T, d *Diag, m, p string) error {
	t.Helper()
	_, err := d.Methods()[m](json.RawMessage(p))
	return err
}

func TestDiagMountedValidatesAndFiresOnce(t *testing.T) {
	var s logSink
	var got []string
	d := &Diag{Logf: s.Logf, OnMounted: func(r string) { got = append(got, r) }}
	for _, bad := range []string{`{}x`, `[]`, `{"route":1}`, `{"route":"/","x":1}`, `{"route":"` + strings.Repeat("a", 201) + `"}`} {
		if call(t, d, "app.mounted", bad) == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if len(got) != 0 {
		t.Fatalf("mount hook ran on invalid input: %v", got)
	}
	for i := 0; i < 3; i++ {
		if err := call(t, d, "app.mounted", `{"route":"/"}`); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 1 || got[0] != "/" {
		t.Fatalf("OnMounted calls = %v", got)
	}
}

func TestDiagLogValidatesBoundsAndSanitises(t *testing.T) {
	var s logSink
	d := &Diag{Logf: s.Logf}
	for _, bad := range []string{`{}`, `{"level":"fatal","message":"x"}`, `{"level":"error"}`, `{"level":"error","message":""}`, `{"level":"error","message":1}`, `{"level":"error","message":"x","extra":1}`, `null`} {
		if call(t, d, "app.log", bad) == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if err := call(t, d, "app.log", `{"level":"error","message":"boom\nforged: line `+strings.Repeat("é", 400)+`"}`); err != nil {
		t.Fatal(err)
	}
	out := s.all()
	if strings.Contains(out, "\nforged") || !strings.Contains(out, "frontend[error]: boom forged: line") || !strings.HasSuffix(out, "...") {
		t.Fatalf("log line not sanitised/bounded: %q", out)
	}
	if len(out) > maxDiagMessage+40 {
		t.Fatalf("line too long: %d", len(out))
	}
}

func TestDiagRejectsOversizedParamsBeforeDecoding(t *testing.T) {
	d := &Diag{Logf: (&logSink{}).Logf}
	if call(t, d, "app.log", `{"level":"error","message":"`+strings.Repeat("x", maxDiagParams)+`"}`) == nil {
		t.Fatal("oversized params accepted")
	}
}

func TestClipIsRuneSafeAndBounded(t *testing.T) {
	got := clip(strings.Repeat("é", 1000), 501)
	if !utf8.ValidString(got) || len(got) > 501+3 || !strings.HasSuffix(got, "...") {
		t.Fatalf("len=%d valid=%v", len(got), utf8.ValidString(got))
	}
}

func TestDiagLogIsRateBounded(t *testing.T) {
	var s logSink
	d := &Diag{Logf: s.Logf}
	for i := 0; i < maxDiagLogs+20; i++ {
		_ = call(t, d, "app.log", `{"level":"warn","message":"m"}`)
	}
	if n := len(s.lines); n != maxDiagLogs+1 || !strings.Contains(s.lines[n-1], "suppressed") {
		t.Fatalf("%d lines, last %q", n, s.lines[len(s.lines)-1])
	}
}

func TestLoadWatchFiresWithStageAndIsDisarmedByMount(t *testing.T) {
	var s logSink
	st := NewStages(s.Logf)
	st.Mark("tray done")
	fired := make(chan string, 1)
	w := &LoadWatch{Timeout: 20 * time.Millisecond, Stage: st.Last, Logf: s.Logf, OnTimeout: func(stage string) { fired <- stage }}
	w.Start()
	select {
	case stage := <-fired:
		if stage != "tray done" || !strings.Contains(s.all(), "frontend did not load within 20ms; last stage reached: tray done") {
			t.Fatalf("stage=%q log=%q", stage, s.all())
		}
	case <-time.After(time.Second):
		t.Fatal("deadline did not fire")
	}
	var lateAfter time.Duration
	w.OnLate = func(d time.Duration) { lateAfter = d }
	if !w.Mounted() || lateAfter <= 0 || !strings.Contains(s.all(), "frontend mounted late after") {
		t.Fatalf("late mount not reported: %v %q", lateAfter, s.all())
	}

	w2 := &LoadWatch{Timeout: 30 * time.Millisecond, Stage: st.Last, Logf: s.Logf, OnTimeout: func(string) { t.Error("fired after mount") }}
	w2.Start()
	if w2.Mounted() {
		t.Fatal("on-time mount reported late")
	}
	time.Sleep(80 * time.Millisecond)
}

func TestMountRacingTheDeadlineIsVisibleBeforeAnyWarningIsShown(t *testing.T) {
	var s logSink
	shown := false
	var w *LoadWatch
	w = &LoadWatch{Timeout: 10 * time.Millisecond, Stage: func() string { return "x" }, Logf: s.Logf,
		OnTimeout: func(string) {
			w.Mounted() // the page mounts after the deadline fired but before the box is created
			if !w.IsMounted() {
				t.Error("mount not visible")
			}
			if !w.IsMounted() { // what main does right before ShowLoadTimeout
				shown = true
			}
		}}
	w.Start()
	time.Sleep(60 * time.Millisecond)
	if shown || !w.IsMounted() {
		t.Fatal("a warning would have been shown after the mount")
	}
}

func TestStagesLogOneLinePerStage(t *testing.T) {
	var s logSink
	st := NewStages(s.Logf)
	st.Mark("a")
	st.Mark("b")
	if st.Last() != "b" || len(s.lines) != 2 || !strings.HasPrefix(s.lines[0], "stage: a (+") {
		t.Fatalf("%q last=%q", s.lines, st.Last())
	}
}
