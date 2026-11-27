package hotkey

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestPollKeyHeldAcrossResumeStaysQuietUntilReleasedThenRearms(t *testing.T) {
	var p pollState
	p.step(true, false) // RCtrl goes down
	p.resync(true)      // recording cancelled while still held
	if p.step(false, false) {
		t.Fatal("releasing a key that was held when the mute ended must not fire")
	}
	p.step(true, false)
	if !p.step(false, false) {
		t.Fatal("the key must be armed again after its first release")
	}
}

func TestPollKeyReleasedWhileMutedNeverFires(t *testing.T) {
	var p pollState
	p.step(true, true)
	if p.step(false, true) {
		t.Fatal("a release while muted fired")
	}
	p.resync(false)
	p.step(true, false)
	if !p.step(false, false) {
		t.Fatal("a fresh press after the mute must fire")
	}
}

func TestSuspendReturnsOnlyAfterInFlightDispatchChecksAndNoneStartsAfter(t *testing.T) {
	var g muteGate
	var started atomic.Int32
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if g.Allowed() {
						started.Add(1)
					}
				}
			}
		}()
	}
	for started.Load() == 0 {
	}
	g.Suspend()
	at := started.Load()
	for i := 0; i < 20000; i++ {
		_ = g.Allowed()
	}
	close(stop)
	wg.Wait()
	if started.Load() != at {
		t.Fatalf("%d callbacks started after Suspend returned", started.Load()-at)
	}
}

func TestResumeDrainsQueuedInputAndResyncsBeforeUnmuting(t *testing.T) {
	var g muteGate
	var order []string
	g.Suspend()
	g.Resume()
	if g.Allowed() {
		t.Fatal("must stay muted until the listener settles")
	}
	g.settle(
		func() {
			order = append(order, "drain")
			if g.Allowed() {
				t.Error("unmuted before the drain")
			}
		},
		func() {
			order = append(order, "resync")
			if g.Allowed() {
				t.Error("unmuted before the resync")
			}
		},
	)
	if len(order) != 2 || order[0] != "drain" || order[1] != "resync" || !g.Allowed() {
		t.Fatalf("order=%v allowed=%v", order, g.Allowed())
	}
	g.settle(func() { t.Error("settle without a pending resume") }, func() {})
}

func TestSuspendAfterResumeCancelsThePendingUnmute(t *testing.T) {
	var g muteGate
	g.Suspend()
	g.Resume()
	g.Suspend()
	g.settle(func() { t.Error("drained although re-suspended") }, func() {})
	if g.Allowed() {
		t.Fatal("a new mute must not be undone by the earlier resume")
	}
}
