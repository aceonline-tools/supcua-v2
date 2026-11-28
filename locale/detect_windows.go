//go:build windows

package locale

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const muiLanguageName = 0x8

var (
	kernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	procGetUserDefaultUILang    = kernel32.NewProc("GetUserDefaultUILanguage")
	procGetUserPreferredUILangs = kernel32.NewProc("GetUserPreferredUILanguages")
)

func platformDetector() Detector {
	return Detector{UILanguage: windowsUILanguage, Preferred: windowsPreferred}
}

func windowsUILanguage() (uint16, bool) {
	if procGetUserDefaultUILang.Find() != nil {
		return 0, false
	}
	r, _, _ := procGetUserDefaultUILang.Call()
	return uint16(r), true
}

// windowsPreferred returns the user's preferred UI language names in order (e.g. "vi-VN").
func windowsPreferred() []string {
	if procGetUserPreferredUILangs.Find() != nil {
		return nil
	}
	var n, size uint32
	if r, _, _ := procGetUserPreferredUILangs.Call(muiLanguageName, uintptr(unsafe.Pointer(&n)), 0, uintptr(unsafe.Pointer(&size))); r == 0 || size == 0 || size > 4096 {
		return nil
	}
	buf := make([]uint16, size)
	if r, _, _ := procGetUserPreferredUILangs.Call(muiLanguageName, uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r == 0 {
		return nil
	}
	var out []string
	start := 0
	for i, c := range buf {
		if c != 0 {
			continue
		}
		if i == start {
			break
		}
		out = append(out, windows.UTF16ToString(buf[start:i]))
		start = i + 1
	}
	return out
}
