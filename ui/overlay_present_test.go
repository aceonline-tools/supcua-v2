package ui

import (
	"sync"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/app/status"
	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/model"
)

type noInput struct {
	executor.InputSimulatorInterface
}

func (noInput) UnlockAll() {}

// overlaySink mimics the production subscriber in main.go: every published snapshot becomes a
// status-change repaint request (OverlayController.Refresh -> render). Publishes arrive from
// worker goroutines, so access is locked and waits use a condition with a deadline.
type overlaySink struct {
	mu   sync.Mutex
	got  []layeredPresent
	seen chan struct{}
}

func newSink() *overlaySink { return &overlaySink{seen: make(chan struct{}, 64)} }

func (o *overlaySink) subscribe(c *app.Controller, st model.ResolvedOverlayStyle) {
	c.Subscribe(func(snap status.Snapshot) {
		p := presentFor(snap, st, 260, 180)
		o.mu.Lock()
		o.got = append(o.got, p)
		o.mu.Unlock()
		select {
		case o.seen <- struct{}{}:
		default:
		}
	})
}

func (o *overlaySink) find(text string) (layeredPresent, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := len(o.got) - 1; i >= 0; i-- {
		if o.got[i].Text == text {
			return o.got[i], true
		}
	}
	return layeredPresent{}, false
}

func (o *overlaySink) waitFor(t *testing.T, text string) layeredPresent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if p, ok := o.find(text); ok {
			return p
		}
		select {
		case <-o.seen:
		case <-deadline:
			o.mu.Lock()
			defer o.mu.Unlock()
			t.Fatalf("no repaint with text %q within 5s; got %+v", text, o.got)
		}
	}
}

func TestOverlayFollowsControllerPublish(t *testing.T) {
	for _, tc := range []struct{ name, colour string }{{"status colours", ""}, {"custom colour", "#FF00FF"}} {
		t.Run(tc.name, func(t *testing.T) {
			st := model.OverlayStyle{TextColor: tc.colour}.Resolve()
			c := app.NewController(executor.NewExecutor(noInput{}, nil, 0), nil, nil, nil)
			sink := newSink()
			sink.subscribe(c, st)

			want := func(def [3]uint8) [3]uint8 {
				if tc.colour == "" {
					return def
				}
				r, g, b := parseHex(tc.colour)
				return [3]uint8{r, g, b}
			}
			c.StartScript(model.Script{ID: "s1", Name: "Fixture Probe", Enabled: true})
			sink.waitFor(t, "RUN: fixture_probe") // the empty script ends by itself
			sink.waitFor(t, "IDLE")
			c.ToggleGlobalFromHotkey(c.HotkeyEpoch())
			sink.waitFor(t, "DISABLE")

			for _, w := range []struct {
				text string
				rgb  [3]uint8
			}{{"RUN: fixture_probe", colorGreenActive}, {"IDLE", colorGrayIdle}, {"DISABLE", colorRedInactive}} {
				p, _ := sink.find(w.text)
				if p.RGB != want(w.rgb) {
					t.Errorf("%q colour = %v, want %v", w.text, p.RGB, want(w.rgb))
				}
				if p.Size != [2]int32{int32(st.Width), int32(st.Height)} || p.Dst != [2]int32{260, 180} {
					t.Errorf("repaint %q geometry = dst %v size %v, want the window's own", w.text, p.Dst, p.Size)
				}
			}
		})
	}
}
