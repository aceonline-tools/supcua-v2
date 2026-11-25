//go:build windows

package ui

import (
	"errors"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/model"
)

var procSendInput = windows.NewLazySystemDLL("user32.dll").NewProc("SendInput")

// kbInput is the 40-byte x64 INPUT of type INPUT_KEYBOARD.
type kbInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uintptr
	_     [8]byte
}

const (
	inputKeyboard    = 1
	keyEventKeyUp    = 0x0002
	keyEventExtended = 0x0001
	vkShift, vkCtl   = 0x10, 0x11
	vkAlt            = 0x12
)

func modifierVK(name string) uint16 {
	switch strings.ToLower(name) {
	case "ctrl":
		return vkCtl
	case "alt":
		return vkAlt
	case "shift":
		return vkShift
	case "win":
		return 0x5B
	}
	return 0
}

// SendHotkey synthesises the key chord with SendInput, exactly as a keyboard would deliver it.
func SendHotkey(hk model.Hotkey) error {
	key := uint16(hotkey.KeyNameToVirtualKeyCode(hk.Key))
	if key == 0 {
		return errors.New("unknown key " + hk.Key)
	}
	var seq []uint16
	for _, m := range hk.Modifiers {
		if v := modifierVK(m); v != 0 {
			seq = append(seq, v)
		}
	}
	seq = append(seq, key)
	send := func(vk uint16, flags uint32) error {
		if vk == 0x5B || vk == 0x5C {
			flags |= keyEventExtended
		}
		in := kbInput{typ: inputKeyboard, vk: vk, flags: flags}
		n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
		if n != 1 {
			return err
		}
		return nil
	}
	pressed := 0
	release := func() {
		for i := pressed - 1; i >= 0; i-- {
			_ = send(seq[i], keyEventKeyUp)
		}
		pressed = 0
	}
	for _, vk := range seq {
		if err := send(vk, 0); err != nil {
			release() // never leave a modifier stuck down
			return err
		}
		pressed++
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond)
	release()
	return nil
}
