package webview

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jchv/go-webview2/pkg/edge"
)

// Self-test building blocks for the final app's `-selftest` / `-selftest-noruntime`.
// The app drives the page (e.g. a window.__selftest harness), samples Resources before and
// after N dialog cycles, and records every step here; Finish prints one JSON line and
// writes <exe>.<mode>.json next to the executable.

const (
	GDITolerance    = 5
	UserTolerance   = 5
	HandleTolerance = 15
)

type StepResult struct {
	AtMs   int64  `json:"at_ms"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type Result struct {
	mu             sync.Mutex
	started        time.Time
	written        bool
	Pass           bool         `json:"pass"`
	Mode           string       `json:"mode"`
	Label          string       `json:"label"`
	RuntimeVersion string       `json:"runtime_version,omitempty"`
	Steps          []StepResult `json:"steps"`
	Before         *Resources   `json:"resources_before,omitempty"`
	After          *Resources   `json:"resources_after,omitempty"`
	Callbacks      []int32      `json:"own_callbacks_before_after,omitempty"`
	Message        string       `json:"missing_runtime_message,omitempty"`
	DurationMs     int64        `json:"duration_ms"`
}

func NewResult(label, mode string) *Result {
	return &Result{started: time.Now(), Label: label, Mode: mode}
}

// Step records one check with its elapsed time (also logged, so a hang names its step).
func (r *Result) Step(name string, ok bool, detail string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	ms := time.Since(r.started).Milliseconds()
	r.Steps = append(r.Steps, StepResult{ms, name, ok, detail})
	log.Printf("selftest +%dms %s ok=%v %s", ms, name, ok, detail)
	return ok
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// CheckFlat records the GDI/USER/handle flatness steps; returns true if all are within tolerance.
func (r *Result) CheckFlat(before, after Resources) bool {
	r.mu.Lock()
	r.Before, r.After = &before, &after
	r.mu.Unlock()
	a := r.Step("GDI objects flat", absDiff(before.GDI, after.GDI) <= GDITolerance, fmt.Sprintf("%d -> %d (tol %d)", before.GDI, after.GDI, GDITolerance))
	b := r.Step("USER objects flat", absDiff(before.User, after.User) <= UserTolerance, fmt.Sprintf("%d -> %d (tol %d)", before.User, after.User, UserTolerance))
	c := r.Step("process handles flat", absDiff(before.Handles, after.Handles) <= HandleTolerance, fmt.Sprintf("%d -> %d (tol %d)", before.Handles, after.Handles, HandleTolerance))
	return a && b && c
}

// CheckCallbacks records that NewCallback calls did not grow and equal the expected constant.
func (r *Result) CheckCallbacks(before, after, expected int32) bool {
	r.mu.Lock()
	r.Callbacks = []int32{before, after}
	r.mu.Unlock()
	return r.Step("NewCallback count unchanged", before == after && after == expected, fmt.Sprintf("own calls %d -> %d (expected const %d)", before, after, expected))
}

// Finish writes the result and returns the exit code (0 only if code==0 and every step passed).
func (r *Result) Finish(code int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.written {
		return code
	}
	r.written = true
	r.Pass = code == 0 && len(r.Steps) > 0
	for _, s := range r.Steps {
		if !s.OK {
			r.Pass = false
		}
	}
	r.DurationMs = time.Since(r.started).Milliseconds()
	b, _ := json.Marshal(r)
	fmt.Fprintln(os.Stdout, string(b))
	if exe, err := os.Executable(); err == nil {
		name := strings.TrimSuffix(filepath.Base(exe), ".exe") + "." + r.Mode + ".json"
		_ = os.WriteFile(filepath.Join(filepath.Dir(exe), name), append(b, '\n'), 0o644)
	}
	if !r.Pass && code == 0 {
		return 1
	}
	return code
}

// ArmWatchdog fails the run (exit 2) if it has not finished within d (hung message loop).
func (r *Result) ArmWatchdog(d time.Duration) {
	time.AfterFunc(d, func() {
		r.Step("watchdog", false, fmt.Sprintf("no completion within %s (hung message loop?)", d))
		os.Exit(r.Finish(2))
	})
}

// RunNoRuntimeSelftest forces the missing-runtime path without admin rights (empty
// WEBVIEW2_BROWSER_EXECUTABLE_FOLDER), records the message instead of showing a MessageBox,
// and checks that WebView2 creation fails fast. Returns the process exit code.
func RunNoRuntimeSelftest(r *Result, timeout time.Duration) int {
	r.ArmWatchdog(timeout)
	dir, err := os.MkdirTemp("", "supcua-noruntime-")
	if err != nil {
		r.Step("temp dir", false, err.Error())
		return r.Finish(1)
	}
	defer os.RemoveAll(dir)
	os.Setenv(runtimeFolderEnv, dir)

	v, derr := CheckRuntime()
	r.Step("detection reports missing", v == "" && derr == ErrRuntimeMissing, fmt.Sprintf("version=%q err=%v", v, derr))
	msg := RuntimeMissingText()
	r.Message = msg
	r.Step("message contains Evergreen installer link", strings.Contains(msg, RuntimeInstallerURL), RuntimeInstallerURL)

	hwnd, err := createHostWindow("noruntime", Bounds{}, nil)
	if err != nil {
		r.Step("host window", false, err.Error())
		return r.Finish(1)
	}
	b := edge.NewChromium()
	t0 := time.Now()
	err = b.EmbedTimeout(uintptr(hwnd), 20*time.Second)
	r.Step("WebView2 creation fails fast", err != nil && time.Since(t0) < 20*time.Second, fmt.Sprintf("err=%v in %s", err, time.Since(t0).Round(time.Millisecond)))
	return r.Finish(0)
}
