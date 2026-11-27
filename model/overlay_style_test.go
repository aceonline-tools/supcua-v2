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

func TestClampOverlayPosition(t *testing.T) {
	x, y := ClampOverlayPosition(5000, 5000, 136, 32, 0, 0, 1920, 1080)
	if x != 1880 || y != 1064 {
		t.Fatalf("far bottom-right: %d,%d", x, y)
	}
	x, y = ClampOverlayPosition(-9999, -9999, 136, 32, 0, 0, 1920, 1080)
	if x != -96 || y != -16 {
		t.Fatalf("far top-left: %d,%d", x, y)
	}
	x, y = ClampOverlayPosition(-1500, 10, 136, 32, -1920, 0, 3840, 1080)
	if x != -1500 || y != 10 {
		t.Fatalf("negative-origin virtual screen must keep a visible position: %d,%d", x, y)
	}
	if x, y = ClampOverlayPosition(7, 8, 136, 32, 0, 0, 0, 0); x != 7 || y != 8 {
		t.Fatal("unknown screen must not move the overlay")
	}
}
