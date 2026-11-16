//go:build windows

package autoactive

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32DLL                    = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow      = user32DLL.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32DLL.NewProc("GetWindowThreadProcessId")
)

const maxImagePathLength = 32768

// ForegroundProcessName returns the full image path of the process owning the foreground
// window, or an empty string when there is no foreground window.
func ForegroundProcessName() (string, error) {
	foregroundWindow, _, _ := procGetForegroundWindow.Call()
	if foregroundWindow == 0 {
		return "", nil
	}

	var processID uint32
	threadID, _, _ := procGetWindowThreadProcessID.Call(foregroundWindow, uintptr(unsafe.Pointer(&processID)))
	if threadID == 0 || processID == 0 {
		return "", nil
	}

	processHandle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(processHandle)

	imagePathBuffer := make([]uint16, windows.MAX_PATH)
	for {
		bufferSize := uint32(len(imagePathBuffer))
		err = windows.QueryFullProcessImageName(processHandle, 0, &imagePathBuffer[0], &bufferSize)
		if err == nil {
			return syscall.UTF16ToString(imagePathBuffer[:bufferSize]), nil
		}
		if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || len(imagePathBuffer) >= maxImagePathLength {
			return "", err
		}
		imagePathBuffer = make([]uint16, len(imagePathBuffer)*2)
	}
}
