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

// SetDefaultBackground sets the surface colour shown before (and behind) the page, so the window is
// never white before the first paint. It reaches ICoreWebView2Controller2 through QueryInterface
// (released here) and checks the HRESULTs from the call results, not GetLastError.
func (e *Chromium) SetDefaultBackground(r, g, b uint8) error {
	if e.controller == nil {
		return errors.New("no controller")
	}
	c := unsafe.Pointer(e.controller)
	var c2 *ICoreWebView2Controller2
	iid := NewGUID("{c979903e-d4ca-4228-92eb-47ee3fa96eab}")
	if hr, _, _ := e.controller.vtbl.QueryInterface.Call(uintptr(c), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&c2))); int32(hr) < 0 || c2 == nil {
		return fmt.Errorf("QueryInterface(Controller2): HRESULT 0x%08X", uint32(hr))
	}
	defer c2.vtbl.Release.Call(uintptr(unsafe.Pointer(c2)))
	col := COREWEBVIEW2_COLOR{A: 0xff, R: r, G: g, B: b}
	if hr, _, _ := c2.vtbl.PutDefaultBackgroundColor.Call(uintptr(unsafe.Pointer(c2)), uintptr(*(*uint32)(unsafe.Pointer(&col)))); int32(hr) < 0 {
		return fmt.Errorf("put_DefaultBackgroundColor: HRESULT 0x%08X", uint32(hr))
	}
	return nil
}
