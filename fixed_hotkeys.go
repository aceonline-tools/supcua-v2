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
}

func newFixedHotkeys(mgr hotkeyRegistry, on bool) *fixedHotkeys {
	return &fixedHotkeys{mgr: mgr, on: on, pending: on, ids: map[string]int{}, cur: map[string]model.Hotkey{}, cb: map[string]hotkey.HotkeyCallback{}}
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
	old, cb := f.cur[kind], f.cb[kind]
	if kind == bridge.HotkeyCapture || !f.on || cb == nil || f.pending || f.stopped {
		f.cur[kind] = hk
		return nil
	}
	if id := f.ids[kind]; id != 0 {
		_ = f.mgr.Unregister(id)
		f.ids[kind] = 0
	}
	if hk.Key != "" {
		id, err := f.mgr.Register(hk, cb)
		if err != nil {
			if old.Key != "" {
				if rid, rerr := f.mgr.Register(old, cb); rerr == nil {
					f.ids[kind] = rid
				} else {
					log.Printf("warning: could not restore %s hotkey: %v", kind, rerr)
				}
			}
			return err
		}
		f.ids[kind] = id
	}
	f.cur[kind] = hk
	return nil
}
