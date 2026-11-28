package hotkey

import "sync/atomic"

// pollMute counts active Settings recordings for the polled bare-modifier keys. The actions
// themselves are gated by the app controller; this only keeps a key pressed during a recording
// from firing on its release after the recording ended.
type pollMute struct {
	depth atomic.Int32
	gen   atomic.Uint64 // bumped by every set(true)
}

func (p *pollMute) set(on bool) {
	if on {
		p.gen.Add(1)
		p.depth.Add(1)
		return
	}
	for {
		d := p.depth.Load()
		if d <= 0 || p.depth.CompareAndSwap(d, d-1) {
			return
		}
	}
}

func (p *pollMute) muted() bool { return p.depth.Load() > 0 }

// sample is the listener's per-poll mute state. It also reports muted when a recording started
// since the previous sample (seen is the listener's copy of gen), even if it already ended, so a
// key down at that sample is suppressed until its release and one released meanwhile does not
// fire: suppression never depends on a sample landing inside the recording.
func (p *pollMute) sample(seen *uint64) bool {
	g := p.gen.Load()
	muted := g != *seen || p.depth.Load() > 0
	*seen = g
	return muted
}

// pollState is one polled bare-modifier hotkey: it fires on key-up. A key seen down while muted
// stays suppressed until its first release, after which it is armed again.
type pollState struct {
	wasDown, suppressed bool
	// tainted: another key went down while this one was held, so its release is part of a combo
	// and does not fire. It clears on the release.
	tainted bool
}

func (p *pollState) step(isDown, muted bool) (fire bool) {
	fire = p.wasDown && !isDown && !muted && !p.suppressed && !p.tainted
	if muted && isDown {
		p.suppressed = true
	}
	if !isDown {
		p.suppressed = false
		p.tainted = false
	}
	p.wasDown = isDown
	return fire
}
