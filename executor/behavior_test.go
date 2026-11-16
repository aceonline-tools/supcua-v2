package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

const (
	tLoop = model.ActionLoop
	tEnd  = "loop_end"
)

func run(t *testing.T, sim *fakeSim, det ImageDetectorInterface, s model.Script) error {
	t.Helper()
	ex := NewExecutor(sim, det, 1)
	return ex.Execute(context.Background(), s)
}

func loopOf(n int, body ...model.Action) model.Action {
	return model.Action{Type: tLoop, LoopCount: n, Actions: body}
}

// ---- action sequences ----

func TestSequence_AllMouseKeyActions(t *testing.T) {
	sim := &fakeSim{}
	err := run(t, sim, nil, once(
		act(model.ActionMouseLeftClick),
		act(model.ActionMouseRightClick),
		act(model.ActionMouseLeftDouble),
		act(model.ActionMouseRightDouble),
		act(model.ActionMouseRightDown),
		act(model.ActionMouseRightUp),
		model.Action{Type: model.ActionKeyPress, Key: "x"},
	))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{
		"ldown", "lup", "rdown", "rup",
		"ldown", "lup", "ldown", "lup",
		"rdown", "rup", "rdown", "rup",
		"rdown", "rup",
		"kd:x", "ku:x",
	})
}

func TestStoredPositionRoundTrip(t *testing.T) {
	sim := &fakeSim{cursorX: 7, cursorY: 9}
	err := run(t, sim, nil, once(
		act(model.ActionMouseStorePos),
		model.Action{Type: model.ActionMouseMove, X: 1, Y: 2},
		act(model.ActionMouseMoveStored),
	))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{"move:1,2", "move:7,9"})
}

func TestUnknownActionTypeErrors(t *testing.T) {
	sim := &fakeSim{}
	err := run(t, sim, nil, once(act(model.ActionMouseLeftDown), act("bogus"), act(model.ActionMouseLeftUp)))
	if err == nil || !strings.Contains(err.Error(), "unknown action type") {
		t.Fatalf("err = %v", err)
	}
	eq(t, sim.log(), []string{"ldown", "unlockall"})
}

func TestActionErrorStopsScript(t *testing.T) {
	boom := errors.New("boom")
	sim := &fakeSim{moveErr: boom}
	err := run(t, sim, nil, once(model.Action{Type: model.ActionMouseMove}, act(model.ActionMouseLeftDown)))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	eq(t, sim.log(), []string{"move:0,0", "unlockall"})

	sim = &fakeSim{keyErr: boom}
	err = run(t, sim, nil, once(model.Action{Type: model.ActionKeyPress, Key: "k"}, act(model.ActionMouseLeftDown)))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	eq(t, sim.log(), []string{"kd:k", "unlockall"})
}

// ---- loops ----

func TestLoopCountNested(t *testing.T) {
	sim := &fakeSim{}
	err := run(t, sim, nil, once(loopOf(2, act(model.ActionMouseLeftClick), loopOf(3, act(model.ActionMouseRightClick)))))
	if err != nil {
		t.Fatal(err)
	}
	if got := sim.count("ldown"); got != 2 {
		t.Errorf("outer body ran %d times, want 2", got)
	}
	if got := sim.count("rdown"); got != 6 {
		t.Errorf("inner body ran %d times, want 6", got)
	}
}

func TestLoopFlatLoopEnd(t *testing.T) {
	sim := &fakeSim{}
	err := run(t, sim, nil, once(
		model.Action{Type: tLoop, LoopCount: 3},
		act(model.ActionMouseLeftClick),
		act(tEnd),
		act(model.ActionMouseRightClick),
	))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{"ldown", "lup", "ldown", "lup", "ldown", "lup", "rdown", "rup"})
}

func TestLoopFlatNestedDepth(t *testing.T) {
	sim := &fakeSim{}
	err := run(t, sim, nil, once(
		model.Action{Type: tLoop, LoopCount: 2},
		act(model.ActionMouseLeftClick),
		model.Action{Type: tLoop, LoopCount: 2},
		act(model.ActionMouseRightClick),
		act(tEnd),
		act(tEnd),
		model.Action{Type: model.ActionKeyDown, Key: "z"},
	))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{
		"ldown", "lup", "rdown", "rup", "rdown", "rup",
		"ldown", "lup", "rdown", "rup", "rdown", "rup",
		"kd:z",
	})
}

func TestLoopFlatUnclosedRunsToEnd(t *testing.T) {
	sim := &fakeSim{}
	err := run(t, sim, nil, once(
		model.Action{Type: tLoop, LoopCount: 2},
		act(model.ActionMouseLeftClick),
	))
	if err != nil {
		t.Fatal(err)
	}
	if got := sim.count("ldown"); got != 2 {
		t.Errorf("ldown = %d, want 2", got)
	}
}

