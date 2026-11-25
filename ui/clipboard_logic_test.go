package ui

import "testing"

type fakeClip struct {
	openOK, emptyOK, allocOK, writeOK, setOK bool
	opened, closed, freed, set               int
	owner                                    uintptr
}

func (f *fakeClip) Open(o uintptr) bool { f.opened++; f.owner = o; return f.openOK }
func (f *fakeClip) Close()              { f.closed++ }
func (f *fakeClip) Empty() bool         { return f.emptyOK }
func (f *fakeClip) Alloc(int) uintptr {
	if f.allocOK {
		return 7
	}
	return 0
}
func (f *fakeClip) Write(uintptr, []uint16) bool { return f.writeOK }
func (f *fakeClip) Free(uintptr)                 { f.freed++ }
func (f *fakeClip) Set(uintptr) bool             { f.set++; return f.setOK }

func TestClipboardFailurePathsFreeTheBlockAndCloseTheClipboard(t *testing.T) {
	all := fakeClip{openOK: true, emptyOK: true, allocOK: true, writeOK: true, setOK: true}
	cases := []struct {
		name      string
		mod       func(*fakeClip)
		wantErr   bool
		wantFreed int
	}{
		{"success", func(*fakeClip) {}, false, 0},
		{"open fails", func(f *fakeClip) { f.openOK = false }, true, 0},
		{"empty fails", func(f *fakeClip) { f.emptyOK = false }, true, 0},
		{"alloc fails", func(f *fakeClip) { f.allocOK = false }, true, 0},
		{"write fails", func(f *fakeClip) { f.writeOK = false }, true, 1},
		{"set fails", func(f *fakeClip) { f.setOK = false }, true, 1},
	}
	for _, c := range cases {
		f := all
		c.mod(&f)
		err := writeClipboardText(&f, 0xABC, []uint16{'h', 'i', 0})
		if (err != nil) != c.wantErr || f.freed != c.wantFreed {
			t.Errorf("%s: err=%v freed=%d want err=%v freed=%d", c.name, err, f.freed, c.wantErr, c.wantFreed)
		}
		if f.openOK && f.closed != 1 {
			t.Errorf("%s: clipboard not closed", c.name)
		}
		if f.owner != 0xABC {
			t.Errorf("%s: owner %#x must be the host window", c.name, f.owner)
		}
	}
}
