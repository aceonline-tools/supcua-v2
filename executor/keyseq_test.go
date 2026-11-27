package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

func seqAction(delay *int, keys ...model.KeyStroke) model.Action {
	return model.Action{Type: model.ActionKeyPress, Keys: keys, KeyDelayMs: delay}
}

func ks(key string, mods ...string) model.KeyStroke { return model.KeyStroke{Key: key, Mods: mods} }

func recordSleeps(t *testing.T, sim *fakeSim) {
	t.Helper()
	orig := sleepCtx
	t.Cleanup(func() { sleepCtx = orig })
	sleepCtx = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sim.rec("sleep:" + d.String())
		return nil
	}
}

func intv(v int) *int { return &v }

func TestKeySeq_OrderModsAndDelayOnlyBetweenKeys(t *testing.T) {
	sim := &fakeSim{}
	recordSleeps(t, sim)
	err := run(t, sim, nil, once(seqAction(intv(30), ks("a"), ks("b", "ctrl", "shift"), ks("c"))))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim.log()), []string{
		"kd:a", "sleep:1ms", "ku:a", "sleep:30ms",
		"kd:ctrl", "kd:shift", "kd:b", "sleep:1ms", "ku:b", "ku:shift", "ku:ctrl", "sleep:30ms",
		"kd:c", "sleep:1ms", "ku:c",
	})
}

func TestKeySeq_DefaultDelayAndLegacyKey(t *testing.T) {
	sim := &fakeSim{}
	recordSleeps(t, sim)
	if err := run(t, sim, nil, once(seqAction(nil, ks("a"), ks("b")))); err != nil {
		t.Fatal(err)
	}
	if sim.count("sleep:50ms") != 1 {
		t.Fatalf("default delay not applied once: %v", sim.log())
	}
	sim2 := &fakeSim{}
	recordSleeps(t, sim2)
	if err := run(t, sim2, nil, once(model.Action{Type: model.ActionKeyPress, Key: "z"})); err != nil {
		t.Fatal(err)
	}
	eq(t, strip(sim2.log()), []string{"kd:z", "sleep:1ms", "ku:z"})
}

func TestKeySeq_AbortDuringDelayInterruptsAndNoStuckKeys(t *testing.T) {
	sim := &fakeSim{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ex := NewExecutor(sim, nil, 1)
	done := runAsync(ex, ctx, once(seqAction(intv(5000), ks("a"), ks("b"))))
	eventually(t, "first key released", func() bool { return sim.count("ku:a") == 1 })
	start := time.Now()
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("delay was not interrupted")
	}
	if sim.count("kd:b") != 0 {
		t.Fatalf("second key pressed after abort: %v", sim.log())
	}
}

func TestKeySeq_AbortBetweenKeysAndMidHold(t *testing.T) {
	sim := &fakeSim{}
	ctx, cancel := context.WithCancel(context.Background())
	orig := sleepCtx
	t.Cleanup(func() { sleepCtx = orig })
	sleepCtx = func(c context.Context, d time.Duration) error {
		if d == 7*time.Millisecond { // key hold of the first key
			cancel()
		}
		return orig(c, d)
	}
	ex := NewExecutor(sim, nil, 7)
	err := ex.Execute(ctx, once(seqAction(intv(0), ks("a", "alt"), ks("b"))))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
	eq(t, strip(sim.log()), []string{"kd:alt", "kd:a", "ku:a", "ku:alt"})
}

func TestKeySeq_ErrorReleasesHeldKeys(t *testing.T) {
	sim := &failOnKey{fakeSim: &fakeSim{}, bad: "a"}
	recordSleeps(t, sim.fakeSim)
	ex := NewExecutor(sim, nil, 1)
	err := ex.Execute(context.Background(), once(seqAction(intv(10), ks("a", "ctrl"), ks("b"))))
	if err == nil || err.Error() != "boom" {
		t.Fatalf("want boom, got %v", err)
	}
	eq(t, strip(sim.log()), []string{"kd:ctrl", "kd:a", "ku:ctrl"})
}

func TestKeySeq_EmptySequenceErrors(t *testing.T) {
	if err := run(t, &fakeSim{}, nil, once(model.Action{Type: model.ActionKeyPress})); err == nil {
		t.Fatal("empty sequence must fail")
	}
}

type failOnKey struct {
	*fakeSim
	bad string
}

func (f *failOnKey) KeyDown(k string) error {
	f.rec("kd:" + k)
	if k == f.bad {
		return errors.New("boom")
	}
	return nil
}
