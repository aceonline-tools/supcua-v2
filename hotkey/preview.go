package hotkey

import "sync"

// Preview lets a polled bare-modifier hotkey show its result on the overlay at key-down, before
// the release commits it. Begin returns a token (0 = nothing shown); Cancel ends that preview.
// Both are presentation-only and must not block.
type Preview struct {
	Begin  func(epoch uint64) uint64
	Cancel func(token uint64)
}

// keyTracker filters key auto-repeat: the hook reports a held key down repeatedly, and only the
// first report is a down-edge. The poll clears a key once the OS reports it up, so a dropped
// release cannot swallow the next press.
type keyTracker struct{ down [256]bool }

func (t *keyTracker) downEdge(vk uint32, down bool) bool {
	if vk >= 256 {
		return false
	}
	if !down {
		t.down[vk] = false
		return false
	}
	if t.down[vk] {
		return false
	}
	t.down[vk] = true
	return true
}

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
// authority for the release action; the hook only starts previews.
type pollSet struct {
	mu      sync.Mutex
	keys    []pollKeyEntry
	tracker keyTracker
	mask    keyMask // registered vks: read by the hook callback without the lock
}

func (s *pollSet) add(e pollKeyEntry) {
	s.mu.Lock()
	s.keys = append(s.keys, e)
	s.mask.set(e.vkCode, true)
	s.mu.Unlock()
}

func (s *pollSet) remove(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.keys {
		if s.keys[i].id == id {
			s.cancelLocked(&s.keys[i])
			vk := s.keys[i].vkCode
			s.keys = append(s.keys[:i], s.keys[i+1:]...)
			still := false
			for _, k := range s.keys {
				still = still || k.vkCode == vk
			}
			s.mask.set(vk, still)
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
		if !down {
			s.tracker.down[e.vkCode&0xFF] = false
		}
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

// key handles one hook key event. The down-edge of a registered key begins its preview whenever
// the release will fire: the poll mute is open and the key is not suppressed. Other keys going
// down change nothing: the release fires exactly as it always did.
func (s *pollSet) key(vk uint32, down, muted bool, stamp func() uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.tracker.downEdge(vk, down) || muted {
		return
	}
	for i := range s.keys {
		e := &s.keys[i]
		if e.vkCode == vk && e.preview != nil && e.token == 0 && !e.state.suppressed {
			e.token = e.preview.Begin(stamp())
		}
	}
}
