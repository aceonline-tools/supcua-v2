//go:build !windows

package winlist

// SystemSource has no windows to list off Windows.
type SystemSource struct{}

// Self is unused off Windows.
func Self() uint32 { return 0 }

// Windows enumerates nothing.
func (SystemSource) Windows(func(Window) bool) {}

// ExeName always fails.
func (SystemSource) ExeName(uint32) (string, error) { return "", constErr("unsupported") }
