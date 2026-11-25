//go:build windows

package ui

import (
	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/model"
)

// OverlayController owns the optional overlay window and implements bridge.Overlay. Every
// method runs on the UI thread. The overlay is created/destroyed from Settings, so the
// window procedure and class are registered once (overlay.go), never per instance.
type OverlayController struct {
	Controller *app.Controller
	Config     interface{ Get() model.AppConfig }
	// OnExternalChange runs on the UI thread after a change the bridge did not initiate
	// (hotkey, tray, drag end); main wires it to Bridge.RefreshOverlay and the tray labels.
	OnExternalChange func()

	w       *OverlayWindow
	lastX   int
	lastY   int
	havePos bool
}

func (o *OverlayController) Window() *OverlayWindow { return o.w }

func (o *OverlayController) position() (int, int) {
	if o.w != nil {
		return o.w.GetPosition()
	}
	if o.havePos {
		return o.lastX, o.lastY
	}
	p := o.Config.Get().OverlayPosition
	return p.X, p.Y
}

// State implements bridge.Overlay.
func (o *OverlayController) State() bridge.OverlayDTO {
	x, y := o.position()
	st := bridge.OverlayDTO{X: x, Y: y}
	if o.w != nil {
		st.Enabled, st.Visible, st.Draggable = true, o.w.IsVisible(), o.w.IsDraggable()
	}
	return st
}

// SetEnabled implements bridge.Overlay: create (visible, at the saved position) or destroy.
func (o *OverlayController) SetEnabled(enabled bool) error {
	if enabled == (o.w != nil) {
		return nil
	}
	if !enabled {
		o.lastX, o.lastY = o.w.GetPosition()
		o.havePos = true
		o.w.Destroy()
		o.w = nil
		return nil
	}
	x, y := o.position()
	w, err := NewOverlayWindow(o.Controller, x, y, true)
	if err != nil {
		return err
	}
	w.OnMoved = func(int, int) { o.external() }
	o.w = w
	return nil
}

// SetVisible implements bridge.Overlay.
func (o *OverlayController) SetVisible(visible bool) {
	if o.w != nil {
		o.w.SetVisible(visible)
	}
}

// SetDraggable implements bridge.Overlay.
func (o *OverlayController) SetDraggable(draggable bool) {
	if o.w != nil {
		o.w.SetDraggable(draggable)
	}
}

// ToggleVisibility is the overlay hotkey / tray action.
func (o *OverlayController) ToggleVisibility() {
	if o.w != nil {
		o.w.ToggleVisibility()
		o.external()
	}
}

// ToggleDraggable is the tray action.
func (o *OverlayController) ToggleDraggable() {
	if o.w != nil {
		o.w.ToggleDraggable()
		o.external()
	}
}

// IsDraggable reports the drag mode (tray label).
func (o *OverlayController) IsDraggable() bool { return o.w != nil && o.w.IsDraggable() }

// Refresh repaints after a status change.
func (o *OverlayController) Refresh() {
	if o.w != nil {
		o.w.Refresh()
	}
}

// Destroy removes the window (shutdown).
func (o *OverlayController) Destroy() {
	if o.w != nil {
		o.lastX, o.lastY = o.w.GetPosition()
		o.havePos = true
		o.w.Destroy()
		o.w = nil
	}
}

func (o *OverlayController) external() {
	if o.OnExternalChange != nil {
		o.OnExternalChange()
	}
}
