package model

import (
	"math"
	"regexp"
	"strings"
)

// Overlay style defaults reproduce the pre-customisation overlay exactly.
const (
	OverlayDefaultWidth      = 136
	OverlayDefaultHeight     = 32
	OverlayDefaultBackground = "#404040"
	OverlayDefaultOpacity    = 200.0 / 255.0

	OverlayMinWidth   = 80
	OverlayMinHeight  = 24
	OverlayMaxWidth   = 1024
	OverlayMaxHeight  = 256
	OverlayMinOpacity = 0.2

	overlayMinVisibleX = 40
	overlayMinVisibleY = 16
)

var hexColorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// OverlayStyle is the persisted, user-adjustable look of the overlay. A zero or missing field
// means the original value, so configs written by older builds load unchanged. Position lives in
// AppConfig.OverlayPosition (virtual-screen coordinates).
type OverlayStyle struct {
	Radius int `json:"radius,omitempty"`
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// Background is #RRGGBB; "" = #404040.
	Background string `json:"background,omitempty"`
	// TextColor is #RRGGBB; "" = status colours (green running, gray idle, red disabled).
	TextColor string `json:"text_color,omitempty"`
	// BackgroundOpacity is 0..1 for the background only; nil = 1 (0 is a valid, fully clear background).
	BackgroundOpacity *float64 `json:"background_opacity,omitempty"`
	// Opacity is the whole-window opacity, floor 0.2; 0 = 200/255.
	Opacity float64 `json:"opacity,omitempty"`
}

// ResolvedOverlayStyle is OverlayStyle with defaults filled and every value clamped.
type ResolvedOverlayStyle struct {
	Radius            int
	Width, Height     int
	Background        string
	TextColor         string
	BackgroundOpacity float64
	Opacity           float64
}

// ValidHexColor reports whether s is #RRGGBB.
func ValidHexColor(s string) bool { return hexColorRe.MatchString(s) }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return hi
	}
	return math.Min(math.Max(v, lo), hi)
}

// Resolve fills defaults and clamps: size to 80x24..1024x256, radius to 0..min(w,h)/2,
// colours to #RRGGBB (invalid -> default), background opacity to 0..1, opacity to 0.2..1.
func (s OverlayStyle) Resolve() ResolvedOverlayStyle {
	r := ResolvedOverlayStyle{Width: s.Width, Height: s.Height, Background: strings.ToUpper(s.Background), TextColor: strings.ToUpper(s.TextColor), BackgroundOpacity: 1, Opacity: OverlayDefaultOpacity}
	if r.Width == 0 {
		r.Width = OverlayDefaultWidth
	}
	if r.Height == 0 {
		r.Height = OverlayDefaultHeight
	}
	r.Width = clampInt(r.Width, OverlayMinWidth, OverlayMaxWidth)
	r.Height = clampInt(r.Height, OverlayMinHeight, OverlayMaxHeight)
	r.Radius = clampInt(s.Radius, 0, min(r.Width, r.Height)/2)
	if !ValidHexColor(r.Background) {
		r.Background = OverlayDefaultBackground
	}
	if !ValidHexColor(r.TextColor) {
		r.TextColor = ""
	}
	if s.BackgroundOpacity != nil {
		r.BackgroundOpacity = clampFloat(*s.BackgroundOpacity, 0, 1)
	}
	if s.Opacity != 0 {
		r.Opacity = clampFloat(s.Opacity, OverlayMinOpacity, 1)
	}
	return r
}

// Normalize returns the canonical persisted form: resolved, with every default stored as its
// zero value so a default style serialises to nothing.
func (s OverlayStyle) Normalize() OverlayStyle {
	r := s.Resolve()
	var out OverlayStyle
	if r.Width != OverlayDefaultWidth {
		out.Width = r.Width
	}
	if r.Height != OverlayDefaultHeight {
		out.Height = r.Height
	}
	out.Radius = r.Radius
	if r.Background != OverlayDefaultBackground {
		out.Background = r.Background
	}
	out.TextColor = r.TextColor
	if r.BackgroundOpacity != 1 {
		v := r.BackgroundOpacity
		out.BackgroundOpacity = &v
	}
	if math.Abs(r.Opacity-OverlayDefaultOpacity) > 0.0005 {
		out.Opacity = r.Opacity
	}
	return out
}

// Rect is a monitor rectangle in virtual-screen coordinates (Right/Bottom exclusive).
type Rect struct{ Left, Top, Right, Bottom int }

func overlapLen(a0, a1, b0, b1 int) int { return max(0, min(a1, b1)-max(a0, b0)) }

// ClampOverlayToMonitors keeps virtual coordinates but guarantees the w x h overlay shows at least
// 40x16 px (or its whole size when smaller) on one real monitor, so it can never sit in a gap between
// staggered monitors or off every screen. A position that already qualifies is returned unchanged;
// otherwise it moves to the closest position on the monitor that needs the smallest move. An empty
// monitor list (unknown) leaves the position unchanged.
func ClampOverlayToMonitors(x, y, w, h int, monitors []Rect) (int, int) {
	visX, visY := min(w, overlayMinVisibleX), min(h, overlayMinVisibleY)
	bestX, bestY, bestCost := x, y, -1
	for _, m := range monitors {
		if m.Right-m.Left <= 0 || m.Bottom-m.Top <= 0 {
			continue
		}
		if overlapLen(x, x+w, m.Left, m.Right) >= visX && overlapLen(y, y+h, m.Top, m.Bottom) >= visY {
			return x, y
		}
		cx := clampInt(x, m.Left+visX-w, m.Right-visX)
		cy := clampInt(y, m.Top+visY-h, m.Bottom-visY)
		cost := (cx-x)*(cx-x) + (cy-y)*(cy-y)
		if bestCost < 0 || cost < bestCost {
			bestX, bestY, bestCost = cx, cy, cost
		}
	}
	return bestX, bestY
}
