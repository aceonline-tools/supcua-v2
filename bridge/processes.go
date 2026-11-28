package bridge

import (
	"log"
	"sync"
	"time"

	"github.com/yanmad27/supcua-v2/winlist"
)

// WindowLister runs one window enumeration into c.
type WindowLister interface {
	Collect(c *winlist.Collector)
}

// SystemWindowLister is the live-desktop WindowLister, bounded by winlist.Budget.
type SystemWindowLister struct{}

func (SystemWindowLister) Collect(c *winlist.Collector) {
	winlist.Collect(winlist.SystemSource{}, winlist.Self(), winlist.Budget, nil, c)
}

// processResponseBudget is the latest a request is answered after its run started.
const processResponseBudget = winlist.Budget + 40*time.Millisecond

type processRun struct {
	col     *winlist.Collector
	started time.Time
	done    chan struct{}
}

// processFlight keeps at most one native enumeration in flight. A run that outlives the response
// budget (a hung native call) is never joined by waiting and never replaced: requests are answered
// from its partial snapshot until it returns.
type processFlight struct {
	mu   sync.Mutex
	cur  *processRun
	last *winlist.Result
}

func (b *Bridge) acquireProcessRun() *processRun {
	f := &b.procFlight
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur != nil {
		return f.cur
	}
	run := &processRun{col: winlist.NewCollector(), started: time.Now(), done: make(chan struct{})}
	f.cur = run
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("bridge: processes.list panicked: %v", r)
			}
			res := run.col.Snapshot()
			f.mu.Lock()
			f.last, f.cur = &res, nil
			f.mu.Unlock()
			close(run.done)
			log.Printf("processes.list: rows=%d truncated=%v elapsed_ms=%d", len(res.Rows), res.Truncated, time.Since(run.started).Milliseconds())
		}()
		b.d.Windows.Collect(run.col)
	}()
	return run
}

// answerProcesses waits for run at most until its response deadline, then answers from a snapshot.
func (b *Bridge) answerProcesses(run *processRun) winlist.Result {
	if wait := processResponseBudget - time.Since(run.started); wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-run.done:
		case <-t.C:
		}
		t.Stop()
	}
	res := run.col.Snapshot()
	select {
	case <-run.done:
	default:
		res.Truncated = true
		if len(res.Rows) == 0 {
			b.procFlight.mu.Lock()
			last := b.procFlight.last
			b.procFlight.mu.Unlock()
			if last != nil {
				res = winlist.Result{Rows: last.Rows, Truncated: true}
			}
		}
	}
	if res.Rows == nil {
		res.Rows = []winlist.Row{}
	}
	return res
}

// processesList answers off the shared RPC worker when the host provides a deferred reply.
func (b *Bridge) processesList() (any, error) {
	if b.d.Windows == nil {
		return winlist.Result{Rows: []winlist.Row{}}, nil
	}
	run := b.acquireProcessRun()
	if b.d.NewDeferred == nil {
		return b.answerProcesses(run), nil
	}
	reply, resolve := b.d.NewDeferred()
	go resolve(b.answerProcesses(run), nil)
	return reply, nil
}
