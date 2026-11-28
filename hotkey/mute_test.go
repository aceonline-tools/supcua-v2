package hotkey

import "testing"

func TestPollKeyHeldThroughRecordingStaysQuietUntilReleasedThenRearms(t *testing.T) {
	var m pollMute
	var p pollState
	p.step(true, m.muted()) // RCtrl down before recording
	m.set(true)
	p.step(true, m.muted()) // still held while recording
	m.set(false)
	if p.step(false, m.muted()) {
		t.Fatal("releasing a key held during the recording fired after it ended")
	}
	p.step(true, false)
	if !p.step(false, false) {
		t.Fatal("the key must be armed again after its first release")
	}
}

func TestPollKeyPressedDuringRecordingReleasedAfterDoesNotFire(t *testing.T) {
	var p pollState
	p.step(false, true)
	p.step(true, true)
	if p.step(false, false) {
		t.Fatal("a press made while recording fired on its release after the recording")
	}
}

func TestPollKeyReleasedWhileMutedNeverFires(t *testing.T) {
	var p pollState
	p.step(true, false)
	if p.step(false, true) {
		t.Fatal("a release while muted fired")
	}
	p.step(true, false)
	if !p.step(false, false) {
		t.Fatal("a fresh press after the mute must fire")
	}
}

func TestPollMuteOldRecordingEndDoesNotClearANewerOne(t *testing.T) {
	var m pollMute
	m.set(true)  // capture A
	m.set(true)  // capture B starts
	m.set(false) // A's end arrives late
	if !m.muted() {
		t.Fatal("the end of an older recording unmuted a newer one")
	}
	m.set(false)
	m.set(false) // unbalanced end must not go negative
	if m.muted() {
		t.Fatal("still muted after every recording ended")
	}
	m.set(true)
	if !m.muted() {
		t.Fatal("a new recording after an extra end must mute")
	}
}

func TestPollMuteWithNoSampleInsideStillSuppressesHeldAndReleasedKeys(t *testing.T) {
	var m pollMute
	var seen uint64
	var held, released pollState
	held.step(true, m.sample(&seen))
	released.step(true, false)
	m.set(true)
	m.set(false) // recording started and ended between two samples
	muted := m.sample(&seen)
	held.step(true, muted)
	if released.step(false, muted) {
		t.Fatal("a key released during an unsampled recording fired")
	}
	if held.step(false, m.sample(&seen)) {
		t.Fatal("a key held through an unsampled recording fired on release")
	}
	held.step(true, m.sample(&seen))
	if !held.step(false, m.sample(&seen)) {
		t.Fatal("the key must re-arm after its release")
	}
}
