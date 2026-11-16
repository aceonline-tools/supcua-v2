package executor

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// hookAPI is the platform seam for low-level hook installation.
type hookAPI struct {
	newCallback func(fn interface{}) uintptr
	setHook     func(hookID int, cb uintptr) uintptr
	unhook      func(handle uintptr)
	pump        func()
}

// hookCallbackCache creates a hook callback exactly once. Callback slots
// created by syscall.NewCallback are never released, so they must not be
// created per lock.
type hookCallbackCache struct {
	once sync.Once
	cb   uintptr
}

func (c *hookCallbackCache) get(api hookAPI, proc interface{}) uintptr {
	c.once.Do(func() { c.cb = api.newCallback(proc) })
	return c.cb
}

// runHook installs the hook on a dedicated OS thread, pumps messages until the
// returned stop function is called, then unhooks. The handle is published to
// handleOut for the hook procedure's CallNextHookEx calls.
func runHook(api hookAPI, cache *hookCallbackCache, proc interface{}, hookID int, handleOut *atomic.Uintptr, onStart func()) func() {
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(doneCh)

		if onStart != nil {
			onStart()
		}
		handle := api.setHook(hookID, cache.get(api, proc))
		if handle == 0 {
			return
		}
		handleOut.Store(handle)
		defer func() {
			api.unhook(handle)
			handleOut.Store(0)
		}()

		for {
			select {
			case <-stopCh:
				return
			default:
				api.pump()
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	return func() {
		close(stopCh)
		<-doneCh
	}
}

// repeatFilter reports true only for the first key-down of a physical press;
// auto-repeat key-downs are ignored until a key-up resets it.
type repeatFilter struct{ down atomic.Bool }

func (f *repeatFilter) onKey(isDown bool) bool {
	if !isDown {
		f.down.Store(false)
		return false
	}
	return f.down.CompareAndSwap(false, true)
}

func (f *repeatFilter) reset() { f.down.Store(false) }
