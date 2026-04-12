//go:build !windows

package executor

import "fmt"

type InputSimulator struct{}

func NewInputSimulator() *InputSimulator { return &InputSimulator{} }

func (inputSimulator *InputSimulator) MoveMouse(positionX, positionY int) error {
	return fmt.Errorf("input simulation only supported on Windows")
}
func (inputSimulator *InputSimulator) MouseLeftDown()            {}
func (inputSimulator *InputSimulator) MouseLeftUp()              {}
func (inputSimulator *InputSimulator) MouseRightDown()           {}
func (inputSimulator *InputSimulator) MouseRightUp()             {}
func (inputSimulator *InputSimulator) KeyDown(keyName string) error { return fmt.Errorf("not supported") }
func (inputSimulator *InputSimulator) KeyUp(keyName string) error   { return fmt.Errorf("not supported") }
func (inputSimulator *InputSimulator) LockMouse()     {}
func (inputSimulator *InputSimulator) UnlockMouse()   {}
func (inputSimulator *InputSimulator) LockKeyboard()  {}
func (inputSimulator *InputSimulator) UnlockKeyboard() {}
func (inputSimulator *InputSimulator) UnlockAll()     {}
