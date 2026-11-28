//go:build windows

package ui

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"

	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
)

func processCPU() time.Duration {
	var c, e, k, u windows.Filetime
	if windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u) != nil {
		return 0
	}
	return time.Duration(k.Nanoseconds()+u.Nanoseconds()) * time.Nanosecond
}

// pollCPU compares the idle process CPU of the old 10 ms and the current poll interval with a bare
// modifier registered (so the listener really polls), three rounds each, alternating.
func (s *SelfTest) pollCPU(h ProductionHooks, step func(string, bool, string) bool) {
	id, err := h.Register(model.Hotkey{Key: "rctrl"}, func(uint64) {})
	if err != nil {
		step("cpu: register", false, err.Error())
		return
	}
	defer h.Unregister(id)
	defer hotkey.SetPollInterval(hotkey.DefaultPollInterval)
	const win = 4 * time.Second
	cpu := map[time.Duration][]float64{}
	for r := 0; r < 3; r++ {
		for _, iv := range []time.Duration{10 * time.Millisecond, 4 * time.Millisecond} {
			hotkey.SetPollInterval(iv)
			time.Sleep(300 * time.Millisecond)
			c0, t0 := processCPU(), time.Now()
			time.Sleep(win)
			cpu[iv] = append(cpu[iv], float64(processCPU()-c0)/float64(time.Since(t0))*100)
		}
	}
	step("cpu: idle process CPU, poll 10ms vs 4ms", true,
		fmt.Sprintf("10ms=%.3f%% %s=%.3f%% (percent of one core, 3 rounds each: %.3f vs %.3f)",
			avg(cpu[10*time.Millisecond]), "4ms", avg(cpu[4*time.Millisecond]),
			cpu[10*time.Millisecond], cpu[4*time.Millisecond]))
}

func avg(v []float64) float64 {
	t := 0.0
	for _, x := range v {
		t += x
	}
	return t / float64(len(v))
}
