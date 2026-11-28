package bridge

import (
	"log"
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

func (b *Bridge) processesList() (any, error) {
	if b.d.Windows == nil {
		return winlist.Result{Rows: []winlist.Row{}}, nil
	}
	start := time.Now()
	res := b.d.Windows.List()
	if res.Rows == nil {
		res.Rows = []winlist.Row{}
	}
	log.Printf("processes.list: rows=%d truncated=%v elapsed_ms=%d", len(res.Rows), res.Truncated, time.Since(start).Milliseconds())
	return res, nil
}
