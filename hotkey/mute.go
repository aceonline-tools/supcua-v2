package hotkey

import (
	"sync"
	"sync/atomic"
)

// muteGate serialises "mute all hotkeys" with callback dispatch. Suspend takes the write lock, so
// it returns only after every in-flight Allowed check finished; once it returns no callback can
// pass the check. Resume is applied by the listener thread (settle), after it discarded the
// WM_HOTKEY messages that queued up while muted, so nothing suppressed can fire late.
type muteGate struct {
	mu            sync.RWMutex
	muted         bool
	pendingResume atomic.Bool
}

func (g *muteGate) Suspend() {
	g.pendingResume.Store(false)
	g.mu.Lock()
	g.muted = true
	g.mu.Unlock()
}

// Resume asks the listener to unmute; the unmute itself happens in settle.
func (g *muteGate) Resume() { g.pendingResume.Store(true) }

// Allowed is the dispatch check: a callback may start only if it returns true.
func (g *muteGate) Allowed() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return !g.muted
}

// IsMuted reports the current state (listener thread: poll suppression).
func (g *muteGate) IsMuted() bool { return !g.Allowed() }

// settle completes a pending resume on the listener thread: drain throws away queued hotkey
// messages, resync re-arms the polled keys against the keys still held, and only then is the gate opened.
func (g *muteGate) settle(drain, resync func()) {
	if !g.pendingResume.Swap(false) {
		return
	}
	drain()
	resync()
	g.mu.Lock()
	g.muted = false
	g.mu.Unlock()
}

// pollState is one polled bare-modifier hotkey: it fires on key-up. Releases that happen while
// muted never fire, and a key still held when the mute ends stays suppressed until its first
// release, after which it is armed again.
type pollState struct {
	wasDown, suppressed bool
}

func (p *pollState) step(isDown, muted bool) (fire bool) {
	fire = p.wasDown && !isDown && !muted && !p.suppressed
	if !isDown {
		p.suppressed = false
	}
	p.wasDown = isDown
	return fire
}

func (p *pollState) resync(isDown bool) {
	p.wasDown = isDown
	p.suppressed = isDown
}
