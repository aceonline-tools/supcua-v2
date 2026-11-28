package ui

import (
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
// status-change repaint request (OverlayController.Refresh -> render(false)).
type overlaySink struct{ got []layeredPresent }

func (o *overlaySink) subscribe(c *app.Controller, st model.ResolvedOverlayStyle) {
	c.Subscribe(func(snap status.Snapshot) { o.got = append(o.got, presentFor(snap, st, 260, 180)) })
}

func (o *overlaySink) find(text string) (layeredPresent, bool) {
	for i := len(o.got) - 1; i >= 0; i-- {
		if o.got[i].Text == text {
			return o.got[i], true
		}
	}
	return layeredPresent{}, false
}

func TestOverlayFollowsControllerPublish(t *testing.T) {
	for _, tc := range []struct{ name, colour string }{{"status colours", ""}, {"custom colour", "#FF00FF"}} {
		t.Run(tc.name, func(t *testing.T) {
			st := model.OverlayStyle{TextColor: tc.colour}.Resolve()
			c := app.NewController(executor.NewExecutor(noInput{}, nil, 0), nil, nil, nil)
			sink := &overlaySink{}
			sink.subscribe(c, st)

			want := func(def [3]uint8) [3]uint8 {
				if tc.colour == "" {
					return def
				}
				r, g, b := parseHex(tc.colour)
				return [3]uint8{r, g, b}
			}
			c.StartScript(model.Script{ID: "s1", Name: "Fixture Probe", Enabled: true})
			time.Sleep(400 * time.Millisecond) // empty script ends; the IDLE publish follows
			c.ToggleGlobalFromHotkey(c.HotkeyEpoch())

			for _, w := range []struct {
				text string
				rgb  [3]uint8
			}{{"RUN: fixture_probe", colorGreenActive}, {"IDLE", colorGrayIdle}, {"DISABLE", colorRedInactive}} {
				p, ok := sink.find(w.text)
				if !ok {
					t.Fatalf("no repaint with text %q; got %+v", w.text, sink.got)
				}
				if p.RGB != want(w.rgb) {
					t.Errorf("%q colour = %v, want %v", w.text, p.RGB, want(w.rgb))
				}
			}
			for _, p := range sink.got {
				if p.Size != [2]int{st.Width, st.Height} || p.Dst != [2]int{260, 180} {
					t.Errorf("repaint %q passes no position/size to UpdateLayeredWindow: the screen keeps the previous frame", p.Text)
				}
			}
		})
	}
}
