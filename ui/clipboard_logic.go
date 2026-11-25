package ui

import "errors"

// clipAPI is the platform clipboard surface, split out so the failure paths (every one must
// free the global block unless the system took ownership) are testable on any OS.
type clipAPI interface {
	Open(owner uintptr) bool
	Close()
	Empty() bool
	Alloc(units int) uintptr // GMEM_MOVEABLE block of units UTF-16 code units, 0 on failure
	Write(h uintptr, text []uint16) bool
	Free(h uintptr)
	Set(h uintptr) bool // SetClipboardData(CF_UNICODETEXT, h); on success the system owns h
}

// writeClipboardText puts text on the clipboard owned by owner (the host window). The global
// block is freed on every failure path; after a successful Set the system owns it.
func writeClipboardText(api clipAPI, owner uintptr, text []uint16) error {
	if !api.Open(owner) {
		return errors.New("clipboard busy")
	}
	defer api.Close()
	if !api.Empty() {
		return errors.New("clipboard could not be emptied")
	}
	h := api.Alloc(len(text))
	if h == 0 {
		return errors.New("clipboard alloc failed")
	}
	if !api.Write(h, text) {
		api.Free(h)
		return errors.New("clipboard write failed")
	}
	if !api.Set(h) {
		api.Free(h)
		return errors.New("set clipboard failed")
	}
	return nil
}
