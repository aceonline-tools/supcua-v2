//go:build windows

package ui

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"

	"github.com/yanmad27/supcua-v2/model"
)

func processCPU() time.Duration {
	var c, e, k, u windows.Filetime
	if windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u) != nil {
		return 0
	}
	return time.Duration(k.Nanoseconds()+u.Nanoseconds()) * time.Nanosecond
}

// pollCPU reports the idle process CPU with a bare modifier registered (the poll loop and the key
// hook both live), 5 rounds of 4 s. The process-time clock ticks every ~15.6 ms, so single rounds
// are quantised; compare the mean with the same figure from earlier runs.
func (s *SelfTest) pollCPU(h ProductionHooks, step func(string, bool, string) bool) {
	id, err := h.Register(model.Hotkey{Key: "rctrl"}, func(uint64) {}, nil)
	if err != nil {
		step("cpu: register", false, err.Error())
		return
	}
	defer h.Unregister(id)
	time.Sleep(300 * time.Millisecond)
	var v []float64
	for r := 0; r < 5; r++ {
		c0, t0 := processCPU(), time.Now()
		time.Sleep(4 * time.Second)
		v = append(v, float64(processCPU()-c0)/float64(time.Since(t0))*100)
	}
	t := 0.0
	for _, x := range v {
		t += x
	}
	step("cpu: idle process CPU with bare-modifier poll + key hook", true, fmt.Sprintf("mean=%.3f%% of one core, rounds=%.3f", t/float64(len(v)), v))
}
