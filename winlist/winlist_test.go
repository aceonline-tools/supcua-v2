package winlist

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeSrc struct {
	wins []Window
	exes map[uint32]string
}

func (f fakeSrc) Windows(visit func(Window) bool) {
	for _, w := range f.wins {
		if !visit(w) {
			return
		}
	}
}

func (f fakeSrc) ExeName(pid uint32) (string, error) {
	if n, ok := f.exes[pid]; ok {
		return n, nil
	}
	return "", errors.New("gone")
}

func vis(pid uint32, title string) Window { return Window{PID: pid, Title: title, Visible: true} }

func TestFilters(t *testing.T) {
	src := fakeSrc{
		wins: []Window{
			vis(1, "ok"),
			{PID: 1, Title: "hidden"},
			{PID: 1, Title: "tool", Visible: true, ToolWindow: true},
			{PID: 1, Title: "cloaked", Visible: true, Cloaked: true},
			{PID: 1, Title: "owned", Visible: true, Owned: true},
			{PID: 1, Title: "owned-app", Visible: true, Owned: true, AppWindow: true},
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
	src := fakeSrc{
		wins: []Window{vis(5, "b"), vis(4, "b"), vis(4, "a"), vis(6, "z"), vis(7, "B")},
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
	src := fakeSrc{exes: map[uint32]string{1: "a.exe"}}
	for i := 0; i < MaxRows+5; i++ {
		src.wins = append(src.wins, vis(1, "t"+string(rune('A'+i%26))+strings.Repeat("x", i)))
	}
	got := List(src, 0, Budget, nil)
	if len(got.Rows) != MaxRows || !got.Truncated {
		t.Fatalf("rows=%d truncated=%v", len(got.Rows), got.Truncated)
	}
	exact := fakeSrc{exes: src.exes, wins: src.wins[:MaxRows]}
	if r := List(exact, 0, Budget, nil); len(r.Rows) != MaxRows || r.Truncated {
		t.Fatalf("exact cap should not truncate: %d %v", len(r.Rows), r.Truncated)
	}
}

func TestBudget(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { now = now.Add(100 * time.Millisecond); return now }
	src := fakeSrc{wins: []Window{vis(1, "a"), vis(1, "b"), vis(1, "c"), vis(1, "d")}, exes: map[uint32]string{1: "a.exe"}}
	got := List(src, 0, Budget, clock)
	if !got.Truncated || len(got.Rows) == 0 || len(got.Rows) >= 4 {
		t.Fatalf("got %+v", got)
	}
}

func TestTitleClip(t *testing.T) {
	src := fakeSrc{wins: []Window{vis(1, strings.Repeat("é", 500))}, exes: map[uint32]string{1: "a.exe"}}
	if n := len([]rune(List(src, 0, Budget, nil).Rows[0].Title)); n != MaxTitleRunes {
		t.Fatalf("runes=%d", n)
	}
}
