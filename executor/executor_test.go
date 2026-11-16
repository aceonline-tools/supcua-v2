package executor

import (
	"context"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestExecuteBasicActions(t *testing.T) {
	sim := &fakeSim{}
	ex := NewExecutor(sim, nil, 10)

	err := ex.Execute(context.Background(), once(
		model.Action{Type: model.ActionMouseMove, X: 100, Y: 200},
		act(model.ActionMouseLeftDown),
		act(model.ActionMouseLeftUp),
		model.Action{Type: model.ActionKeyDown, Key: "a"},
		model.Action{Type: model.ActionKeyUp, Key: "a"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	eq(t, strip(sim.log()), []string{"move:100,200", "ldown", "lup", "kd:a", "ku:a"})
}

func TestExecuteLoop(t *testing.T) {
	sim := &fakeSim{}
	ex := NewExecutor(sim, nil, 10)

	err := ex.Execute(context.Background(), once(model.Action{
		Type:      model.ActionLoop,
		LoopCount: 3,
		Actions:   []model.Action{act(model.ActionMouseLeftDown), act(model.ActionMouseLeftUp)},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	eq(t, strip(sim.log()), []string{"ldown", "lup", "ldown", "lup", "ldown", "lup"})
}

func TestExecuteCancelledContext(t *testing.T) {
	sim := &fakeSim{}
	ex := NewExecutor(sim, nil, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ex.Execute(ctx, once(
		model.Action{Type: model.ActionDelay, DelayMs: 5000},
		act(model.ActionMouseLeftDown),
	))
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	eq(t, sim.log(), []string{"unlockall"})
}

func TestExecuteInfiniteMode(t *testing.T) {
	sim := &fakeSim{}
	ex := NewExecutor(sim, nil, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runAsync(ex, ctx, model.Script{
		ID:      "test-infinite",
		RunMode: model.RunModeInfinite,
		Actions: []model.Action{act(model.ActionMouseLeftDown)},
	})

	eventually(t, "5 actions", func() bool { return sim.count("ldown") >= 5 })
	cancel()

	if err := waitDone(t, done); err == nil {
		t.Error("expected context cancelled error")
	}
	if got := sim.count("ldown"); got < 5 {
		t.Errorf("expected at least 5 actions, got %d", got)
	}
}
