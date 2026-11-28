package ui

import (
	"github.com/yanmad27/supcua-v2/app/status"
	"github.com/yanmad27/supcua-v2/model"
)

var (
	colorGreenActive = [3]uint8{0x00, 0xFF, 0x00}
	colorRedInactive = [3]uint8{0xFF, 0x00, 0x00}
	colorGrayIdle    = [3]uint8{0x80, 0x80, 0x80}
)

// overlayStatus is the overlay text and status colour for a published snapshot (v1.1.x texts and colours).
func overlayStatus(snap status.Snapshot) (string, [3]uint8) {
	if !snap.GlobalActive {
		return "DISABLE", colorRedInactive
	}
	if snap.RunningScriptName != "" {
		return "RUN: " + sanitizeStatusText(snap.RunningScriptName), colorGreenActive
	}
	return "IDLE", colorGrayIdle
}

// layeredPresent is what one repaint hands to UpdateLayeredWindow. Dst and Size are nil when the
// call leaves the position and size to the window.
type layeredPresent struct {
	Text string
	RGB  [3]uint8
	Dst  *[2]int
	Size *[2]int
}

// presentFor builds the repaint request for snap. A repaint of unchanged geometry (status change)
// must still carry the position and size.
func presentFor(snap status.Snapshot, st model.ResolvedOverlayStyle, x, y int, withGeometry bool) layeredPresent {
	text, rgb := overlayStatus(snap)
	if st.TextColor != "" {
		rgb[0], rgb[1], rgb[2] = parseHex(st.TextColor)
	}
	p := layeredPresent{Text: text, RGB: rgb}
	if withGeometry {
		p.Dst, p.Size = &[2]int{x, y}, &[2]int{st.Width, st.Height}
	}
	return p
}
