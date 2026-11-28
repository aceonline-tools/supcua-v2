package hotkey

import (
	"sync"
	"sync/atomic"
)

type previewEvent struct {
	vk   uint32
	down bool
}

// keyMask is a lock-free set of virtual-key codes, read by the keyboard hook callback.
type keyMask [4]atomic.Uint64

func (m *keyMask) set(vk uint32, on bool) {
	if vk >= 256 {
		return
	}
	w, bit := &m[vk/64], uint64(1)<<(vk%64)
	for {
		old := w.Load()
		n := old | bit
		if !on {
			n = old &^ bit
		}
		if w.CompareAndSwap(old, n) {
			return
		}
	}
}

func (m *keyMask) has(vk uint32) bool {
	return vk < 256 && m[vk/64].Load()&(1<<(vk%64)) != 0
}

// queueKey is the whole hook callback decision: only registered bare-modifier keys are queued, so
// ordinary typing costs one atomic load and queues nothing.
func queueKey(mask *keyMask, emit func(previewEvent), vk uint32, down, injected, acceptInjected bool) bool {
	if (injected && !acceptInjected) || !mask.has(vk) {
		return false
	}
	emit(previewEvent{vk, down})
	return true
}

// previewRunner owns the key hook and exactly one worker that feeds its events, in order, to
// handle. Start is idempotent; stop unhooks, then ends the worker; a hook that dies by itself
// resets the state so the next start installs a fresh one.
type previewRunner struct {
	install func(emit func(previewEvent), dead func()) (stop func(), err error)
	handle  func(previewEvent)

	events  atomic.Pointer[chan previewEvent]
	workers atomic.Int32

	mu      sync.Mutex
	running bool
	gen     uint64
	stopHk  func()
	quit    chan struct{}
	done    chan struct{}
}

// emit never blocks and takes no lock (it runs inside the hook callback): a full or abandoned
// queue drops the event, and the poll still ends any preview whose key is up.
func (r *previewRunner) emit(ev previewEvent) {
	if p := r.events.Load(); p != nil {
		select {
		case *p <- ev:
		default:
		}
	}
}

func (r *previewRunner) start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	ch := make(chan previewEvent, 256)
	r.events.Store(&ch)
	gen := r.gen + 1
	stop, err := r.install(r.emit, func() { go r.died(gen) })
	if err != nil {
		r.events.Store(nil)
		return err
	}
	r.gen, r.stopHk, r.running = gen, stop, true
	r.quit, r.done = make(chan struct{}), make(chan struct{})
	r.workers.Add(1)
	go func(quit, done chan struct{}) {
		defer r.workers.Add(-1)
		defer close(done)
		for {
			select {
			case ev := <-ch:
				r.handle(ev)
			case <-quit:
				return
			}
		}
	}(r.quit, r.done)
	return nil
}

func (r *previewRunner) died(gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running && r.gen == gen {
		r.teardownLocked()
	}
}

func (r *previewRunner) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		r.teardownLocked()
	}
}

func (r *previewRunner) teardownLocked() {
	r.stopHk() // returns once the hook thread has unhooked and exited: no more emits
	close(r.quit)
	<-r.done
	r.events.Store(nil)
	r.running = false
}
