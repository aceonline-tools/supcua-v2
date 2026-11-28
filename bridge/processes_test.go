package bridge

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/winlist"
)

type hangSource struct {
	gate  chan struct{}
	block *atomic.Bool
}

func (h hangSource) Windows(visit func(winlist.Window) bool, _ func() bool) {
	for i := uint32(1); i <= 3; i++ {
		if i == 3 && h.block.Load() {
			<-h.gate // a hung native call
		}
		if !visit(winlist.Window{H: uintptr(i), PID: i, Visible: true}) {
			return
		}
	}
}
func (hangSource) Title(h uintptr) string { return "t" + string(rune('0'+h)) }
func (hangSource) ExeName(pid uint32, _ func() bool) (string, error) {
	return "p" + string(rune('0'+pid)) + ".exe", nil
}

type hangLister struct {
	calls atomic.Int32
	src   hangSource
}

func (l *hangLister) Collect(c *winlist.Collector) {
	l.calls.Add(1)
	winlist.Collect(l.src, 0, winlist.Budget, nil, c)
}

func request(b *Bridge) (winlist.Result, time.Duration) {
	start := time.Now()
	out, _ := b.processesList()
	return (<-out.(chan winlist.Result)), time.Since(start)
}

func newProcBridge(l WindowLister) *Bridge {
	return &Bridge{d: Deps{Windows: l, NewDeferred: func() (any, func(any, error)) {
		ch := make(chan winlist.Result, 1)
		return ch, func(out any, _ error) { ch <- out.(winlist.Result) }
	}}}
}

func TestProcessesListHungRunIsBoundedAndSingle(t *testing.T) {
	block := &atomic.Bool{}
	block.Store(true)
	l := &hangLister{src: hangSource{gate: make(chan struct{}), block: block}}
	b := newProcBridge(l)
	before := runtime.NumGoroutine()

	res, el := request(b)
	if el > 300*time.Millisecond || !res.Truncated || len(res.Rows) != 2 {
		t.Fatalf("first: %v %+v", el, res)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := request(b)
			if e > 300*time.Millisecond || !r.Truncated || len(r.Rows) != 2 {
				t.Errorf("later: %v %+v", e, r)
			}
		}()
	}
	wg.Wait()
	if l.calls.Load() != 1 {
		t.Fatalf("native runs=%d, want 1", l.calls.Load())
	}

	block.Store(false)
	close(l.src.gate)
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.procFlight.mu.Lock()
		idle := b.procFlight.cur == nil
		b.procFlight.mu.Unlock()
		if idle || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	res, _ = request(b)
	if l.calls.Load() != 2 || res.Truncated || len(res.Rows) != 3 {
		t.Fatalf("fresh run: calls=%d %+v", l.calls.Load(), res)
	}
	time.Sleep(50 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("goroutines %d -> %d", before, after)
	}
}
