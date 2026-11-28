//go:build windows

package ui

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/latency"
	"github.com/yanmad27/supcua-v2/model"
)

const vkRCtrl = 0xA3

// rctrlHold is how long RCtrl stays down in a trial: a quick human tap.
const rctrlHold = 150 * time.Millisecond

type latTrial struct {
	fromDown, fromUp time.Duration
	stage            [5]time.Duration // relative to key-down; 0 = stage did not run
	ok               bool
}

// overlayRect is the overlay's on-screen rectangle (read on the UI thread).
func (s *SelfTest) overlayRect() (x, y, w, h int, ok bool) {
	s.onUI(func() {
		if ow := s.Overlay.Window(); ow != nil {
			hwnd, _ := ow.Handle()
			var r win.RECT
			if win.GetWindowRect(win.HWND(hwnd), &r) {
				x, y, w, h, ok = int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), true
			}
		}
	})
	return
}

// measureRCtrl presses (down), holds rctrlHold, releases (up) and samples the screen until the
// overlay shows `to`. Times are from the key-down / key-up call to the first sample showing it.
func measureRCtrl(down, up func(), x, y, w, h int, to [3]uint8) latTrial {
	var (
		wg       sync.WaitGroup
		seen     time.Duration
		stop     = make(chan struct{})
		t0       time.Duration
		released = make(chan struct{})
		upAt     time.Duration
	)
	latency.Reset()
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-stop:
				return
			default:
			}
			if px, err := captureScreen(x, y, w, h); err == nil && screenHue(overlayObs{screen: px}, to) >= 10 {
				seen = latency.Now()
				return
			}
		}
	}()
	t0 = latency.Now()
	down()
	go func() { time.Sleep(rctrlHold); upAt = latency.Now(); up(); close(released) }()
	wg.Wait()
	close(stop)
	<-released
	tr := latTrial{ok: seen != 0}
	if tr.ok {
		tr.fromDown, tr.fromUp = seen-t0, seen-upAt
	}
	for i, m := range latency.Marks() {
		if m != 0 {
			tr.stage[i] = m - t0
		}
	}
	return tr
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func medMax(v []float64) (med, max float64) {
	if len(v) == 0 {
		return 0, 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	return c[len(c)/2], c[len(c)-1]
}

func summarize(trials []latTrial) (okN int, line string, med, max float64) {
	var fd, fu []float64
	st := make([][]float64, 5)
	for _, t := range trials {
		if !t.ok {
			continue
		}
		okN++
		fd, fu = append(fd, ms(t.fromDown)), append(fu, ms(t.fromUp))
		for i, d := range t.stage {
			if d != 0 {
				st[i] = append(st[i], ms(d))
			}
		}
	}
	dm, dx := medMax(fd)
	um, ux := medMax(fu)
	names := []string{"detect", "state", "publish", "refresh", "present"}
	var sb strings.Builder
	for i, n := range names {
		m, x := medMax(st[i])
		fmt.Fprintf(&sb, " %s=%.1f/%.1f", n, m, x)
	}
	return okN, fmt.Sprintf("n=%d/%d fromDown median=%.1fms max=%.1fms fromUp median=%.1fms max=%.1fms | stages(ms after key-down, median/max):%s",
		okN, len(trials), dm, dx, um, ux, sb.String()), dm, dx
}

// rctrlLatency measures key-down→on-screen overlay latency for the three RCtrl uses on the
// real path. It leaves the global state active, the probe script stopped and no extra hotkeys.
func (s *SelfTest) rctrlLatency(h ProductionHooks, step func(string, bool, string) bool) {
	if h.Register == nil || s.Overlay == nil {
		return
	}
	latency.Enable()
	hotkey.SetAcceptInjected(true)
	defer hotkey.SetAcceptInjected(false)
	s.onUI(func() { s.Overlay.ApplyStyle(model.OverlayStyle{}, 260, 180) })
	time.Sleep(300 * time.Millisecond)
	x, y, w, hh, ok := s.overlayRect()
	if !step("latency: overlay rect", ok && w > 0 && hh > 0, fmt.Sprintf("%d,%d %dx%d", x, y, w, hh)) {
		return
	}
	rctrl := model.Hotkey{Key: "rctrl"}
	s.pollCPU(h, step)
	keyDown := func() { _ = SendKey(vkRCtrl, false) }
	keyUp := func() { _ = SendKey(vkRCtrl, true) }
	defer keyUp()
	settle := func() { time.Sleep(500 * time.Millisecond) }

	const n = 10
	run := func(name string, trigDown, trigUp func(), prep func(i int) ([3]uint8, bool)) {
		var trials []latTrial
		for i := 0; i < n; i++ {
			to, ok := prep(i)
			if !ok {
				break
			}
			trials = append(trials, measureRCtrl(trigDown, trigUp, x, y, w, hh, to))
			settle()
		}
		okN, line, med, max := summarize(trials)
		// The on-screen time includes this sampler's BitBlt/DWM floor (~30 ms measured on the
		// emergency path, which has no polling), so 60 ms median / 100 ms max from key-down keeps
		// "about 50 ms" with CI jitter headroom.
		step("latency: "+name, okN == len(trials) && okN > 0 && med <= 60 && max <= 100, line)
	}
	toggleTo := func(i int) ([3]uint8, bool) {
		if i%2 == 0 {
			return colorRedInactive, h.GlobalActive()
		}
		return colorGrayIdle, !h.GlobalActive()
	}

	id, err := h.Register(rctrl, h.ToggleGlobal, h.PreviewToggle)
	if step("latency: register rctrl global hotkey", err == nil, fmt.Sprint(err)) {
		run("(a) global hotkey RCtrl DISABLE<->IDLE", keyDown, keyUp, toggleTo)
		s.rctrlCombo(h, step, x, y, w, hh)
		h.Unregister(id)
		if !h.GlobalActive() {
			_ = SendHotkey(h.GlobalHotkey)
		}
	}

	id, err = h.Register(rctrl, func(epoch uint64) {
		if h.RunningScriptID() != "" {
			h.StopRunning(epoch)
		}
	}, h.PreviewStop)
	if step("latency: register rctrl stop hotkey", err == nil, fmt.Sprint(err)) {
		run("(b) RCtrl stops a running script", keyDown, keyUp, func(int) ([3]uint8, bool) {
			if h.RunningScriptID() == "" {
				_ = SendHotkey(h.ProbeHotkey)
				waitFor(4*time.Second, func() bool { return h.RunningScriptID() != "" })
			}
			time.Sleep(300 * time.Millisecond)
			return colorGrayIdle, h.RunningScriptID() != ""
		})
		h.Unregister(id)
		waitFor(4*time.Second, func() bool { return h.RunningScriptID() == "" })
	}

	if h.PhysicalRCtrl != nil {
		run("(c) emergency toggle (keyboard-lock LL hook, simulated physical RCtrl)",
			func() { h.PhysicalRCtrl(true) }, func() { h.PhysicalRCtrl(false) }, toggleTo)
		if !h.GlobalActive() {
			_ = SendHotkey(h.GlobalHotkey)
		}
	}
	step("latency: global active restored", waitFor(4*time.Second, h.GlobalActive), "")
}

// rctrlCombo checks the real path for RCtrl+C: the overlay previews on RCtrl down, returns to the
// real state when C goes down, and the release does not toggle the global state.
func (s *SelfTest) rctrlCombo(h ProductionHooks, step func(string, bool, string) bool, x, y, w, hh int) {
	if !waitFor(4*time.Second, h.GlobalActive) {
		step("latency: combo precondition (global active)", false, "")
		return
	}
	showing := func(rgb [3]uint8) bool {
		px, err := captureScreen(x, y, w, hh)
		return err == nil && screenHue(overlayObs{screen: px}, rgb) >= 10
	}
	defer SendKey(vkRCtrl, true)
	_ = SendKey(vkRCtrl, false)
	previewed := waitFor(time.Second, func() bool { return showing(colorRedInactive) })
	_ = SendKey(0x43, false)
	_ = SendKey(0x43, true)
	reverted := waitFor(time.Second, func() bool { return showing(colorGrayIdle) })
	time.Sleep(150 * time.Millisecond)
	_ = SendKey(vkRCtrl, true)
	time.Sleep(500 * time.Millisecond)
	step("latency: RCtrl+C previews DISABLE, reverts to IDLE and does not toggle", previewed && reverted && h.GlobalActive(),
		fmt.Sprintf("previewed=%v reverted=%v globalActive=%v", previewed, reverted, h.GlobalActive()))
}
