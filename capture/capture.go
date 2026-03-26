//go:build windows

package capture

import (
	"syscall"
	"unsafe"
)

var (
	user32DLL        = syscall.NewLazyDLL("user32.dll")
	procGetCursorPos = user32DLL.NewProc("GetCursorPos")
)

type cursorPoint struct {
	X int32
	Y int32
}

// PositionCallback is called with the captured mouse coordinates.
type PositionCallback func(positionX, positionY int)

// GetCurrentMousePosition returns the current cursor position.
func GetCurrentMousePosition() (int, int, error) {
	var currentPoint cursorPoint
	returnValue, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&currentPoint)))
	if returnValue == 0 {
		return 0, 0, err
	}
	return int(currentPoint.X), int(currentPoint.Y), nil
}
