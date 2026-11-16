package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lxn/walk"

	"spike/rpc"
	"spike/status"
)

//go:embed web/index.html
var indexHTML string

// The UI thread is the main goroutine's OS thread: it owns the walk message
// loop, the tray, the WebView2 host and the overlay. Lock it before anything runs.
func init() { runtime.LockOSThread() }

type app struct {
	mw   *walk.MainWindow
	ni   *walk.NotifyIcon
	h    *host
	ov   *overlay
	disp *rpc.Dispatcher
	hub  status.Hub

	selftest    bool
	ready       chan struct{}
	readyOnce   atomic.Bool
	pingSeen    chan string
	reportCh    chan rpc.Report
	dialogOpens atomic.Int64
	trayGone    atomic.Bool
	exiting     atomic.Bool
	cancel      context.CancelFunc
}

func main() {
	selftest := flag.Bool("selftest", false, "headless self-test; exit 0 on PASS")
	noRuntime := flag.Bool("selftest-noruntime", false, "self-test the missing-runtime path")
	label := flag.String("label", "plain", "build label echoed in the result")
	timeout := flag.Duration("timeout", 90*time.Second, "hard self-test timeout")
	flag.Parse()

	logf, _ := os.Create(filepath.Join(exeDir(), "spike.log"))
	if logf != nil {
		log.SetOutput(logf)
	}

	if *selftest || *noRuntime {
		attachParentConsole()
	}
	res := newResult(*label, map[bool]string{true: "selftest-noruntime", false: "selftest"}[*noRuntime])

	if *noRuntime {
		os.Exit(runNoRuntimeSelftest(res, *timeout))
	}

	ver, err := detectRuntime()
	if err != nil || ver == "" {
		log.Printf("webview2 runtime missing: version=%q err=%v", ver, err)
		if *selftest {
			res.step("runtime present", false, fmt.Sprintf("version=%q err=%v", ver, err))
			os.Exit(res.finish(1))
		}
		showMissingRuntime()
		os.Exit(3)
	}
	res.RuntimeVersion = ver

	if *selftest {
		res.armWatchdog(*timeout)
	}
	a, err := newApp(*selftest)
	if err != nil {
		log.Printf("startup failed: %v", err)
		if *selftest {
			res.step("startup", false, err.Error())
			os.Exit(res.finish(1))
		}
		showMissingRuntime() // Embed failure is also a runtime problem (corrupt/blocked install)
		os.Exit(4)
	}
	if *selftest {
		go a.runSelftest(res)
	}
	a.mw.Run() // walk pump: tray + Synchronize + WebView2 messages
	a.cancel()
	a.disp.Close()
	if *selftest {
		os.Exit(res.finish(0))
	}
}

func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(p)
}

