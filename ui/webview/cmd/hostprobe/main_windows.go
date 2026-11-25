// hostprobe is a tiny program that imports ui/webview and exercises the host the way the
// final app's -selftest will: page served from https://app.local/ under the CSP, JS->Go
// call, Go->JS events through PostWebMessageAsJson, navigation lock, resource flatness and
// a constant NewCallback count. Flags: -selftest, -selftest-noruntime, -label, -timeout.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"testing/fstest"
	"time"

	"github.com/yanmad27/supcua-v2/ui/webview"
	"github.com/yanmad27/supcua-v2/ui/webview/serve"
)

import _ "embed"

//go:embed app.js
var appJS []byte

type report struct {
	Pong   string `json:"pong"`
	Events int    `json:"events"`
	Cycles int    `json:"cycles"`
	Error  string `json:"error"`
}

func main() {
	selftest := flag.Bool("selftest", false, "")
	noRuntime := flag.Bool("selftest-noruntime", false, "")
	label := flag.String("label", "plain", "")
	timeout := flag.Duration("timeout", 90*time.Second, "")
	flag.Parse()

	if *selftest || *noRuntime {
		webview.AttachParentConsole()
	}
	if *noRuntime {
		os.Exit(webview.RunNoRuntimeSelftest(webview.NewResult(*label, "hostprobe-noruntime"), *timeout))
	}
	res := webview.NewResult(*label, "hostprobe")

	if v, err := webview.CheckRuntime(); err != nil {
		if *selftest {
			res.Step("runtime present", false, err.Error())
			os.Exit(res.Finish(1))
		}
		webview.ShowRuntimeMissing()
		os.Exit(3)
	} else {
		res.RuntimeVersion = v
	}
	if *selftest {
		res.ArmWatchdog(*timeout)
	}

	var ui *webview.UI
	ready := make(chan struct{}, 1)
	reports := make(chan report, 4)
	methods := map[string]func(json.RawMessage) (any, error){
		"ready": func(json.RawMessage) (any, error) {
			select {
			case ready <- struct{}{}:
			default:
			}
			return struct{}{}, nil
		},
		"ping": func(p json.RawMessage) (any, error) {
			var in struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(p, &in)
			return map[string]string{"echo": in.Text, "upper": strings.ToUpper(in.Text)}, nil
		},
		"cycle": func(json.RawMessage) (any, error) {
			ui.Emit("tick", map[string]int{"n": 1})
			done := make(chan struct{})
			ui.Post(func() { close(done) })
			<-done
			return struct{}{}, nil
		},
		"report": func(p json.RawMessage) (any, error) {
			var r report
			_ = json.Unmarshal(p, &r)
			reports <- r
			return struct{}{}, nil
		},
	}
	page := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="%s"></head><body>hostprobe<script src="/app.js"></script></body></html>`, serve.CSP)
	assets := fstest.MapFS{"index.html": {Data: []byte(page)}, "app.js": {Data: appJS}}

	ui, err := webview.New(webview.Config{Title: "hostprobe", Methods: methods, Assets: assets, DarkTitleBar: true, MinW: 400, MinH: 300})
	if err != nil {
		log.Printf("hostprobe: %v", err)
		if *selftest {
			res.Step("startup", false, err.Error())
			os.Exit(res.Finish(1))
		}
		webview.ShowStartupFailure()
		os.Exit(4)
	}
	if *selftest {
		go drive(ui, res, ready, reports)
	}
	ui.Run()
	if *selftest {
		os.Exit(res.Finish(0))
	}
}

func drive(ui *webview.UI, res *webview.Result, ready chan struct{}, reports chan report) {
	fail := func(name, detail string) { res.Step(name, false, detail); ui.RequestQuit() }
	select {
	case <-ready:
		res.Step("page loaded + ready over https://app.local (CSP, JS->Go)", true, "")
	case <-time.After(20 * time.Second):
		fail("page loaded + ready", "timeout")
		return
	}
	run := func(n int) (report, bool) {
		ui.Eval(fmt.Sprintf("__probe.run(%d)", n))
		select {
		case r := <-reports:
			return r, r.Error == ""
		case <-time.After(30 * time.Second):
			return report{Error: "timeout"}, false
		}
	}
	if r, ok := run(3); !ok {
		fail("warm-up", r.Error)
		return
	}
	time.Sleep(500 * time.Millisecond)
	before, cbBefore := webview.SampleResources(), webview.CallbackCount()
	const n = 50
	r, ok := run(n)
	if !ok {
		fail("cycles", r.Error)
		return
	}
	time.Sleep(500 * time.Millisecond)
	after, cbAfter := webview.SampleResources(), webview.CallbackCount()
	res.Step("JS->Go value returned", r.Pong == "PROBE", "js saw "+r.Pong)
	res.Step("Go->JS events via PostWebMessageAsJson", r.Events >= n, fmt.Sprintf("js counted %d ticks", r.Events))
	res.CheckFlat(before, after)
	res.CheckCallbacks(cbBefore, cbAfter, 1) // host window procedure only; the key hook is created on first capture
	res.Step("navigation lock cancels foreign origin", ui.ProbeNavigationLock(5*time.Second), fmt.Sprintf("stats=%+v", ui.Stats()))
	ui.RequestQuit()
}
