//go:build !windows

package capture

import "fmt"

type PositionCallback func(positionX, positionY int)

func GetCurrentMousePosition() (int, int, error) {
	return 0, 0, fmt.Errorf("mouse capture only supported on Windows")
}
