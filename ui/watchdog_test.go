package ui

import (
	"testing"
	"time"
)

func exitRecorder() (func(int), chan int) {
	ch := make(chan int, 4)
	return func(c int) { ch <- c }, ch
}

func TestExitWatchdogForcesExitAfterBound(t *testing.T) {
	exit, ch := exitRecorder()
	w := NewExitWatchdog(exit)
	start := time.Now()
	w.Arm(60*time.Millisecond, 0, "normal exit")
	select {
	case c := <-ch:
		if c != 0 || time.Since(start) < 50*time.Millisecond {
			t.Fatalf("code %d after %v", c, time.Since(start))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog never fired")
	}
}

func TestExitWatchdogFatalTightensNormalNeverLoosens(t *testing.T) {
	exit, ch := exitRecorder()
	w := NewExitWatchdog(exit)
	w.Arm(time.Hour, 0, "normal exit")
	w.Arm(50*time.Millisecond, 5, "fatal")
	w.Arm(time.Hour, 0, "normal exit again") // must not loosen the fatal bound
	select {
	case c := <-ch:
		if c != 5 {
			t.Fatalf("fatal bound must exit 5, got %d", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tightened bound never fired")
	}
	select {
	case c := <-ch:
		t.Fatalf("second exit %d", c)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestExitWatchdogDisarmIsFinal(t *testing.T) {
	exit, ch := exitRecorder()
	w := NewExitWatchdog(exit)
	w.Arm(40*time.Millisecond, 5, "fatal")
	w.Disarm()
	w.Arm(10*time.Millisecond, 0, "late")
	select {
	case c := <-ch:
		t.Fatalf("disarmed watchdog exited %d", c)
	case <-time.After(150 * time.Millisecond):
	}
}