func newApp(selftest bool) (*app, error) {
	a := &app{
		selftest: selftest,
		ready:    make(chan struct{}),
		pingSeen: make(chan string, 4),
		reportCh: make(chan rpc.Report, 4),
	}
	var err error
	if a.mw, err = walk.NewMainWindow(); err != nil { // hidden: message owner only
		return nil, err
	}
	if err = a.initTray(); err != nil {
		return nil, fmt.Errorf("tray: %w", err)
	}
	if a.h, err = newHost("supcua WebView2 spike"); err != nil {
		return nil, err
	}
	a.h.onClose = a.quit
	a.initRPC()
	if err = a.h.attachWebView(a.disp.Submit, func() {}); err != nil {
		return nil, err
	}
	a.h.browser.NavigateToString(indexHTML)
	a.h.Show()

	if a.ov, err = newOverlay(10, 10); err != nil {
		return nil, err
	}

	// One status dispatcher fans out to frontend, tray and overlay.
	a.hub.Subscribe(func(s rpc.Status) {
		b, _ := json.Marshal(s)
		a.onUI(func() { a.h.browser.Eval("window.__emit('status'," + string(b) + ")") })
	})
	a.hub.Subscribe(func(s rpc.Status) {
		a.onUI(func() {
			if !a.trayGone.Load() {
				_ = a.ni.SetToolTip(fmt.Sprintf("spike - tick %d", s.Tick))
			}
		})
	})
	a.hub.Subscribe(func(s rpc.Status) {
		a.onUI(func() { a.ov.Refresh(fmt.Sprintf("TICK %d", s.Tick)) })
	})

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	period := time.Second
	if selftest {
		period = 150 * time.Millisecond
	}
	go func() {
		t := time.NewTicker(period)
		defer t.Stop()
		for n := 1; ; n++ {
			a.hub.Publish(rpc.Status{Active: true, Running: "", Tick: n})
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return a, nil
}

func (a *app) initTray() error {
	ni, err := walk.NewNotifyIcon(a.mw)
	if err != nil {
		return err
	}
	a.ni = ni
	for _, id := range []int{1, 2, 3, 7} {
		if ico, err := walk.NewIconFromResourceId(id); err == nil {
			_ = ni.SetIcon(ico)
			break
		}
	}
	_ = ni.SetToolTip("spike")
	add := func(text string, fn func()) {
		act := walk.NewAction()
		_ = act.SetText(text)
		act.Triggered().Attach(fn)
		_ = ni.ContextMenu().Actions().Add(act)
	}
	add("Show", func() { a.h.Show() })
	add("Hide", func() { a.h.Hide() })
	add("Exit", func() { a.quit() })
	ni.MouseDown().Attach(func(_, _ int, b walk.MouseButton) {
		if b == walk.LeftButton {
			if a.h.Visible() {
				a.h.Hide()
			} else {
				a.h.Show()
			}
		}
	})
	return ni.SetVisible(true)
}

func (a *app) disposeTray() {
	if a.trayGone.CompareAndSwap(false, true) {
		a.ni.Dispose()
	}
}

// quit tears everything down on the UI thread and ends the walk loop.
func (a *app) quit() {
	if !a.exiting.CompareAndSwap(false, true) {
		return
	}
	a.disposeTray()
	if a.ov != nil {
		a.ov.Destroy()
	}
	a.h.Destroy()
	a.mw.Close()
}

// onUI posts f to the UI thread (non-blocking).
func (a *app) onUI(f func()) { a.mw.Synchronize(f) }

// callUI runs f on the UI thread and waits. Never call it from the UI thread.
func (a *app) callUI(f func()) error {
	done := make(chan struct{})
	a.mw.Synchronize(func() { f(); close(done) })
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("UI thread did not respond")
	}
}

func (a *app) initRPC() {
	d := rpc.NewDispatcher(64)
	var seq int
	rpc.Register(d, "app.ready", func(struct{}) (struct{}, error) {
		if a.readyOnce.CompareAndSwap(false, true) {
			close(a.ready)
		}
		return struct{}{}, nil
	})
	rpc.Register(d, "app.ping", func(p rpc.PingParams) (rpc.PingResult, error) {
		seq++
		if p.Text == "selftest" {
			select {
			case a.pingSeen <- strings.ToUpper(p.Text):
			default:
			}
		}
		return rpc.PingResult{Echo: p.Text, Upper: strings.ToUpper(p.Text), Seq: seq}, nil
	})
	rpc.Register(d, "overlay.setVisible", func(p struct {
		Visible bool `json:"visible"`
	}) (rpc.OverlayState, error) {
		var st rpc.OverlayState
		err := a.callUI(func() {
			a.ov.SetVisible(p.Visible)
			st = rpc.OverlayState{Visible: a.ov.visible, Draggable: a.ov.draggable}
		})
		return st, err
	})
	rpc.Register(d, "overlay.setDraggable", func(p rpc.OverlayParams) (rpc.OverlayState, error) {
		var st rpc.OverlayState
		err := a.callUI(func() {
			a.ov.SetDraggable(p.Draggable)
			st = rpc.OverlayState{Visible: a.ov.visible, Draggable: a.ov.draggable}
		})
		return st, err
	})
	rpc.Register(d, "window.hide", func(struct{}) (struct{}, error) {
		a.onUI(a.h.Hide)
		return struct{}{}, nil
	})
	rpc.Register(d, "app.exit", func(struct{}) (struct{}, error) {
		a.onUI(a.quit)
		return struct{}{}, nil
	})
	rpc.Register(d, "dialog.open", func(p rpc.DialogParams) (struct{}, error) {
		a.dialogOpens.Add(1)
		return struct{}{}, a.callUI(func() { a.ov.Refresh(fmt.Sprintf("DLG %d", p.Cycle)) })
	})
	rpc.Register(d, "dialog.close", func(p rpc.DialogParams) (struct{}, error) {
		return struct{}{}, a.callUI(func() { a.ov.Refresh("IDLE") })
	})
	rpc.Register(d, "selftest.report", func(r rpc.Report) (struct{}, error) {
		select {
		case a.reportCh <- r:
		default:
		}
		return struct{}{}, nil
	})
	d.Start(func(b []byte) {
		a.onUI(func() { a.h.browser.Eval("window.__resolve(" + string(b) + ")") })
	})
	a.disp = d
}
