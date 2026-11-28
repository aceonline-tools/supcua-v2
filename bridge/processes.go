package bridge

import (
	"log"
	"sync"
	"time"

	"github.com/yanmad27/supcua-v2/winlist"
)

// WindowLister lists the processes that own a visible top-level window (names and titles only).
type WindowLister interface {
	List() winlist.Result
}

// SystemWindowLister is the live-desktop WindowLister, bounded by winlist.Budget.
type SystemWindowLister struct{}

func (SystemWindowLister) List() winlist.Result {
	return winlist.List(winlist.SystemSource{}, winlist.Self(), winlist.Budget, nil)
}

type processFlight struct {
	mu      sync.Mutex
	running bool
	waiters []func(any, error)
}

func (b *Bridge) listProcesses() winlist.Result {
	start := time.Now()
	res := b.d.Windows.List()
	if res.Rows == nil {
		res.Rows = []winlist.Row{}
	}
	log.Printf("processes.list: rows=%d truncated=%v elapsed_ms=%d", len(res.Rows), res.Truncated, time.Since(start).Milliseconds())
	return res
}

// processesList answers off the shared RPC worker when the host provides a deferred reply, and
// single-flights: a request arriving while an enumeration runs joins it.
func (b *Bridge) processesList() (any, error) {
	if b.d.Windows == nil {
		return winlist.Result{Rows: []winlist.Row{}}, nil
	}
	if b.d.NewDeferred == nil {
		return b.listProcesses(), nil
	}
	reply, resolve := b.d.NewDeferred()
	f := &b.procFlight
	f.mu.Lock()
	f.waiters = append(f.waiters, resolve)
	start := !f.running
	f.running = true
	f.mu.Unlock()
	if start {
		go func() {
			var res any
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("bridge: processes.list panicked: %v", r)
						err = newErr(CodeInternal)
					}
				}()
				res = b.listProcesses()
			}()
			f.mu.Lock()
			ws := f.waiters
			f.waiters, f.running = nil, false
			f.mu.Unlock()
			for _, w := range ws {
				w(res, err)
			}
		}()
	}
	return reply, nil
}
