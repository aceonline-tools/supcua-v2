package executor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

func fakeAPI(created, hooked *atomic.Int32) hookAPI {
	return hookAPI{
		newCallback: func(interface{}) uintptr { created.Add(1); return 1 },
		setHook:     func(int, uintptr) uintptr { hooked.Add(1); return 42 },
		unhook:      func(uintptr) {},
		pump:        func() {},
	}
}

func TestHookCallbackCreatedOnce(t *testing.T) {
	for _, name := range []string{"mouse", "keyboard"} {
		t.Run(name, func(t *testing.T) {
			var created, hooked atomic.Int32
			var cache hookCallbackCache
			var handle atomic.Uintptr
			api := fakeAPI(&created, &hooked)
			for i := 0; i < 150; i++ {
				stop := runHook(api, &cache, func() {}, 13, &handle, nil)
				stop()
			}
			if created.Load() != 1 {
				t.Fatalf("newCallback calls = %d, want 1", created.Load())
			}
			if hooked.Load() != 150 {
				t.Fatalf("setHook calls = %d, want 150", hooked.Load())
			}
		})
	}
}

func TestRepeatFilter(t *testing.T) {
	var f repeatFilter
	toggles := 0
	for _, down := range []bool{true, true, true, false, true, true} {
		if f.onKey(down) {
			toggles++
		}
	}
	if toggles != 2 {
		t.Fatalf("toggles = %d, want 2", toggles)
	}
	f.reset()
	if !f.onKey(true) {
		t.Fatal("expected toggle after reset")
	}
}

func TestStopEndsEmptyInfiniteScripts(t *testing.T) {
	cases := map[string]model.Script{
		"top-level empty": {RunMode: model.RunModeInfinite},
		"flat empty loop": {RunMode: model.RunModeOnce, Actions: []model.Action{
			{Type: model.ActionLoop, LoopCount: 0}, {Type: "loop_end"}}},
		"nested empty loops": {RunMode: model.RunModeOnce, Actions: []model.Action{
			{Type: model.ActionLoop, LoopCount: 0}, {Type: model.ActionLoop, LoopCount: 0},
			{Type: "loop_end"}, {Type: "loop_end"}}},
		"nested struct empty": {RunMode: model.RunModeOnce, Actions: []model.Action{
			{Type: model.ActionLoop, LoopCount: 0, Actions: []model.Action{
				{Type: model.ActionLoop, LoopCount: 0, Actions: nil}}}}},
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			ex := NewExecutor(&fakeSim{}, nil, 10)
			done := make(chan error, 1)
			go func() { done <- ex.Execute(ctx, script) }()
			time.Sleep(30 * time.Millisecond)
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("expected cancellation error")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Execute did not stop after cancel")
			}
		})
	}
}
