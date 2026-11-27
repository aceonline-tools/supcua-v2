package webview

import "testing"

func TestRecoverRectLeavesAReachableWindowAlone(t *testing.T) {
	work := scRect{0, 0, 1920, 1040}
	r := scRect{100, 100, 1000, 800}
	if out, moved := recoverRect(r, work, work); moved || out != r {
		t.Fatalf("%v %v", out, moved)
	}
	// a window mostly off the edge but with a 120x60 corner still on the monitor is reachable
	r = scRect{1800, 980, 2700, 1700}
	if _, moved := recoverRect(r, work, work); moved {
		t.Fatal("120x60 visible is enough")
	}
}

func TestRecoverRectMovesAWindowWhoseMonitorWasRemovedToTheMainMonitor(t *testing.T) {
	removed := scRect{-30000, -30000, -29000, -29300}
	main := scRect{0, 0, 1366, 728}
	nearest := main // the nearest remaining monitor: no overlap with the stale rectangle
	out, moved := recoverRect(removed, nearest, main)
	if !moved || out.L < main.L || out.T < main.T || out.R > main.R || out.B > main.B {
		t.Fatalf("%v %v", out, moved)
	}
	if out.w() != 1000 || out.h() != 700 {
		t.Fatalf("size kept when it fits: %v", out)
	}
	big := scRect{5000, 5000, 7000, 6500}
	out, moved = recoverRect(big, main, main)
	if !moved || out.w() != 1366 || out.h() != 728 {
		t.Fatalf("size clamped to the target work area: %v", out)
	}
}
