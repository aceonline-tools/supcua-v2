package ui

import (
	"sync"
	"time"
)

// Stages records startup progress as one short log line per stage, so a stall is attributable
// to the last stage reached.
type Stages struct {
	Logf func(format string, args ...any)

	mu    sync.Mutex
	start time.Time
	last  string
}

func NewStages(logf func(string, ...any)) *Stages {
	return &Stages{Logf: logf, start: time.Now()}
}

// Mark logs "stage: <name> (+<ms>ms)" and remembers it as the last stage reached.
func (s *Stages) Mark(name string) {
	s.mu.Lock()
	s.last = name
	ms := time.Since(s.start).Milliseconds()
	s.mu.Unlock()
	s.Logf("stage: %s (+%dms)", name, ms)
}

// Last is the most recent stage, or "" before the first.
func (s *Stages) Last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}
