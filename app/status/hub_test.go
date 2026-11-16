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

func TestHubDeliversInRevisionOrderUnderConcurrency(t *testing.T) {
	var h Hub
	var mu sync.Mutex
	var seen []int
	h.Subscribe(func(s Snapshot) { mu.Lock(); seen = append(seen, s.QueueLength); mu.Unlock() })
	var counter int
	var cmu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.PublishFunc(func() Snapshot { cmu.Lock(); counter++; v := counter; cmu.Unlock(); return Snapshot{QueueLength: v} })
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("out-of-order delivery: %v", seen)
		}
	}
	if last, _ := h.Last(); seen[len(seen)-1] != last.QueueLength {
		t.Errorf("final delivery %d != last %d", seen[len(seen)-1], last.QueueLength)
	}
}

func TestHubReentrantPublishDoesNotDeadlockAndKeepsOrder(t *testing.T) {
	var h Hub
	var got []int
	h.Subscribe(func(s Snapshot) {
		got = append(got, s.QueueLength)
		if s.QueueLength == 1 {
			h.Publish(Snapshot{QueueLength: 2})
		}
	})
	h.Publish(Snapshot{QueueLength: 1})
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestHubSubscribeReplayNeverRegresses(t *testing.T) {
	var h Hub
	h.Publish(Snapshot{QueueLength: 1})
	var got []int
	h.Subscribe(func(s Snapshot) {
		got = append(got, s.QueueLength)
		if len(got) == 1 {
			h.Publish(Snapshot{QueueLength: 2})
		}
	})
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("replay then update expected, got %v", got)
	}
}
