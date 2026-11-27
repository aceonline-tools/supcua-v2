package ui

import "testing"

func TestMonitorDeviceActiveFilter(t *testing.T) {
	cases := []struct {
		flags uint32
		want  bool
	}{
		{0, false},
		{displayAttachedToDesktop, true},
		{displayAttachedToDesktop | 0x4 /* PRIMARY_DEVICE */, true},
		{displayMirroringDriver, false},
		{displayAttachedToDesktop | displayMirroringDriver, false},
		{0x4, false},
	}
	for _, c := range cases {
		if got := monitorDeviceActive(c.flags); got != c.want {
			t.Errorf("flags %#x: got %v want %v", c.flags, got, c.want)
		}
	}
}
