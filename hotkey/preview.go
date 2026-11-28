package hotkey

import "sync"

// Preview lets a polled bare-modifier hotkey show its result on the overlay at key-down, before
// the release commits it. Begin returns a token (0 = nothing shown); Cancel ends that preview.
// Both are presentation-only and must not block.
type Preview struct {
	Begin  func(epoch uint64) uint64
	Cancel func(token uint64)
}

type keyKind int

const (
	keyNone  keyKind = iota
	keyLone          // pressed while no other key was down
	keyCombo         // pressed while another key was down
)

// keyTracker follows which keys are down from the keyboard hook. A key the hook reports down but
// the OS no longer holds (a dropped release) is pruned with isDown, so it cannot make later presses
// look like combos.
type keyTracker struct{ down [256]bool }

func (t *keyTracker) onKey(vk uint32, down bool, isDown func(uint32) bool) keyKind {
	if vk >= 256 {
		return keyNone
	}
	if !down {
		t.down[vk] = false
		return keyNone
	}
	if t.down[vk] {
		return keyNone // auto-repeat
	}
	others := 0
	for k := range t.down {
		if t.down[k] && uint32(k) != vk {
			if isDown(uint32(k)) {
				others++
			} else {
				t.down[k] = false
			}
		}
	}
	t.down[vk] = true
	if others == 0 {
		return keyLone
	}
	return keyCombo
}

func (t *keyTracker) isDown(vk uint32) bool { return vk < 256 && t.down[vk] }

type pollKeyEntry struct {
	id       int
	vkCode   uint32
	callback HotkeyCallback
	preview  *Preview
	token    uint64
	state    pollState
}

// pollFire is one release the poll decided to dispatch.
type pollFire struct {
	id       int
	callback HotkeyCallback
	preview  *Preview
	token    uint64
}

// pollSet is the polled bare-modifier hotkeys plus the hook-fed key tracker. The poll stays the
// authority for the release action; the hook only starts and cancels previews and taints presses
// that turned into combos.
type pollSet struct {
	mu      sync.Mutex
	keys    []pollKeyEntry
	tracker keyTracker
}

func (s *pollSet) add(e pollKeyEntry) {
	s.mu.Lock()
	s.keys = append(s.keys, e)
	s.mu.Unlock()
}

func (s *pollSet) remove(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.keys {
		if s.keys[i].id == id {
			s.cancelLocked(&s.keys[i])
			s.keys = append(s.keys[:i], s.keys[i+1:]...)
			return true
		}
	}
	return false
}

func (s *pollSet) setPreview(id int, p Preview) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.keys {
		if s.keys[i].id == id {
			s.keys[i].preview = &p
			return true
		}
	}
	return false
}

func (s *pollSet) cancelLocked(e *pollKeyEntry) {
	if e.token != 0 && e.preview != nil {
		e.preview.Cancel(e.token)
	}
	e.token = 0
}

// tick is one poll pass: it dispatches releases and ends previews that will not commit (released
// without firing, or muted by a recording).
func (s *pollSet) tick(isDown func(uint32) bool, muted bool, fire func(pollFire)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.keys {
		e := &s.keys[i]
		down := isDown(e.vkCode)
		if e.state.step(down, muted) {
			f := pollFire{e.id, e.callback, e.preview, e.token}
			e.token = 0
			fire(f)
			continue
		}
		if e.token != 0 && (!down || muted) {
			s.cancelLocked(e)
		}
	}
}

// key handles one hook key event. A lone press of a registered key begins its preview when the
// poll mute is open; a press that makes a combo taints every held registered key (its release will
// not fire) and cancels its preview.
func (s *pollSet) key(vk uint32, down, muted bool, isDown func(uint32) bool, stamp func() uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.tracker.onKey(vk, down, isDown) {
	case keyLone:
		if muted {
			return
		}
		for i := range s.keys {
			e := &s.keys[i]
			if e.vkCode == vk && e.preview != nil && e.token == 0 && !e.state.suppressed && !e.state.tainted {
				e.token = e.preview.Begin(stamp())
			}
		}
	case keyCombo:
		for i := range s.keys {
			e := &s.keys[i]
			if s.tracker.isDown(e.vkCode) {
				e.state.tainted = true
				s.cancelLocked(e)
			}
		}
	}
}
