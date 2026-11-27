package model

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func fp(v float64) *float64 { return &v }

func TestOverlayStyleDefaultsEqualToday(t *testing.T) {
	r := OverlayStyle{}.Resolve()
	if r.Width != 136 || r.Height != 32 || r.Radius != 0 || r.Background != "#404040" || r.TextColor != "" ||
		r.BackgroundOpacity != 1 || r.Opacity != 200.0/255.0 {
		t.Fatalf("defaults drifted from the original overlay: %+v", r)
	}
	if math.Round(r.Opacity*255) != 200 {
		t.Fatalf("overall alpha %v != 200", r.Opacity*255)
	}
	if DefaultConfig().OverlayStyle != (OverlayStyle{}) {
		t.Fatal("DefaultConfig must carry the zero style")
	}
}

func TestOverlayStyleOldConfigLoadsUnchanged(t *testing.T) {
	var c AppConfig
	if err := json.Unmarshal([]byte(`{"overlay_enabled":true,"overlay_position":{"x":5,"y":6}}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.OverlayStyle.Resolve() != (OverlayStyle{}).Resolve() || c.OverlayPosition.X != 5 {
		t.Fatalf("old config changed: %+v", c)
	}
}

func TestOverlayStyleClamping(t *testing.T) {
	cases := []struct {
		name string
		in   OverlayStyle
		chk  func(ResolvedOverlayStyle) bool
	}{
		{"size floors", OverlayStyle{Width: 10, Height: 5}, func(r ResolvedOverlayStyle) bool { return r.Width == 80 && r.Height == 24 }},
		{"size caps", OverlayStyle{Width: 99999, Height: 99999}, func(r ResolvedOverlayStyle) bool { return r.Width == 1024 && r.Height == 256 }},
		{"radius capped to half the short side", OverlayStyle{Radius: 500, Width: 200, Height: 40}, func(r ResolvedOverlayStyle) bool { return r.Radius == 20 }},
		{"negative radius", OverlayStyle{Radius: -3}, func(r ResolvedOverlayStyle) bool { return r.Radius == 0 }},
		{"radius follows clamped size", OverlayStyle{Radius: 100, Width: 10, Height: 10}, func(r ResolvedOverlayStyle) bool { return r.Radius == 12 }},
		{"bad colours fall back", OverlayStyle{Background: "red", TextColor: "#12"}, func(r ResolvedOverlayStyle) bool { return r.Background == "#404040" && r.TextColor == "" }},
		{"colours upper-cased", OverlayStyle{Background: "#abcdef", TextColor: "#00ff7f"}, func(r ResolvedOverlayStyle) bool { return r.Background == "#ABCDEF" && r.TextColor == "#00FF7F" }},
		{"bg opacity may reach 0", OverlayStyle{BackgroundOpacity: fp(0)}, func(r ResolvedOverlayStyle) bool { return r.BackgroundOpacity == 0 }},
		{"bg opacity clamps", OverlayStyle{BackgroundOpacity: fp(7)}, func(r ResolvedOverlayStyle) bool { return r.BackgroundOpacity == 1 }},
		{"overall opacity floor", OverlayStyle{Opacity: 0.01}, func(r ResolvedOverlayStyle) bool { return r.Opacity == 0.2 }},
		{"overall opacity cap", OverlayStyle{Opacity: 3}, func(r ResolvedOverlayStyle) bool { return r.Opacity == 1 }},
		{"NaN opacity never invisible", OverlayStyle{Opacity: math.NaN()}, func(r ResolvedOverlayStyle) bool { return r.Opacity >= 0.2 }},
	}
	for _, c := range cases {
		if r := c.in.Resolve(); !c.chk(r) {
			t.Errorf("%s: got %+v", c.name, r)
		}
	}
}

func TestOverlayStyleNormalizeStoresDefaultsAsZero(t *testing.T) {
	in := OverlayStyle{Width: 136, Height: 32, Background: "#404040", BackgroundOpacity: fp(1), Opacity: 200.0 / 255.0}
	if got := in.Normalize(); got != (OverlayStyle{}) {
		t.Fatalf("defaults not collapsed: %+v", got)
	}
	b, _ := json.Marshal(AppConfig{OverlayStyle: in.Normalize()})
	if !strings.Contains(string(b), `"overlay_style":{}`) {
		t.Fatalf("default style must serialise empty: %s", b)
	}
}

func TestOverlayStyleJSONRoundTrip(t *testing.T) {
	in := OverlayStyle{Radius: 16, Width: 220, Height: 48, Background: "#112233", TextColor: "#FFEEDD", BackgroundOpacity: fp(0), Opacity: 0.8}
	b, err := json.Marshal(AppConfig{OverlayStyle: in})
	if err != nil {
		t.Fatal(err)
	}
	var out AppConfig
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.OverlayStyle.BackgroundOpacity == nil || *out.OverlayStyle.BackgroundOpacity != 0 {
		t.Fatalf("background opacity 0 lost in round-trip: %s", b)
	}
	got, want := out.OverlayStyle, in
	got.BackgroundOpacity, want.BackgroundOpacity = nil, nil
	if got != want {
		t.Fatalf("round-trip: %+v != %+v", got, want)
	}
}

func TestClampOverlayToMonitors(t *testing.T) {
	single := []Rect{{0, 0, 1920, 1080}}
	if x, y := ClampOverlayToMonitors(5000, 5000, 136, 32, single); x != 1880 || y != 1064 {
		t.Fatalf("far bottom-right: %d,%d", x, y)
	}
	if x, y := ClampOverlayToMonitors(-9999, -9999, 136, 32, single); x != -96 || y != -16 {
		t.Fatalf("far top-left: %d,%d", x, y)
	}
	if x, y := ClampOverlayToMonitors(100, 50, 136, 32, single); x != 100 || y != 50 {
		t.Fatalf("a visible position must not move: %d,%d", x, y)
	}
	if x, y := ClampOverlayToMonitors(7, 8, 136, 32, nil); x != 7 || y != 8 {
		t.Fatal("unknown monitors must not move the overlay")
	}
	// staggered monitors: inside the bounding box (0,0)-(3840,1680) but over the empty gap above the lower-right monitor
	staggered := []Rect{{0, 0, 1920, 1080}, {1920, 600, 3840, 1680}}
	x, y := ClampOverlayToMonitors(2500, 100, 136, 32, staggered)
	if !visibleOn(x, y, 136, 32, staggered) || x != 2500 || y != 584 {
		t.Fatalf("staggered monitors: overlay left in the gap / moved to the wrong monitor: %d,%d", x, y)
	}
	if x, y = ClampOverlayToMonitors(2500, 700, 136, 32, staggered); x != 2500 || y != 700 {
		t.Fatalf("a position on the second monitor must not move: %d,%d", x, y)
	}
}

func visibleOn(x, y, w, h int, ms []Rect) bool {
	for _, m := range ms {
		if overlapLen(x, x+w, m.Left, m.Right) >= min(w, 40) && overlapLen(y, y+h, m.Top, m.Bottom) >= min(h, 16) {
			return true
		}
	}
	return false
}
