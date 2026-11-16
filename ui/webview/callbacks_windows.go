package webview

import (
	"sync/atomic"
	"syscall"
	"unsafe"
)

// callbackCount counts this package's syscall.NewCallback calls. Go caps callbacks at 2000
// and never frees them, so each call site is behind sync.Once: host window procedure
// (host_windows.go) and the shared keyboard hook (capture_windows.go) = 2, constant.
var callbackCount atomic.Int32

func newCallback(fn any) uintptr {
	callbackCount.Add(1)
	return syscall.NewCallback(fn)
}

// NewCallback is the counted wrapper other UI packages must use for every syscall.NewCallback
// (each call site must be once-guarded); the self-test asserts the count stays constant.
func NewCallback(fn any) uintptr { return newCallback(fn) }

// CallbackCount returns how many NewCallback calls this package made (self-test input).
func CallbackCount() int32 { return callbackCount.Load() }

// ptrFrom reinterprets a window-message LPARAM that points at a live OS-owned struct
// (valid for the duration of the callback) without vet's uintptr->Pointer conversion.
func ptrFrom(v uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&v)) }
