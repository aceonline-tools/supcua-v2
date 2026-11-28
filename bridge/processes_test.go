package bridge

import (
	"encoding/json"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/ui/webview/rpc"
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

func TestProcessesListDoesNotBlockTheRPCWorker(t *testing.T) {
	block := &atomic.Bool{}
	block.Store(true)
	l := &hangLister{src: hangSource{gate: make(chan struct{}), block: block}}
	t.Cleanup(func() { block.Store(false); close(l.src.gate) })
	b := &Bridge{d: Deps{Windows: l, NewDeferred: func() (any, func(any, error)) {
		d := rpc.NewDeferred()
		return d, d.Resolve
	}}}
	d := rpc.New(map[string]rpc.Handler{
		"processes.list": func(json.RawMessage) (any, error) { return b.processesList() },
		"ping":           func(json.RawMessage) (any, error) { return "pong", nil },
	}, 8)
	d.Start()
	t.Cleanup(d.Close)
	out := make(chan string, 8)
	caller := rpc.Caller{Role: rpc.RoleMain, Gen: 1}
	d.Submit(`{"id":1,"method":"processes.list"}`, caller, func(b []byte) { out <- string(b) })
	start := time.Now()
	d.Submit(`{"id":2,"method":"ping"}`, caller, func(b []byte) { out <- string(b) })
	select {
	case got := <-out:
		if got != `{"id":2,"ok":true,"result":"pong"}` || time.Since(start) > 50*time.Millisecond {
			t.Fatalf("got %s after %v", got, time.Since(start))
		}
	case <-time.After(time.Second):
		t.Fatal("ping was blocked behind processes.list")
	}
	select {
	case <-out:
	case <-time.After(time.Second):
		t.Fatal("processes.list never answered")
	}
}

type panicLister struct{ calls atomic.Int32 }

func (p *panicLister) Collect(c *winlist.Collector) {
	if p.calls.Add(1) == 1 {
		panic("boom")
	}
	winlist.Collect(hangSource{gate: make(chan struct{}), block: &atomic.Bool{}}, 0, winlist.Budget, nil, c)
}

func TestProcessesListPanicIsIncompleteAndNotRemembered(t *testing.T) {
	l := &panicLister{}
	b := newProcBridge(l)
	res, _ := request(b)
	if !res.Truncated {
		t.Fatalf("panicked run reported complete: %+v", res)
	}
	b.procFlight.mu.Lock()
	last := b.procFlight.last
	b.procFlight.mu.Unlock()
	if last != nil {
		t.Fatal("panicked run installed as last completed result")
	}
	res, _ = request(b)
	if res.Truncated || len(res.Rows) != 3 {
		t.Fatalf("fresh run: %+v", res)
	}
}
