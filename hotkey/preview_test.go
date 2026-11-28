package hotkey

import "testing"

const vkR, vkC, vkShift = 0xA3, 0x43, 0xA0

type fakePreview struct {
	begun, cancelled []uint64
	next             uint64
}

func (f *fakePreview) p() *Preview {
	return &Preview{
		Begin:  func(uint64) uint64 { f.next++; f.begun = append(f.begun, f.next); return f.next },
		Cancel: func(t uint64) { f.cancelled = append(f.cancelled, t) },
	}
}

type rig struct {
	set  pollSet
	held map[uint32]bool
	fp   *fakePreview
	fire []pollFire
}

func newRig() *rig {
	r := &rig{held: map[uint32]bool{}, fp: &fakePreview{}}
	r.set.add(pollKeyEntry{id: 1, vkCode: vkR, preview: r.fp.p()})
	return r
}

func (r *rig) isDown(vk uint32) bool { return r.held[vk] }

// hook is a key event as the hook delivers it (the OS key state is updated first).
func (r *rig) hook(vk uint32, down, muted bool) {
	r.held[vk] = down
	r.set.key(vk, down, muted, r.isDown, func() uint64 { return 7 })
}

func (r *rig) tick(muted bool) {
	r.set.tick(r.isDown, muted, func(f pollFire) { r.fire = append(r.fire, f) })
}

func TestPreview_LoneDownBeginsThenReleaseCommits(t *testing.T) {
	r := newRig()
	r.hook(vkR, true, false)
	if len(r.fp.begun) != 1 {
		t.Fatalf("preview not begun on a lone down: %v", r.fp.begun)
	}
	r.tick(false) // still held: nothing fires, nothing cancels
	if len(r.fire) != 0 || len(r.fp.cancelled) != 0 {
		t.Fatalf("held key fired/cancelled: %v %v", r.fire, r.fp.cancelled)
	}
	r.hook(vkR, false, false)
	r.tick(false)
	if len(r.fire) != 1 || r.fire[0].token != 1 {
		t.Fatalf("release did not commit with the preview token: %+v", r.fire)
	}
	if len(r.fp.cancelled) != 0 {
		t.Fatal("a committed preview must be ended by the action, not cancelled by the poll")
	}
}

func TestPreview_ComboCancelsAndSuppressesAction(t *testing.T) {
	r := newRig()
	r.hook(vkR, true, false)
	r.tick(false)
	r.hook(vkC, true, false)
	if len(r.fp.cancelled) != 1 || r.fp.cancelled[0] != 1 {
		t.Fatalf("combo did not revert the preview: %v", r.fp.cancelled)
	}
	r.hook(vkC, false, false)
	r.hook(vkR, false, false)
	r.tick(false)
	if len(r.fire) != 0 {
		t.Fatalf("RCtrl+C fired the action: %+v", r.fire)
	}
	r.hook(vkR, true, false) // the next lone press works again
	if len(r.fp.begun) != 2 {
		t.Fatal("taint leaked into the next press")
	}
}

func TestPreview_ModifierHeldBeforeShowsNothingAndDoesNotFire(t *testing.T) {
	r := newRig()
	r.hook(vkShift, true, false)
	r.hook(vkR, true, false)
	if len(r.fp.begun) != 0 {
		t.Fatalf("preview with a modifier held before: %v", r.fp.begun)
	}
	r.hook(vkR, false, false)
	r.tick(false)
	if len(r.fire) != 0 {
		t.Fatal("fired with a modifier held before")
	}
}

func TestPreview_MutedShowsNothing(t *testing.T) {
	r := newRig()
	r.hook(vkR, true, true)
	if len(r.fp.begun) != 0 {
		t.Fatal("preview begun while a recording mutes the poll")
	}
}

func TestPreview_RecordingStartingEndsIt(t *testing.T) {
	r := newRig()
	r.hook(vkR, true, false)
	r.tick(true)
	if len(r.fp.cancelled) != 1 {
		t.Fatalf("recording did not end the preview: %v", r.fp.cancelled)
	}
}

func TestPreview_KeySeenDownWhileMutedStaysSuppressed(t *testing.T) {
	r := newRig()
	r.held[vkR] = true
	r.tick(true) // poll sees the key down while muted: suppressed until released
	r.hook(vkR, true, false)
	if len(r.fp.begun) != 0 {
		t.Fatal("preview for a suppressed key")
	}
}

func TestPreview_ReleaseWithoutCommitEndsIt(t *testing.T) {
	r := newRig()
	r.hook(vkR, true, false)
	r.hook(vkR, false, false) // tapped between two polls: the poll never saw it down
	r.tick(false)
	if len(r.fire) != 0 {
		t.Fatal("fired a key the poll never saw down")
	}
	if len(r.fp.cancelled) != 1 {
		t.Fatalf("preview stuck after a release without a commit: %v", r.fp.cancelled)
	}
}

func TestPreview_DroppedReleaseOfOtherKeyDoesNotMakeComboForever(t *testing.T) {
	r := newRig()
	r.hook(vkC, true, false)
	r.held[vkC] = false // the hook never saw C's release (focus loss, secure desktop)
	r.hook(vkR, true, false)
	if len(r.fp.begun) != 1 {
		t.Fatalf("stale tracked key blocked the preview: %v", r.fp.begun)
	}
}

func TestPreview_DroppedReleaseOfRCtrlEndsAtNextPoll(t *testing.T) {
	r := newRig()
	r.hook(vkR, true, false)
	r.tick(false)
	r.held[vkR] = false // released while the hook was not listening
	r.tick(false)
	if len(r.fire) != 1 {
		t.Fatalf("the poll must still commit the release: %+v", r.fire)
	}
	r2 := newRig()
	r2.hook(vkR, true, false)
	r2.held[vkR] = false // released and the poll never saw it down
	r2.tick(false)
	if len(r2.fp.cancelled) != 1 {
		t.Fatal("preview stuck after a dropped release")
	}
}

func TestPreview_AutoRepeatDoesNotBeginTwice(t *testing.T) {
	r := newRig()
	r.hook(vkR, true, false)
	r.hook(vkR, true, false)
	if len(r.fp.begun) != 1 {
		t.Fatalf("auto-repeat began %d previews", len(r.fp.begun))
	}
}
