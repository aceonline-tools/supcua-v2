package ui

// DISPLAY_DEVICE state flags (wingdi.h).
const (
	displayAttachedToDesktop = 0x1
	displayMirroringDriver   = 0x8
)

// monitorDeviceActive reports whether an EnumDisplayDevices entry is a real, attached monitor:
// it must be attached to the desktop and must not be a mirroring (pseudo) driver.
func monitorDeviceActive(stateFlags uint32) bool {
	return stateFlags&displayAttachedToDesktop != 0 && stateFlags&displayMirroringDriver == 0
}
