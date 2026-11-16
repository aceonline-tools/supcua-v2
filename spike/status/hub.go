// Package status is the single fan-out point for app status: one Publish, many
// subscribers (frontend, tray, overlay). Subscribers must not block.
package status

import (
	"sync"

	"spike/rpc"
)

type Hub struct {
	mu   sync.Mutex
	subs []func(rpc.Status)
	last rpc.Status
	set  bool
}

// Subscribe registers fn; the latest status (if any) is replayed immediately.
func (h *Hub) Subscribe(fn func(rpc.Status)) {
	h.mu.Lock()
	h.subs = append(h.subs, fn)
	last, set := h.last, h.set
	h.mu.Unlock()
	if set {
		fn(last)
	}
}

// Publish delivers s to every subscriber unless it equals the last status.
func (h *Hub) Publish(s rpc.Status) {
	h.mu.Lock()
	if h.set && h.last == s {
		h.mu.Unlock()
		return
	}
	h.last, h.set = s, true
	subs := append([]func(rpc.Status){}, h.subs...)
	h.mu.Unlock()
	for _, fn := range subs {
		fn(s)
	}
}

func (h *Hub) Last() rpc.Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}
