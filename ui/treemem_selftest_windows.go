//go:build windows

package ui

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

type procMemCounters struct {
	cb, pageFaults                                       uint32
	peakWS, ws, qPeakPP, qPP, qPeakNP, qNP, pagefile, pk uintptr
	privateUsage                                         uintptr
}

// treeMem sums private bytes and working set over this process and all its descendants
// (msedgewebview2 browser, renderer, GPU and utility processes included).
func treeMem() (privateMB, wsMB float64, procs int, err error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	defer windows.CloseHandle(snap)
	parent := map[uint32]uint32{}
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for e := windows.Process32First(snap, &pe); e == nil; e = windows.Process32Next(snap, &pe) {
		parent[pe.ProcessID] = pe.ParentProcessID
	}
	self := uint32(os.Getpid())
	var priv, ws uint64
	for pid := range parent {
		p, in := pid, pid == self
		for i := 0; !in && i < 12; i++ {
			pp, ok := parent[p]
			if !ok || pp == 0 {
				break
			}
			p, in = pp, pp == self
		}
		if !in {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			continue
		}
		var c procMemCounters
		c.cb = uint32(unsafe.Sizeof(c))
		if r, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)); r != 0 {
			priv += uint64(c.privateUsage)
			ws += uint64(c.ws)
			procs++
		}
		windows.CloseHandle(h)
	}
	return float64(priv) / (1 << 20), float64(ws) / (1 << 20), procs, nil
}

func memLine(label string) string {
	p, w, n, err := treeMem()
	if err != nil {
		return fmt.Sprintf("mem %s: error %v", label, err)
	}
	return fmt.Sprintf("mem %s: private=%.1fMB workingset=%.1fMB processes=%d", label, p, w, n)
}
