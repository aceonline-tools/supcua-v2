//go:build windows

package ui

import (
	"testing"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/model"
)

// A status repaint must hand UpdateLayeredWindow an explicit position and size. The seam records
// exactly what render passes; a nil or zero value here is the stale-screen regression.
func TestStatusRepaintPassesGeometryToUpdateLayeredWindow(t *testing.T) {
	type call struct {
		dst  *win.POINT
		size *win.SIZE
	}
	var calls []call
	orig := updateLayered
	updateLayered = func(_ win.HWND, _ win.HDC, dst *win.POINT, size *win.SIZE, _ *win.POINT, _ *blendFunction) error {
		c := call{}
		if dst != nil {
			d := *dst
			c.dst = &d
		}
		if size != nil {
			s := *size
			c.size = &s
		}
		calls = append(calls, c)
		return nil
	}
	defer func() { updateLayered = orig }()

	st := model.OverlayStyle{}.Resolve()
	c := app.NewController(executor.NewExecutor(noInput{}, nil, 0), nil, nil, nil)
	ow := &OverlayWindow{appController: c, style: st, positionX: 260, positionY: 180}

	ow.Refresh()
	c.SetGlobalActive(false)
	ow.Refresh()

	if len(calls) != 2 {
		t.Fatalf("UpdateLayeredWindow calls = %d, want 2", len(calls))
	}
	for i, got := range calls {
		if got.dst == nil || got.size == nil {
			t.Fatalf("repaint %d: dst=%v size=%v, want both non-nil", i, got.dst, got.size)
		}
		if got.size.CX != int32(st.Width) || got.size.CY != int32(st.Height) || got.dst.X != 260 || got.dst.Y != 180 {
			t.Errorf("repaint %d: dst=%+v size=%+v, want 260,180 %dx%d", i, *got.dst, *got.size, st.Width, st.Height)
		}
	}
}
