package bridge

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yanmad27/supcua-v2/winlist"
)

type gateLister struct {
	calls   atomic.Int32
	release chan struct{}
}

func (g *gateLister) List() winlist.Result {
	g.calls.Add(1)
	<-g.release
	return winlist.Result{Rows: []winlist.Row{{Exe: "a.exe", Title: "t"}}}
}

func TestProcessesListSingleFlight(t *testing.T) {
	g := &gateLister{release: make(chan struct{})}
	var wg sync.WaitGroup
	var got atomic.Int32
	b := &Bridge{d: Deps{Windows: g, NewDeferred: func() (any, func(any, error)) {
		wg.Add(1)
		return struct{}{}, func(out any, err error) {
			if r, ok := out.(winlist.Result); ok && err == nil && len(r.Rows) == 1 {
				got.Add(1)
			}
			wg.Done()
		}
	}}}
	for i := 0; i < 3; i++ {
		if _, err := b.processesList(); err != nil {
			t.Fatal(err)
		}
	}
	close(g.release)
	wg.Wait()
	if g.calls.Load() != 1 || got.Load() != 3 {
		t.Fatalf("calls=%d resolved=%d", g.calls.Load(), got.Load())
	}
	if _, err := b.processesList(); err != nil { // a later request starts a fresh run
		t.Fatal(err)
	}
	wg.Wait()
	if g.calls.Load() != 2 {
		t.Fatalf("calls=%d", g.calls.Load())
	}
}
