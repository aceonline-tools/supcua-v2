// Package otrace records when each stage of an editor-window open ran, so the Windows selftest can
// break a measured click-to-visible time into stages. Marks are off (one atomic load) in production.
package otrace

import (
	"sync"
	"sync/atomic"
	"time"
)

type Mark struct {
	Name string
	At   time.Time
}

var (
	on    atomic.Bool
	mu    sync.Mutex
	marks []Mark
)

// Enable turns recording on. Only the selftests call it.
func Enable() { on.Store(true) }

// At records name at the current time.
func At(name string) {
	if !on.Load() {
		return
	}
	mu.Lock()
	marks = append(marks, Mark{name, time.Now()})
	mu.Unlock()
}

// Reset clears the recorded marks.
func Reset() {
	mu.Lock()
	marks = nil
	mu.Unlock()
}

// Marks returns a copy of the recorded marks in order.
func Marks() []Mark {
	mu.Lock()
	defer mu.Unlock()
	return append([]Mark(nil), marks...)
}
