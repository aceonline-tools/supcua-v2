package main

import (
	"log"
	"sync"

	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
)

type hotkeyRegistry interface {
	Register(model.Hotkey, hotkey.HotkeyCallback) (int, error)
	Unregister(int) error
}

// fixedHotkeys owns the global and overlay hotkey registrations and re-binds them with
// restore-on-failure (bridge.Host.RebindHotkey).
//
// Startup is deferred: add only records the desired hotkey and callback; start (off the UI
// thread, after the pump runs) registers whatever is desired at that moment. A rebind that
// lands before start just updates the desired hotkey, so startup can never re-apply an older
// value. stop (shutdown) forbids any further registration.
type fixedHotkeys struct {
	mu      sync.Mutex
	mgr     hotkeyRegistry
	on      bool
	pending bool
	stopped bool
	order   []string
	ids     map[string]int
	cur     map[string]model.Hotkey
	cb      map[string]hotkey.HotkeyCallback
	pv      map[string]*hotkey.Preview
}

func newFixedHotkeys(mgr hotkeyRegistry, on bool) *fixedHotkeys {
	return &fixedHotkeys{mgr: mgr, on: on, pending: on, ids: map[string]int{}, cur: map[string]model.Hotkey{}, cb: map[string]hotkey.HotkeyCallback{}, pv: map[string]*hotkey.Preview{}}
}

func (f *fixedHotkeys) registerLocked(kind string) {
	hk := f.cur[kind]
	if !f.on || hk.Key == "" || f.ids[kind] != 0 {
		return
	}
	id, err := f.mgr.Register(hk, f.cb[kind])
	if err != nil {
		log.Printf("warning: failed to register %s hotkey: %v", kind, err)
		return
	}
	f.ids[kind] = id
	if pv := f.pv[kind]; pv != nil {
		if b, ok := f.mgr.(previewBinder); ok {
			b.SetPreview(id, *pv)
		}
	}
}

// setPreview attaches a key-down overlay preview to kind's hotkey (used when it is a bare modifier).
func (f *fixedHotkeys) setPreview(kind string, p hotkey.Preview) {
	f.mu.Lock()
	f.pv[kind] = &p
	f.mu.Unlock()
}

func (f *fixedHotkeys) add(kind string, hk model.Hotkey, cb hotkey.HotkeyCallback) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, known := f.cb[kind]; !known {
		f.order = append(f.order, kind)
	}
	f.cur[kind], f.cb[kind] = hk, cb
	if f.pending || f.stopped {
		return
	}
	f.registerLocked(kind)
}

// start registers the latest desired hotkeys and ends the deferred phase.
func (f *fixedHotkeys) start() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = false
	if f.stopped {
		return
	}
	for _, kind := range f.order {
		f.registerLocked(kind)
	}
}

// stop forbids further registrations; it waits for one in flight (bounded by hotkey.RegisterTimeout).
func (f *fixedHotkeys) stop() {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
}

func (f *fixedHotkeys) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range f.ids {
		if id != 0 {
			n++
		}
	}
	return n
}

func (f *fixedHotkeys) rebind(kind string, hk model.Hotkey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cb := f.cb[kind]
	if kind == bridge.HotkeyCapture || !f.on || cb == nil || f.pending || f.stopped {
		f.cur[kind] = hk
		return nil
	}
	oldID := f.ids[kind]
	if hk.Key != "" {
		// Register the new binding first: a failure leaves the old OS registration untouched.
		id, err := f.mgr.Register(hk, cb)
		if err != nil {
			return err
		}
		f.ids[kind] = id
	} else {
		f.ids[kind] = 0
	}
	if oldID != 0 {
		if err := f.mgr.Unregister(oldID); err != nil {
			log.Printf("warning: could not release the old %s hotkey: %v", kind, err)
		}
	}
	f.cur[kind] = hk
	return nil
}
