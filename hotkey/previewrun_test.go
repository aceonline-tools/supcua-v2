package hotkey

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeHook struct {
	mu       sync.Mutex
	installs int
	stops    int
	emit     func(previewEvent)
	dead     func()
	fail     bool
}

func (f *fakeHook) install(emit func(previewEvent), dead func()) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("hook install failed")
	}
	f.installs++
	f.emit, f.dead = emit, dead
	return func() { f.mu.Lock(); f.stops++; f.emit = nil; f.mu.Unlock() }, nil
}

func (f *fakeHook) send(ev previewEvent) {
	f.mu.Lock()
	e := f.emit
	f.mu.Unlock()
	if e != nil {
		e(ev)
	}
}

type recorder struct {
	mu  sync.Mutex
	got []previewEvent
}

func (r *recorder) handle(ev previewEvent) { r.mu.Lock(); r.got = append(r.got, ev); r.mu.Unlock() }
func (r *recorder) snapshot() []previewEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]previewEvent(nil), r.got...)
}

func waitEvents(t *testing.T, r *recorder, n int) []previewEvent {
	t.Helper()
	for i := 0; i < 500; i++ {
		if g := r.snapshot(); len(g) >= n {
			return g
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("got %v, want %d events", r.snapshot(), n)
	return nil
}

func newRunner(f *fakeHook, r *recorder) *previewRunner {
	return &previewRunner{install: f.install, handle: r.handle}
}

func TestPreviewRunner_StartStopStartKeepsOneWorkerAndOrder(t *testing.T) {
	f, rec := &fakeHook{}, &recorder{}
	r := newRunner(f, rec)
	for round := 0; round < 2; round++ {
		if err := r.start(); err != nil {
			t.Fatal(err)
		}
		if err := r.start(); err != nil { // idempotent
			t.Fatal(err)
		}
		if n := r.workers.Load(); n != 1 {
			t.Fatalf("round %d: %d workers", round, n)
		}
		base := len(rec.snapshot())
		for i := uint32(0); i < 5; i++ {
			f.send(previewEvent{vk: i, down: true})
		}
		got := waitEvents(t, rec, base+5)[base:]
		for i, ev := range got {
			if ev.vk != uint32(i) {
				t.Fatalf("round %d: events out of order: %v", round, got)
			}
		}
		r.stop()
		if n := r.workers.Load(); n != 0 {
			t.Fatalf("round %d: %d workers after stop", round, n)
		}
	}
	if f.installs != 2 || f.stops != 2 {
		t.Fatalf("installs=%d stops=%d", f.installs, f.stops)
	}
	f.send(previewEvent{vk: 99}) // after stop: nothing may reach the handler
	time.Sleep(20 * time.Millisecond)
	if len(rec.snapshot()) != 10 {
		t.Fatalf("event delivered after stop: %v", rec.snapshot())
	}
}

func TestPreviewRunner_InstallFailureResetsSoALaterStartWorks(t *testing.T) {
	f, rec := &fakeHook{fail: true}, &recorder{}
	r := newRunner(f, rec)
	if r.start() == nil || r.workers.Load() != 0 {
		t.Fatal("failed install must report the error and start no worker")
	}
	f.fail = false
	if err := r.start(); err != nil || r.workers.Load() != 1 {
		t.Fatalf("restart after failure: err=%v workers=%d", err, r.workers.Load())
	}
	r.stop()
}

func TestPreviewRunner_DeadHookResetsSoALaterStartInstallsOne(t *testing.T) {
	f, rec := &fakeHook{}, &recorder{}
	r := newRunner(f, rec)
	_ = r.start()
	f.dead() // GetMessage failed: the hook thread ended by itself
	for i := 0; i < 500 && r.workers.Load() != 0; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if r.workers.Load() != 0 {
		t.Fatal("worker still running after the hook died")
	}
	if err := r.start(); err != nil || r.workers.Load() != 1 || f.installs != 2 {
		t.Fatalf("restart: err=%v workers=%d installs=%d", err, r.workers.Load(), f.installs)
	}
	r.stop()
}

func TestQueueKey_OnlyRegisteredKeysAreQueued(t *testing.T) {
	var set pollSet
	set.add(pollKeyEntry{id: 1, vkCode: vkR})
	var queued []previewEvent
	emit := func(ev previewEvent) { queued = append(queued, ev) }
	for vk := uint32(0); vk < 300; vk++ { // every key but RCtrl: typing, modifiers, out of range
		if vk != vkR {
			queueKey(&set.mask, emit, vk, true, false, false)
			queueKey(&set.mask, emit, vk, false, false, false)
		}
	}
	if len(queued) != 0 {
		t.Fatalf("unregistered keys were queued: %v", queued)
	}
	if !queueKey(&set.mask, emit, vkR, true, false, false) || len(queued) != 1 {
		t.Fatal("registered key not queued")
	}
	if queueKey(&set.mask, emit, vkR, true, true, false) {
		t.Fatal("injected key queued in production mode")
	}
	if !queueKey(&set.mask, emit, vkR, true, true, true) {
		t.Fatal("injected key not queued in selftest mode")
	}
	set.remove(1)
	if queueKey(&set.mask, emit, vkR, true, false, false) {
		t.Fatal("key still queued after unregistering")
	}
}

func TestKeyMask_SharedVKStaysUntilLastRegistrationGoes(t *testing.T) {
	var set pollSet
	set.add(pollKeyEntry{id: 1, vkCode: vkR})
	set.add(pollKeyEntry{id: 2, vkCode: vkR})
	set.remove(1)
	if !set.mask.has(vkR) {
		t.Fatal("mask cleared while another hotkey still uses the key")
	}
	set.remove(2)
	if set.mask.has(vkR) {
		t.Fatal("mask kept after the last registration")
	}
}
