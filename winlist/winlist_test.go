package winlist

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fw struct {
	Window
	title string
}

type fakeSrc struct {
	wins                         []fw
	exes                         map[uint32]string
	now                          *time.Time
	slowTitle, slowExe, slowWalk map[int]time.Duration
	exeCalls                     int
}

func (f *fakeSrc) tick(d time.Duration) {
	if f.now != nil {
		*f.now = f.now.Add(d)
	}
}

func (f *fakeSrc) Windows(visit func(Window) bool, _ func() bool) {
	for i, w := range f.wins {
		f.tick(f.slowWalk[i])
		w.H = uintptr(i + 1)
		if !visit(w.Window) {
			return
		}
	}
}

func (f *fakeSrc) Title(h uintptr) string {
	f.tick(f.slowTitle[int(h)-1])
	return f.wins[h-1].title
}

func (f *fakeSrc) ExeName(pid uint32, _ func() bool) (string, error) {
	f.exeCalls++
	f.tick(f.slowExe[f.exeCalls-1])
	if n, ok := f.exes[pid]; ok {
		return n, nil
	}
	return "", errors.New("gone")
}

func vis(pid uint32, title string) fw { return fw{Window{PID: pid, Visible: true}, title} }
func raw(w Window, title string) fw   { return fw{w, title} }

func TestFilters(t *testing.T) {
	src := &fakeSrc{
		wins: []fw{
			vis(1, "ok"),
			raw(Window{PID: 1}, "hidden"),
			raw(Window{PID: 1, Visible: true, ToolWindow: true}, "tool"),
			raw(Window{PID: 1, Visible: true, Cloaked: true}, "cloaked"),
			raw(Window{PID: 1, Visible: true, Owned: true}, "owned"),
			raw(Window{PID: 1, Visible: true, Owned: true, AppWindow: true}, "owned-app"),
			vis(1, "   "),
			vis(2, "self"),
			vis(3, "no access"),
		},
		exes: map[uint32]string{1: `C:\x\Notepad.exe`, 2: `C:\me\supcua-v2.exe`},
	}
	got := List(src, 2, Budget, nil)
	want := []Row{{"Notepad.exe", "ok"}, {"Notepad.exe", "owned-app"}}
	if len(got.Rows) != len(want) || got.Rows[0] != want[0] || got.Rows[1] != want[1] || got.Truncated {
		t.Fatalf("got %+v", got)
	}
}

func TestDedupeAndSort(t *testing.T) {
	src := &fakeSrc{
		wins: []fw{vis(5, "b"), vis(4, "b"), vis(4, "a"), vis(6, "z"), vis(7, "B")},
		exes: map[uint32]string{4: `c:\Zed.exe`, 5: `c:\zed.EXE`, 6: `c:\alpha.exe`, 7: `c:\Beta.exe`},
	}
	got := List(src, 0, Budget, nil).Rows
	want := []Row{{"alpha.exe", "z"}, {"Beta.exe", "B"}, {"Zed.exe", "a"}, {"Zed.exe", "b"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestCap(t *testing.T) {
	src := &fakeSrc{exes: map[uint32]string{1: "a.exe"}}
	for i := 0; i < MaxRows+5; i++ {
		src.wins = append(src.wins, vis(1, "t"+string(rune('A'+i%26))+strings.Repeat("x", i)))
	}
	got := List(src, 0, Budget, nil)
	if len(got.Rows) != MaxRows || !got.Truncated {
		t.Fatalf("rows=%d truncated=%v", len(got.Rows), got.Truncated)
	}
	exact := &fakeSrc{exes: src.exes, wins: src.wins[:MaxRows]}
	if r := List(exact, 0, Budget, nil); len(r.Rows) != MaxRows || r.Truncated {
		t.Fatalf("exact cap should not truncate: %d %v", len(r.Rows), r.Truncated)
	}
}

func budgetRun(t *testing.T, mutate func(f *fakeSrc, now *time.Time)) (Result, time.Duration) {
	t.Helper()
	start := time.Unix(1000, 0)
	now := start
	f := &fakeSrc{now: &now, exes: map[uint32]string{1: "a.exe", 2: "b.exe"}, slowTitle: map[int]time.Duration{}, slowExe: map[int]time.Duration{}, slowWalk: map[int]time.Duration{}}
	f.wins = []fw{vis(1, "a"), vis(2, "b"), vis(1, "c")}
	mutate(f, &now)
	res := List(f, 0, Budget, func() time.Time { return now })
	return res, now.Sub(start)
}

func TestBudgetSlowFirstWindow(t *testing.T) {
	res, el := budgetRun(t, func(f *fakeSrc, _ *time.Time) { f.slowTitle[0] = 400 * time.Millisecond })
	if !res.Truncated || el > 400*time.Millisecond || len(res.Rows) != 0 {
		t.Fatalf("%+v elapsed=%v", res, el)
	}
}

func TestBudgetSlowLastWindow(t *testing.T) {
	res, el := budgetRun(t, func(f *fakeSrc, _ *time.Time) { f.slowTitle[2] = 400 * time.Millisecond })
	if !res.Truncated || el > 400*time.Millisecond || len(res.Rows) != 3 {
		t.Fatalf("%+v elapsed=%v", res, el)
	}
}

func TestBudgetSlowExeName(t *testing.T) {
	res, el := budgetRun(t, func(f *fakeSrc, _ *time.Time) { f.slowExe[0] = 300 * time.Millisecond })
	if !res.Truncated || el > 300*time.Millisecond || f1(res) != 1 {
		t.Fatalf("%+v elapsed=%v", res, el)
	}
}

func TestBudgetNoNativeCallAfterDeadline(t *testing.T) {
	_, el := budgetRun(t, func(f *fakeSrc, _ *time.Time) { f.slowWalk[1] = 300 * time.Millisecond })
	if el != 300*time.Millisecond {
		t.Fatalf("elapsed=%v", el)
	}
}

func TestSelfExcludedBeforeTitle(t *testing.T) {
	f := &fakeSrc{wins: []fw{vis(9, "self")}, exes: map[uint32]string{9: "me.exe"}, slowTitle: map[int]time.Duration{}}
	f.slowTitle[0] = time.Hour // would trip the budget if the title were read
	now := time.Unix(0, 0)
	f.now = &now
	res := List(f, 9, Budget, func() time.Time { return now })
	if res.Truncated || len(res.Rows) != 0 {
		t.Fatalf("%+v", res)
	}
}

func f1(r Result) int { return len(r.Rows) }

func TestTitleClip(t *testing.T) {
	src := &fakeSrc{wins: []fw{vis(1, strings.Repeat("é", 500))}, exes: map[uint32]string{1: "a.exe"}}
	if n := len([]rune(List(src, 0, Budget, nil).Rows[0].Title)); n != MaxTitleRunes {
		t.Fatalf("runes=%d", n)
	}
}