func TestLoopNegativeCountRunsZero(t *testing.T) {
	sim := &fakeSim{}
	if err := run(t, sim, nil, once(loopOf(-1, act(model.ActionMouseLeftClick)))); err != nil {
		t.Fatal(err)
	}
	eq(t, sim.log(), []string{"unlockall"})
}

// ---- run modes ----

func TestRunModeOnce(t *testing.T) {
	sim := &fakeSim{}
	if err := run(t, sim, nil, once(act(model.ActionMouseLeftDown))); err != nil {
		t.Fatal(err)
	}
	if sim.count("ldown") != 1 {
		t.Fatalf("events=%v", sim.log())
	}
}

func TestRunModeInfiniteRunsMultipleThenCancel(t *testing.T) {
	sim := &fakeSim{}
	ex := NewExecutor(sim, nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runAsync(ex, ctx, model.Script{RunMode: model.RunModeInfinite, Actions: []model.Action{act(model.ActionMouseLeftDown)}})
	eventually(t, "3 iterations", func() bool { return sim.count("ldown") >= 3 })
	cancel()
	if err := waitDone(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if sim.count("unlockall") != 1 {
		t.Fatalf("events tail missing unlockall")
	}
}

func TestLoopCountZeroIsInfinite_Cancel(t *testing.T) {
	sim := &fakeSim{}
	ex := NewExecutor(sim, nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runAsync(ex, ctx, once(loopOf(0, act(model.ActionMouseLeftDown))))
	eventually(t, "iterations", func() bool { return sim.count("ldown") >= 3 })
	cancel()
	if err := waitDone(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	assertUnlockAllLast(t, sim)
}

// ---- if_image ----

func ifImage(then, els []model.Action) model.Action {
	return model.Action{Type: model.ActionIfImage, Image: "img", ThenActions: then, ElseActions: els}
}

func TestIfImage_FoundRunsThen(t *testing.T) {
	sim := &fakeSim{}
	det := &fakeDetector{}
	err := run(t, sim, det, once(ifImage([]model.Action{act(model.ActionMouseLeftDown)}, []model.Action{act(model.ActionMouseRightDown)})))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{"ldown"})
}

func TestIfImage_NotFoundRunsElse(t *testing.T) {
	sim := &fakeSim{}
	det := &fakeDetector{foundAfter: -1}
	err := run(t, sim, det, once(ifImage([]model.Action{act(model.ActionMouseLeftDown)}, []model.Action{act(model.ActionMouseRightDown)})))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{"rdown"})
}

func TestIfImage_NotFoundNoElseNoop(t *testing.T) {
	sim := &fakeSim{}
	det := &fakeDetector{foundAfter: -1}
	if err := run(t, sim, det, once(ifImage([]model.Action{act(model.ActionMouseLeftDown)}, nil), act(model.ActionMouseRightDown))); err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{"rdown"})
}

func TestIfImage_UseFoundPositionSetsFound(t *testing.T) {
	sim := &fakeSim{}
	det := &fakeDetector{x: 100, y: 50}
	a := ifImage(nil, nil)
	a.UseFoundPosition = true
	err := run(t, sim, det, once(a, model.Action{Type: model.ActionMouseMove, Source: "found", OffsetX: 5, OffsetY: -5}))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{"move:105,45"})
}

