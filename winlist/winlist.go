// Package winlist lists the running processes that own a user-visible top-level window, for the
// auto-active process picker. It reads executable base names and window titles only.
package winlist

import (
	"sort"
	"strings"
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
	// Windows calls visit for each top-level window until visit returns false.
	Windows(visit func(Window) bool)
	// Title returns the window title. It is only asked for windows that passed every cheap filter.
	Title(h uintptr) string
	// ExeName returns the executable base name; an error means the process is inaccessible or gone.
	ExeName(pid uint32) (string, error)
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

// List builds the picker rows from src, excluding selfPID. It stops enumerating once budget has
// elapsed on clock and then reports Truncated with whatever it has.
func List(src Source, selfPID uint32, budget time.Duration, clock func() time.Time) Result {
	if clock == nil {
		clock = time.Now
	}
	deadline := clock().Add(budget)
	truncated := false
	// expired is checked before every native call; once it trips the run is partial.
	expired := func() bool {
		if !clock().Before(deadline) {
			truncated = true
		}
		return truncated
	}
	exeCache := map[uint32]string{}
	bad := map[uint32]bool{}
	var entries []entry

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
			name, err := src.ExeName(w.PID)
			if err != nil || strings.TrimSpace(name) == "" {
				bad[w.PID] = true
				return !expired()
			}
			exe = baseName(name)
			exeCache[w.PID] = exe
		}
		entries = append(entries, entry{Row{Exe: exe, Title: clip(title)}, w.PID})
		return !expired()
	})
	expired()

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
