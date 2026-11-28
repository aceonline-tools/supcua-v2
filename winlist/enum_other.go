//go:build !windows

package winlist

// SystemSource has no windows to list off Windows.
type SystemSource struct{}

// Self is unused off Windows.
func Self() uint32 { return 0 }

// Windows enumerates nothing.
func (SystemSource) Windows(func(Window) bool, func() bool) {}

// Title always returns empty.
func (SystemSource) Title(uintptr) string { return "" }

// ExeName always fails.
func (SystemSource) ExeName(uint32, func() bool) (string, error) { return "", constErr("unsupported") }
