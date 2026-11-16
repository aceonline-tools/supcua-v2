package status

import (
	"sync"
	"testing"
)

func TestHubReplaysAndDedupes(t *testing.T) {
	var h Hub
	h.Publish(Snapshot{GlobalActive: true})
	var got []Snapshot
	h.Subscribe(func(s Snapshot) { got = append(got, s) })
	if len(got) != 1 || !got[0].GlobalActive {
		t.Fatalf("expected replay of last snapshot, got %v", got)
	}
	h.Publish(Snapshot{GlobalActive: true})
	if len(got) != 1 {
		t.Fatalf("equal snapshot must be dropped, got %d deliveries", len(got))
	}
	h.Publish(Snapshot{GlobalActive: true, QueueLength: 1})
	if len(got) != 2 {
		t.Fatalf("changed snapshot must be delivered, got %d", len(got))
	}
}

func TestHubConcurrentPublish(t *testing.T) {
	var h Hub
	var mu sync.Mutex
	n := 0
	h.Subscribe(func(Snapshot) { mu.Lock(); n++; mu.Unlock() })
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); h.Publish(Snapshot{QueueLength: i}) }(i)
	}
	wg.Wait()
	if _, ok := h.Last(); !ok {
		t.Fatal("expected last snapshot")
	}
}
