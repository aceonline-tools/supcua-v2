package main

import (
	"sync/atomic"
	"syscall"
)

// callbackCount counts this program's own syscall.NewCallback calls. Go never
// frees callback slots (cap 2000), so the count must be a constant: every call
// site below runs once at startup, never per window/dialog open.
var callbackCount atomic.Int32

func newCallback(fn any) uintptr {
	callbackCount.Add(1)
	return syscall.NewCallback(fn)
}