func TestIfImage_DetectorNilAndError(t *testing.T) {
	sim := &fakeSim{}
	if err := run(t, sim, nil, once(ifImage(nil, nil))); err == nil {
		t.Fatal("expected error with nil detector")
	}
	boom := errors.New("boom")
	err := run(t, &fakeSim{}, &fakeDetector{err: boom}, once(ifImage(nil, nil)))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestIfImage_SearchRegionPassed(t *testing.T) {
	region := &model.Region{X: 1, Y: 2, Width: 3, Height: 4}
	det := &fakeDetector{}
	a := ifImage(nil, nil)
	a.SearchRegion = region
	if err := run(t, &fakeSim{}, det, once(a)); err != nil {
		t.Fatal(err)
	}
	if det.lastRegion != region || det.lastImage != "img" {
		t.Fatalf("region=%v image=%q", det.lastRegion, det.lastImage)
	}
}

// ---- wait_for_image ----

func waitImg(timeoutMs int) model.Action {
	return model.Action{Type: model.ActionWaitForImage, Image: "img", TimeoutMs: timeoutMs}
}

func TestWaitForImage_FoundImmediately(t *testing.T) {
	det := &fakeDetector{}
	if err := run(t, &fakeSim{}, det, once(waitImg(5000))); err != nil {
		t.Fatal(err)
	}
	if det.callCount() != 1 {
		t.Fatalf("calls = %d", det.callCount())
	}
}

func TestWaitForImage_FoundAfterRetries(t *testing.T) {
	det := &fakeDetector{foundAfter: 3}
	sim := &fakeSim{}
	if err := run(t, sim, det, once(waitImg(10000), act(model.ActionMouseLeftDown))); err != nil {
		t.Fatal(err)
	}
	if det.callCount() != 4 {
		t.Fatalf("calls = %d, want 4", det.callCount())
	}
	eq(t, strip(sim.log()), []string{"ldown"})
}

func TestWaitForImage_TimesOut(t *testing.T) {
	det := &fakeDetector{foundAfter: -1}
	sim := &fakeSim{}
	err := run(t, sim, det, once(waitImg(50), act(model.ActionMouseLeftDown)))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	eq(t, sim.log(), []string{"unlockall"})
}

func TestWaitForImage_DetectorErrorAndNil(t *testing.T) {
	boom := errors.New("boom")
	err := run(t, &fakeSim{}, &fakeDetector{err: boom}, once(waitImg(1000)))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if err := run(t, &fakeSim{}, nil, once(waitImg(1000))); err == nil {
		t.Fatal("expected error with nil detector")
	}
}

func TestWaitForImage_UseFoundPosition(t *testing.T) {
	det := &fakeDetector{x: 10, y: 20}
	w := waitImg(1000)
	w.UseFoundPosition = true
	sim := &fakeSim{}
	if err := run(t, sim, det, once(w, model.Action{Type: model.ActionMouseMove, Source: "found", OffsetX: 1, OffsetY: 2})); err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{"move:11,22"})
}

// ---- delays ----

func TestDelay_WaitsAtLeast(t *testing.T) {
	start := time.Now()
	if err := run(t, &fakeSim{}, nil, once(model.Action{Type: model.ActionDelay, DelayMs: 40})); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < 30*time.Millisecond {
		t.Fatalf("elapsed %v < 30ms", el)
	}
}

func TestDelay_ZeroReturnsPromptly(t *testing.T) {
	if err := run(t, &fakeSim{}, nil, once(model.Action{Type: model.ActionDelay})); err != nil {
		t.Fatal(err)
	}
}

// ---- cancellation at every level ----

