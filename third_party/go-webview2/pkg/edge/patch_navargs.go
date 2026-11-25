//go:build windows

// PATCH (supcua): NavigationCompleted event-args getters. No NewCallback: plain vtable calls.

package edge

import "unsafe"

// NavigationResult reports whether the navigation succeeded and its COREWEBVIEW2_WEB_ERROR_STATUS.
// Fails closed: any HRESULT failure reports success=false.
func (a *ICoreWebView2NavigationCompletedEventArgs) NavigationResult() (success bool, status int32) {
	if a == nil {
		return false, -1
	}
	var ok int32
	if r, _, _ := a.vtbl.GetIsSuccess.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&ok))); int32(r) < 0 {
		return false, -1
	}
	if r, _, _ := a.vtbl.GetWebErrorStatus.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&status))); int32(r) < 0 {
		status = -1
	}
	return ok != 0, status
}
