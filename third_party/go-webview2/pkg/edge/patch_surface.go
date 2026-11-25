package edge

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
)

// SurfaceState is the controller's own view of its presentation surface (diagnostics).
type SurfaceState struct {
	Visible bool
	Bounds  w32.Rect
}

// Surface reads IsVisible and Bounds back from the controller. HRESULTs are checked from the
// call result, never from GetLastError.
func (e *Chromium) Surface() (SurfaceState, error) {
	var s SurfaceState
	if e.controller == nil {
		return s, errors.New("no controller")
	}
	c := unsafe.Pointer(e.controller)
	var vis int32
	if r, _, _ := e.controller.vtbl.GetIsVisible.Call(uintptr(c), uintptr(unsafe.Pointer(&vis))); int32(r) < 0 {
		return s, fmt.Errorf("get_IsVisible: HRESULT 0x%08X", uint32(r))
	}
	s.Visible = vis != 0
	if r, _, _ := e.controller.vtbl.GetBounds.Call(uintptr(c), uintptr(unsafe.Pointer(&s.Bounds))); int32(r) < 0 {
		return s, fmt.Errorf("get_Bounds: HRESULT 0x%08X", uint32(r))
	}
	return s, nil
}

// Reassert re-applies the controller's visibility and bounds from the host window's current
// state. The put_IsVisible and NotifyParentWindowPositionChanged HRESULTs are checked; the
// bounds update goes through Resize, whose results are not. A controller created under a hidden parent window can stay non-presenting (the
// default white background) after the parent is shown unless it is told to present.
func (e *Chromium) Reassert() error {
	if e.controller == nil {
		return errors.New("no controller")
	}
	if r, _, _ := e.controller.vtbl.PutIsVisible.Call(uintptr(unsafe.Pointer(e.controller)), 1); int32(r) < 0 {
		return fmt.Errorf("put_IsVisible: HRESULT 0x%08X", uint32(r))
	}
	e.Resize()
	if r, _, _ := e.controller.vtbl.NotifyParentWindowPositionChanged.Call(uintptr(unsafe.Pointer(e.controller))); int32(r) < 0 {
		return fmt.Errorf("NotifyParentWindowPositionChanged: HRESULT 0x%08X", uint32(r))
	}
	return nil
}
