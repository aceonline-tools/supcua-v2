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

// Hub delivers snapshots to subscribers, dropping consecutive duplicates.
type Hub struct {
	mu   sync.Mutex
	subs []func(Snapshot)
	last Snapshot
	set  bool
}

// Subscribe registers fn; the latest snapshot (if any) is replayed immediately.
func (h *Hub) Subscribe(fn func(Snapshot)) {
	h.mu.Lock()
	h.subs = append(h.subs, fn)
	last, set := h.last, h.set
	h.mu.Unlock()
	if set {
		fn(last)
	}
}

// Publish delivers s to every subscriber unless it equals the last snapshot.
// Callers must not hold locks that subscribers may need.
func (h *Hub) Publish(s Snapshot) {
	h.mu.Lock()
	if h.set && h.last == s {
		h.mu.Unlock()
		return
	}
	h.last, h.set = s, true
	subs := append([]func(Snapshot){}, h.subs...)
	h.mu.Unlock()
	for _, fn := range subs {
		fn(s)
	}
}

// Last returns the most recently published snapshot.
func (h *Hub) Last() (Snapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last, h.set
}
