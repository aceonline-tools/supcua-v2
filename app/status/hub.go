// Package status is the single fan-out point for app status: one Publish, many
// subscribers (frontend, tray, overlay, hotkey reconcile). Subscribers must not block.
package status

import "sync"

// Snapshot is the comparable app status published by the controller.
type Snapshot struct {
	GlobalActive      bool
	RunningScriptID   string
	RunningScriptName string
	EditDialogOpen    bool
	QueueLength       int
}

type subscriber struct {
	fn      func(Snapshot)
	lastRev uint64 // touched only by the active drainer
}

type task struct {
	rev  uint64
	snap Snapshot
	only *subscriber
}

// Hub delivers snapshots to subscribers in revision order. Each snapshot is read under the hub
// lock and stamped with a monotonic revision, one drainer at a time delivers the queue, and a
// subscriber never sees a revision older than one it already received.
type Hub struct {
	mu       sync.Mutex
	subs     []*subscriber
	last     Snapshot
	set      bool
	rev      uint64
	queue    []task
	draining bool
}

// Subscribe registers fn; the latest snapshot (if any) is replayed through the same ordered
// queue, so it can never arrive after a newer one.
func (h *Hub) Subscribe(fn func(Snapshot)) {
	sub := &subscriber{fn: fn}
	h.mu.Lock()
	h.subs = append(h.subs, sub)
	if h.set {
		h.queue = append(h.queue, task{rev: h.rev, snap: h.last, only: sub})
	}
	h.drainLocked()
}

// Publish delivers s to every subscriber unless it equals the last snapshot.
func (h *Hub) Publish(s Snapshot) { h.PublishFunc(func() Snapshot { return s }) }

// PublishFunc reads the snapshot under the hub lock, so concurrent publishers cannot deliver an
// older state after a newer one. read must not call back into the hub.
func (h *Hub) PublishFunc(read func() Snapshot) {
	h.mu.Lock()
	s := read()
	if h.set && h.last == s {
		h.mu.Unlock()
		return
	}
	h.last, h.set = s, true
	h.rev++
	h.queue = append(h.queue, task{rev: h.rev, snap: s})
	h.drainLocked()
}

// drainLocked is called with mu held and returns with it released. Re-entrant publishes from a
// subscriber only enqueue; the active drainer delivers them in order.
func (h *Hub) drainLocked() {
	if h.draining {
		h.mu.Unlock()
		return
	}
	h.draining = true
	for len(h.queue) > 0 {
		t := h.queue[0]
		h.queue[0] = task{}
		h.queue = h.queue[1:]
		targets := []*subscriber{t.only}
		if t.only == nil {
			targets = append([]*subscriber(nil), h.subs...)
		}
		h.mu.Unlock()
		for _, sub := range targets {
			if t.rev > sub.lastRev {
				sub.lastRev = t.rev
				sub.fn(t.snap)
			}
		}
		h.mu.Lock()
	}
	h.queue = nil
	h.draining = false
	h.mu.Unlock()
}

// Last returns the most recently published snapshot.
func (h *Hub) Last() (Snapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last, h.set
}
