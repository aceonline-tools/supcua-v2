// Package winlist lists the running processes that own a user-visible top-level window, for the
// auto-active process picker. It reads executable base names and window titles only.
package winlist

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// MaxRows caps the returned list.
	MaxRows = 200
	// Budget is the hard total time allowed for one enumeration.
	Budget = 250 * time.Millisecond
	// MaxTitleRunes bounds a returned title.
	MaxTitleRunes = 200
)

// Window is one top-level window as reported by a Source.
type Window struct {
	H          uintptr // opaque handle, only meaningful to the Source that produced it
	PID        uint32
	Visible    bool
	ToolWindow bool
	Cloaked    bool
	Owned      bool
	AppWindow  bool
}

// Source enumerates top-level windows and resolves a process id to its executable base name.
type Source interface {
	// Windows calls visit for each top-level window until visit returns false. expired reports the
	// budget is spent: a Source checks it between native calls and stops once it is true.
	Windows(visit func(Window) bool, expired func() bool)
	// Title returns the window title. It is only asked for windows that passed every cheap filter.
	Title(h uintptr) string
	// ExeName returns the executable base name; an error means the process is inaccessible or gone.
	ExeName(pid uint32, expired func() bool) (string, error)
}

// Row is one picker entry. It carries no path, PID or handle.
type Row struct {
	Exe   string `json:"exe"`
	Title string `json:"title"`
}

// Result is the processes.list reply.
type Result struct {
	Rows      []Row `json:"rows"`
	Truncated bool  `json:"truncated"`
}

type entry struct {
	row Row
	pid uint32
}

// Collector accumulates rows while an enumeration runs, so a snapshot can be taken at any time.
type Collector struct {
	mu        sync.Mutex
	entries   []entry
	truncated bool
}

// NewCollector returns an empty Collector.
func NewCollector() *Collector { return &Collector{} }

func (c *Collector) add(e entry) {
	c.mu.Lock()
	c.entries = append(c.entries, e)
	c.mu.Unlock()
}

func (c *Collector) setTruncated() {
	c.mu.Lock()
	c.truncated = true
	c.mu.Unlock()
}

// Snapshot returns the rows collected so far, deduplicated, sorted and capped. Truncated is set
// when the run hit its budget or the cap; a caller answering before the run finished sets it too.
func (c *Collector) Snapshot() Result {
	c.mu.Lock()
	entries := append([]entry(nil), c.entries...)
	truncated := c.truncated
	c.mu.Unlock()
	return finish(entries, truncated)
}

// List runs one enumeration to completion (or to its budget) and returns the result.
func List(src Source, selfPID uint32, budget time.Duration, clock func() time.Time) Result {
	c := NewCollector()
	Collect(src, selfPID, budget, clock, c)
	return c.Snapshot()
}

// Collect enumerates src into c, excluding selfPID, and stops once budget has elapsed on clock.
func Collect(src Source, selfPID uint32, budget time.Duration, clock func() time.Time, c *Collector) {
	if clock == nil {
		clock = time.Now
	}
	deadline := clock().Add(budget)
	expired := func() bool {
		if !clock().Before(deadline) {
			c.setTruncated()
			return true
		}
		return false
	}
	exeCache := map[uint32]string{}
	bad := map[uint32]bool{}

	src.Windows(func(w Window) bool {
		if expired() {
			return false
		}
		if !w.Visible || w.ToolWindow || w.Cloaked || (w.Owned && !w.AppWindow) || w.PID == 0 || w.PID == selfPID || bad[w.PID] {
			return true
		}
		title := strings.TrimSpace(src.Title(w.H))
		if title == "" {
			return !expired()
		}
		exe, ok := exeCache[w.PID]
		if !ok {
			if expired() {
				return false
			}
			name, err := src.ExeName(w.PID, expired)
			if err != nil || strings.TrimSpace(name) == "" {
				bad[w.PID] = true
				return !expired()
			}
			exe = baseName(name)
			exeCache[w.PID] = exe
		}
		c.add(entry{Row{Exe: exe, Title: clip(title)}, w.PID})
		return !expired()
	}, expired)
	expired()
}

func finish(entries []entry, truncated bool) Result {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if ae, be := strings.ToLower(a.row.Exe), strings.ToLower(b.row.Exe); ae != be {
			return ae < be
		}
		if a.row.Title != b.row.Title {
			return a.row.Title < b.row.Title
		}
		return a.pid < b.pid
	})
	entries = dedupe(entries)
	if len(entries) > MaxRows {
		entries = entries[:MaxRows]
		truncated = true
	}
	rows := make([]Row, len(entries))
	for i, e := range entries {
		rows[i] = e.row
	}
	return Result{Rows: rows, Truncated: truncated}
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func clip(s string) string {
	r := []rune(s)
	if len(r) <= MaxTitleRunes {
		return s
	}
	return string(r[:MaxTitleRunes-1]) + "…"
}

type constErr string

func (e constErr) Error() string { return string(e) }

// dedupe drops entries repeating the (exe, title) of the previous one; entries are sorted, so the
// lowest PID of each pair is the one kept.
func dedupe(entries []entry) []entry {
	out := entries[:0]
	for _, e := range entries {
		if n := len(out); n > 0 && strings.EqualFold(out[n-1].row.Exe, e.row.Exe) && out[n-1].row.Title == e.row.Title {
			continue
		}
		out = append(out, e)
	}
	return out
}
