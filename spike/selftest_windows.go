package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/lxn/win"
)

const (
	warmupCycles = 3
	dialogCycles = 50
	// tolerances for "flat": a real per-open leak shows up as >= dialogCycles.
	gdiTolerance    = 5
	userTolerance   = 5
	handleTolerance = 15
	// our own NewCallback call sites: hostProc, overlayProc.
	expectedOwnCallbacks = 2
)

type stepResult struct {
	AtMs   int64  `json:"at_ms"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type result struct {
	mu             sync.Mutex
	started        time.Time
	Pass           bool         `json:"pass"`
	Mode           string       `json:"mode"`
	Label          string       `json:"label"`
	RuntimeVersion string       `json:"runtime_version,omitempty"`
	Steps          []stepResult `json:"steps"`
	Before         *resources   `json:"resources_before,omitempty"`
	After          *resources   `json:"resources_after,omitempty"`
	Cycles         int          `json:"cycles,omitempty"`
	OwnCallbacks   []int32      `json:"own_callbacks_before_after,omitempty"`
	Message        string       `json:"missing_runtime_message,omitempty"`
	DurationMs     int64        `json:"duration_ms"`
	written        bool
}

func newResult(label, mode string) *result {
	return &result{started: time.Now(), Label: label, Mode: mode}
}

func (r *result) step(name string, ok bool, detail string) bool {
	r.mu.Lock()
	r.Steps = append(r.Steps, stepResult{time.Since(r.started).Milliseconds(), name, ok, detail})
	log.Printf("selftest +%dms %s ok=%v %s", time.Since(r.started).Milliseconds(), name, ok, detail)
	r.mu.Unlock()
	return ok
}

// finish computes PASS, writes the JSON line to stdout and a file next to the exe,
// and returns the process exit code (0 PASS, else code or 1).
func (r *result) finish(code int) int {
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
	exe, _ := os.Executable()
	_ = os.WriteFile(filepath.Join(filepath.Dir(exe), strings.TrimSuffix(filepath.Base(exe), ".exe")+"."+r.Mode+".json"), append(b, '\n'), 0o644)
	if !r.Pass && code == 0 {
		return 1
	}
	return code
}

// armWatchdog fails the run if the message loop hangs.
func (r *result) armWatchdog(d time.Duration) {
	time.AfterFunc(d, func() {
		r.step("watchdog", false, fmt.Sprintf("no completion within %s (hung message loop?)", d))
		os.Exit(r.finish(2))
	})
}

func (a *app) runSelftest(res *result) {
	fail := func(name, detail string) {
		res.step(name, false, detail)
		a.onUI(a.quit)
	}
	wait := func(name string, ch <-chan struct{}, d time.Duration) bool {
		select {
		case <-ch:
			return true
		case <-time.After(d):
			fail(name, "timeout")
			return false
		}
	}
	evalJS := func(js string) { a.onUI(func() { a.h.browser.Eval(js) }) }

	if !wait("page loaded + app.ready (JS->Go)", a.ready, 20*time.Second) {
		return
	}
	res.step("page loaded + app.ready (JS->Go)", true, "")

	evalJS("__selftest.ping()")
	select {
	case v := <-a.pingSeen:
		res.step("JS->Go call returns value", v == "SELFTEST", "go computed "+v)
	case <-time.After(10 * time.Second):
		fail("JS->Go call returns value", "timeout")
		return
	}

	type cyc struct {
		ping   string
		events int
		cycles int
		err    string
	}
	runCycles := func(n int) (bool, cyc) {
		var rep cyc
		evalJS(fmt.Sprintf("__selftest.cycles(%d)", n))
		select {
		case r := <-a.reportCh:
			return true, cyc{r.Ping, r.Events, r.Cycles, r.Error}
		case <-time.After(30 * time.Second):
			return false, rep
		}
	}

	ok, wrep := runCycles(warmupCycles)
	if !ok || wrep.err != "" {
		fail("warm-up dialog cycles", "timeout/err: "+wrep.err)
		return
	}
	res.step("warm-up dialog cycles", true, fmt.Sprintf("%d cycles", wrep.cycles))
	runtime.GC()
	time.Sleep(500 * time.Millisecond)
	before := sampleResources()
	cbBefore := callbackCount.Load()
	paintsBefore := a.ov.paints.Load()

	ok, rep := runCycles(dialogCycles)
	if !ok || rep.err != "" {
		fail("dialog cycles", "timeout/err: "+rep.err)
		return
	}
	runtime.GC()
	time.Sleep(500 * time.Millisecond)
	after := sampleResources()
	cbAfter := callbackCount.Load()
	res.Before, res.After, res.Cycles = &before, &after, dialogCycles
	res.OwnCallbacks = []int32{cbBefore, cbAfter}

	res.step("page value returned to JS", rep.ping == "SELFTEST", "js saw "+rep.ping)
	res.step("Go->JS events received", rep.events >= 3, fmt.Sprintf("js counted %d status events", rep.events))
	res.step("dialog open/close cycles", rep.cycles == dialogCycles && a.dialogOpens.Load() >= dialogCycles+warmupCycles,
		fmt.Sprintf("js=%d go_opens=%d", rep.cycles, a.dialogOpens.Load()))
	res.step("GDI objects flat", delta(before.GDI, after.GDI) <= gdiTolerance, fmt.Sprintf("%d -> %d (tol %d)", before.GDI, after.GDI, gdiTolerance))
	res.step("USER objects flat", delta(before.User, after.User) <= userTolerance, fmt.Sprintf("%d -> %d (tol %d)", before.User, after.User, userTolerance))
	res.step("process handles flat", delta(before.Handles, after.Handles) <= handleTolerance, fmt.Sprintf("%d -> %d (tol %d)", before.Handles, after.Handles, handleTolerance))
	res.step("NewCallback count unchanged", cbBefore == cbAfter && cbAfter == expectedOwnCallbacks,
		fmt.Sprintf("own calls %d -> %d (expected const %d)", cbBefore, cbAfter, expectedOwnCallbacks))

	_ = a.callUI(func() {
		ex := win.GetWindowLong(a.ov.hwnd, win.GWL_EXSTYLE)
		want := int32(win.WS_EX_TOPMOST | win.WS_EX_LAYERED)
		res.step("overlay on UI thread, layered+topmost",
			a.ov.threadID == win.GetCurrentThreadId() && ex&want == want,
			fmt.Sprintf("thread %d==%d exstyle=%#x", a.ov.threadID, win.GetCurrentThreadId(), ex))
		res.step("overlay repainted", a.ov.paints.Load()-paintsBefore >= dialogCycles,
			fmt.Sprintf("%d WM_PAINT during %d cycles", a.ov.paints.Load()-paintsBefore, dialogCycles))
	})

	trayErr := a.callUI(a.disposeTray)
	res.step("tray created and removed", trayErr == nil && a.trayGone.Load(), "")

	a.onUI(a.quit)
}

func delta(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// runNoRuntimeSelftest forces the missing-runtime path without admin rights by
// pointing WEBVIEW2_BROWSER_EXECUTABLE_FOLDER at an empty directory. The message
// is recorded instead of shown so nothing blocks.
func runNoRuntimeSelftest(res *result, timeout time.Duration) int {
	res.armWatchdog(timeout)
	dir, err := os.MkdirTemp("", "spike-noruntime-")
	if err != nil {
		res.step("temp dir", false, err.Error())
		return res.finish(1)
	}
	defer os.RemoveAll(dir)
	os.Setenv(runtimeFolderEnv, dir)

	ver, derr := detectRuntime()
	res.step("detection reports missing", ver == "", fmt.Sprintf("version=%q err=%v", ver, derr))

	msg := missingRuntimeText()
	res.Message = msg
	res.step("message contains Evergreen installer link", strings.Contains(msg, runtimeInstallerURL), runtimeInstallerURL)

	// The real loader must also fail fast (not hang) when pointed at the empty folder.
	h, err := newHost("noruntime")
	if err != nil {
		res.step("host window", false, err.Error())
		return res.finish(1)
	}
	t0 := time.Now()
	err = h.attachWebView(func(string) {}, func() {})
	res.step("WebView2 creation fails fast", err != nil && time.Since(t0) < 20*time.Second, fmt.Sprintf("err=%v in %s", err, time.Since(t0).Round(time.Millisecond)))
	return res.finish(0)
}
