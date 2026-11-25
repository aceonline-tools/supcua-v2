package ui

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/yanmad27/supcua-v2/bridge"
)

const (
	maxDiagMessage = 500
	maxDiagParams  = 8 << 10 // raw params bound, checked before decoding
	maxDiagLogs    = 50      // per process: a crash loop must not flood the log
)

// Diag serves the frontend's two diagnostics RPCs: app.mounted (the app rendered) and app.log
// (forwarded window errors, rejected promises and failed script loads). Both are pure
// reporting channels with bounded input and no effect besides a log line and the mount hook.
type Diag struct {
	Logf      func(format string, args ...any)
	OnMounted func(route string) // first call only

	mu      sync.Mutex
	mounted bool
	logs    int
}

type handler = func(json.RawMessage) (any, error)

func badRequest() error { return &bridge.Error{Code: "BAD_REQUEST", Message: "invalid request"} }

func strictDecode(p json.RawMessage, v any) error {
	if len(p) > maxDiagParams {
		return errors.New("params too large")
	}
	d := json.NewDecoder(strings.NewReader(string(p)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

// clip keeps at most n bytes without splitting a rune, in one pass.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := 0
	for i := range s {
		if i > n {
			break
		}
		cut = i
	}
	return s[:cut] + "..."
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// Methods returns the RPC handlers to merge into the method table.
func (d *Diag) Methods() map[string]handler {
	return map[string]handler{"app.mounted": d.handleMounted, "app.log": d.handleLog}
}

func (d *Diag) handleMounted(p json.RawMessage) (any, error) {
	var in struct {
		Route string `json:"route"`
	}
	if err := strictDecode(p, &in); err != nil || in.Route == "" || len(in.Route) > 200 {
		return nil, badRequest()
	}
	d.mu.Lock()
	first := !d.mounted
	d.mounted = true
	d.mu.Unlock()
	if first && d.OnMounted != nil {
		d.OnMounted(sanitize(in.Route))
	}
	return struct{}{}, nil
}

func (d *Diag) handleLog(p json.RawMessage) (any, error) {
	var in struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	if err := strictDecode(p, &in); err != nil || in.Message == "" {
		return nil, badRequest()
	}
	switch in.Level {
	case "error", "warn", "info":
	default:
		return nil, badRequest()
	}
	d.mu.Lock()
	d.logs++
	n := d.logs
	d.mu.Unlock()
	switch {
	case n == maxDiagLogs+1:
		d.Logf("frontend[log]: further frontend log messages suppressed")
	case n <= maxDiagLogs:
		d.Logf("frontend[%s]: %s", in.Level, sanitize(clip(in.Message, maxDiagMessage)))
	}
	return struct{}{}, nil
}
