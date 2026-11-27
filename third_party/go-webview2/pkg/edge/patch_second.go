package edge

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

// markInited records the end of controller creation (success or failure) and reports it once to
// InitDoneCallback. initErr must be set before the call.
func (e *Chromium) markInited() {
	if !atomic.CompareAndSwapUintptr(&e.inited, 0, 1) {
		return
	}
	if e.InitDoneCallback != nil {
		e.InitDoneCallback(e.initErr)
	}
}

// EmbedFromEnvironment starts controller creation inside hwnd on an environment created by
// another Chromium (same user-data folder, same browser process) and returns at once: no message
// pump runs. InitDoneCallback reports the result on the UI thread. The environment is AddRef'd
// and released by Close.
func (e *Chromium) EmbedFromEnvironment(env *ICoreWebView2Environment, hwnd uintptr) error {
	if env == nil {
		return fmt.Errorf("no environment")
	}
	e.hwnd = hwnd
	_, _, _ = env.vtbl.AddRef.Call(uintptr(unsafe.Pointer(env)))
	e.environment = env
	r, _, _ := env.vtbl.CreateCoreWebView2Controller.Call(uintptr(unsafe.Pointer(env)), hwnd, uintptr(unsafe.Pointer(e.controllerCompleted)))
	if int32(r) < 0 {
		return fmt.Errorf("CreateCoreWebView2Controller: HRESULT 0x%08X", uint32(r))
	}
	return nil
}

// Abandon marks creation cancelled so a late completion releases its controller instead of
// keeping it (rollback while the controller is still being created).
func (e *Chromium) Abandon() { atomic.StoreUint32(&e.cancelled, 1) }
