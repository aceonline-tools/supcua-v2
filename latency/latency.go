// Package latency records the last time each stage of a hotkey-to-overlay update ran, so the
// Windows selftest can break a measured end-to-end latency into stages. Marks are lock-free.
package latency

import (
	"sync/atomic"
	"time"
)

type Stage int

const (
	Detect  Stage = iota // listener / LL hook decided the hotkey fired
	State                // controller state changed
	Publish              // status subscriber received the snapshot
	Refresh              // UI thread started the overlay repaint
	Present              // UpdateLayeredWindow returned
	stages
)

var (
	base  = time.Now()
	marks [stages]atomic.Int64
)

// Now is a monotonic timestamp comparable with Marks.
func Now() time.Duration { return time.Since(base) }

var enabled atomic.Bool

// Enable turns the marks on. Only the selftests call it; in production Mark is one atomic load.
func Enable() { enabled.Store(true) }

func Mark(s Stage) {
	if enabled.Load() {
		marks[s].Store(int64(Now()) + 1)
	}
}

func Reset() {
	for i := range marks {
		marks[i].Store(0)
	}
}

// Marks returns each stage's last mark; a stage that has not run since Reset is zero.
func Marks() (out [stages]time.Duration) {
	for i := range marks {
		if v := marks[i].Load(); v != 0 {
			out[i] = time.Duration(v - 1)
		}
	}
	return out
}