// cancelAfterEvent runs s, waits for trigger (an observable checkpoint inside
// the script), then checks Execute has not returned after a short settle
// window before cancelling. The check is best-effort: it makes a cancel that
// lands before the script reached its blocking construct unlikely, but a
// descheduled executor goroutine can still be cancelled early, so the failure
// mode is a false pass, never a false failure. Scripts passed here must block
// (or loop) far longer than any test timeout so they cannot finish on their own.
func cancelAfterEvent(t *testing.T, s model.Script, det ImageDetectorInterface, trigger func(*fakeSim) bool) (*fakeSim, error) {
	t.Helper()
	sim := &fakeSim{}
	ex := NewExecutor(sim, det, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runAsync(ex, ctx, s)
	eventually(t, "trigger", func() bool { return trigger(sim) })
	select {
	case err := <-done:
		t.Fatalf("Execute returned before cancel: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	start := time.Now()
	err := waitDone(t, done)
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("cancel took %v", el)
	}
	return sim, err
}

func hasLdown(s *fakeSim) bool { return s.count("ldown") >= 1 }

func TestCancel_BeforeStart(t *testing.T) {
	sim := &fakeSim{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewExecutor(sim, nil, 1).Execute(ctx, once(act(model.ActionMouseLeftDown)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	eq(t, sim.log(), []string{"unlockall"})
}

func TestCancel_DuringDelay(t *testing.T) {
	sim, err := cancelAfterEvent(t, once(act(model.ActionMouseLeftDown), model.Action{Type: model.ActionDelay, DelayMs: 3600000}, act(model.ActionMouseRightDown)), nil, hasLdown)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	eq(t, sim.log(), []string{"ldown", "unlockall"})
}

func TestCancel_DuringWaitForImage(t *testing.T) {
	det := &fakeDetector{foundAfter: -1}
	sim, err := cancelAfterEvent(t, once(act(model.ActionMouseLeftDown), waitImg(3600000), act(model.ActionMouseRightDown)), det, func(f *fakeSim) bool { return hasLdown(f) && det.callCount() > 0 })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	assertUnlockAllLast(t, sim)
	if sim.count("rdown") != 0 {
		t.Fatal("action after wait ran")
	}
}

func TestCancel_InsideNestedLoop(t *testing.T) {
	s := once(loopOf(1000000, loopOf(1000000, act(model.ActionMouseLeftDown), model.Action{Type: model.ActionDelay, DelayMs: 1})))
	sim, err := cancelAfterEvent(t, s, nil, hasLdown)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	assertUnlockAllLast(t, sim)
}

func TestCancel_InsideInfiniteLoopAction(t *testing.T) {
	for name, body := range map[string][]model.Action{
		"body":  {act(model.ActionMouseLeftDown)},
		"empty": nil,
	} {
		t.Run(name, func(t *testing.T) {
			sim, err := cancelAfterEvent(t, once(act(model.ActionMouseLeftDown), loopOf(0, body...), act(model.ActionMouseRightDown)), nil, hasLdown)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v", err)
			}
			assertUnlockAllLast(t, sim)
		})
	}
}

func TestCancel_FlatInfiniteLoopEmptyBody(t *testing.T) {
	s := once(act(model.ActionMouseLeftDown), model.Action{Type: tLoop}, act(tEnd), act(model.ActionMouseRightDown))
	sim, err := cancelAfterEvent(t, s, nil, hasLdown)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	assertUnlockAllLast(t, sim)
}

func TestCancel_EmptyInfiniteScript(t *testing.T) {
	sim, err := cancelAfterEvent(t, model.Script{RunMode: model.RunModeInfinite}, nil, func(*fakeSim) bool { return true })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	eq(t, sim.log(), []string{"unlockall"})
}

func TestCancel_InsideIfThen(t *testing.T) {
	then := []model.Action{act(model.ActionMouseLeftDown), model.Action{Type: model.ActionDelay, DelayMs: 3600000}}
	sim, err := cancelAfterEvent(t, once(ifImage(then, nil)), &fakeDetector{}, hasLdown)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	assertUnlockAllLast(t, sim)
}

func TestCancel_InsideIfElse(t *testing.T) {
	els := []model.Action{act(model.ActionMouseLeftDown), model.Action{Type: model.ActionDelay, DelayMs: 3600000}}
	sim, err := cancelAfterEvent(t, once(ifImage([]model.Action{act(model.ActionMouseRightDown)}, els)), &fakeDetector{foundAfter: -1}, hasLdown)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if sim.count("rdown") != 0 {
		t.Fatal("then branch ran")
	}
	assertUnlockAllLast(t, sim)
}

func TestCancel_InsideCountedLoopOfDelays(t *testing.T) {
	s := once(loopOf(1000000, act(model.ActionMouseLeftDown), model.Action{Type: model.ActionDelay, DelayMs: 5}), act(model.ActionMouseRightDown))
	sim, err := cancelAfterEvent(t, s, nil, func(f *fakeSim) bool { return f.count("ldown") >= 2 })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if sim.count("rdown") != 0 {
		t.Fatal("action after cancelled loop ran")
	}
	assertUnlockAllLast(t, sim)
}

// ---- lock/unlock pairing ----

func TestLockActionsDispatchInOrder(t *testing.T) {
	sim := &fakeSim{}
	err := run(t, sim, nil, once(
		act(model.ActionLockMouse), act(model.ActionLockKeyboard),
		act(model.ActionUnlockMouse), act(model.ActionUnlockKeyboard),
	))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, sim.log(), []string{"lockm", "lockk", "unlockm", "unlockk", "unlockall"})
}

func TestUnlockAll_AfterSuccessEmptyAndLeftLocked(t *testing.T) {
	sim := &fakeSim{}
	if err := run(t, sim, nil, once(act(model.ActionLockMouse), act(model.ActionLockKeyboard))); err != nil {
		t.Fatal(err)
	}
	assertUnlockAllLast(t, sim)

	sim = &fakeSim{}
	if err := run(t, sim, nil, once()); err != nil {
		t.Fatal(err)
	}
	eq(t, sim.log(), []string{"unlockall"})
}

func TestUnlockAll_AfterErrorAndTimeout(t *testing.T) {
	sim := &fakeSim{}
	if err := run(t, sim, nil, once(act(model.ActionLockMouse), act("bogus"))); err == nil {
		t.Fatal("expected error")
	}
	assertUnlockAllLast(t, sim)

	sim = &fakeSim{}
	err := run(t, sim, &fakeDetector{foundAfter: -1}, once(act(model.ActionLockKeyboard), waitImg(20)))
	if err == nil {
		t.Fatal("expected timeout")
	}
	assertUnlockAllLast(t, sim)
}

func TestUnlockAll_AfterCancelWhileLocked(t *testing.T) {
	s := once(act(model.ActionLockMouse), model.Action{Type: model.ActionDelay, DelayMs: 3600000})
	sim, err := cancelAfterEvent(t, s, nil, func(f *fakeSim) bool { return f.count("lockm") == 1 })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	assertUnlockAllLast(t, sim)
}
