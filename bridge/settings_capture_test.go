package bridge

import (
	"errors"
	"github.com/yanmad27/supcua-v2/app"
	"sync"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

type fakeMute struct {
	mu    sync.Mutex
	calls []bool
}

func (f *fakeMute) SetSuspended(on bool) { f.mu.Lock(); f.calls = append(f.calls, on); f.mu.Unlock() }
func (f *fakeMute) muted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls) > 0 && f.calls[len(f.calls)-1]
}
func (f *fakeMute) n() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

func newCaptureRig(t *testing.T) (*rig, *fakeMute) {
	t.Helper()
	r := newRig(t)
	mute := &fakeMute{}
	r.b.capture.suspend = mute.SetSuspended
	return r, mute
}

func TestSettingsCaptureRecordsComboAndSingleModifiersWithSide(t *testing.T) {
	cases := []struct {
		hk   model.Hotkey
		want string
	}{
		{model.Hotkey{Modifiers: []string{"ctrl", "alt"}, Key: "f8"}, "Ctrl + Alt + F8"},
		{model.Hotkey{Key: "rctrl"}, "RCtrl"},
		{model.Hotkey{Key: "lshift"}, "LShift"},
		{model.Hotkey{Key: "ralt"}, "RAlt"},
	}
	for _, c := range cases {
		r, _ := newCaptureRig(t)
		r.mustCall(t, "settings.capture.start", `{"field":"global_hotkey"}`)
		r.cap.sink.Hotkey(c.hk)
		ev := r.em.named(EvSettingsCaptureHotkey)
		if len(ev) != 1 {
			t.Fatalf("%s: want one settings.capture.hotkey, got %v", c.want, ev)
		}
		got := ev[0].payload.(hotkeyEvent)
		if got.Display != c.want || got.Field != "global_hotkey" {
			t.Errorf("display = %q field=%q, want %q", got.Display, got.Field, c.want)
		}
		if _, ok := ParseHotkey(got.Display); !ok {
			t.Errorf("%q does not round-trip through ParseHotkey", got.Display)
		}
		if len(r.em.named(EvCaptureHotkey)) != 0 {
			t.Error("the editor's capture.hotkey event must not be used")
		}
	}
}

func TestSettingsCaptureMutesHotkeysAndRestoresOnEveryExit(t *testing.T) {
	exits := map[string]func(t *testing.T, r *rig){
		"stop":    func(t *testing.T, r *rig) { r.mustCall(t, "settings.capture.stop", `{}`) },
		"blur":    func(t *testing.T, r *rig) { r.b.NotifyBlur() },
		"reset":   func(t *testing.T, r *rig) { r.b.ResetSession(EndEditorClosed) },
		"timeout": func(t *testing.T, r *rig) { r.b.capture.End(EndTimeout) },
		"replaced": func(t *testing.T, r *rig) {
			r.mustCall(t, "settings.capture.start", `{"field":"overlay_hotkey"}`)
			r.mustCall(t, "settings.capture.stop", `{}`)
		},
	}
	for name, exit := range exits {
		t.Run(name, func(t *testing.T) {
			r, mute := newCaptureRig(t)
			r.mustCall(t, "settings.capture.start", `{"field":"global_hotkey"}`)
			if !mute.muted() {
				t.Fatal("hotkeys must be muted while recording")
			}
			exit(t, r)
			if mute.muted() {
				t.Fatalf("hotkeys still muted after %s: %v", name, mute.calls)
			}
			if len(r.em.named(EvSettingsCaptureEnded)) == 0 {
				t.Error("settings.capture.ended not emitted")
			}
			if len(r.em.named(EvCaptureEnded)) != 0 {
				t.Error("editor capture.ended must not be emitted")
			}
		})
	}
}

func TestSettingsCaptureRestoresWhenBackendFails(t *testing.T) {
	r, mute := newCaptureRig(t)
	r.cap.err = errors.New("hook failed")
	if _, err := r.call(t, "settings.capture.start", `{"field":"global_hotkey"}`); err == nil {
		t.Fatal("start must fail")
	}
	if mute.muted() {
		t.Fatalf("muted after a failed start: %v", mute.calls)
	}
}

func TestSettingsCaptureRejectsBadFieldAndOpenEditor(t *testing.T) {
	r, mute := newCaptureRig(t)
	if _, err := r.call(t, "settings.capture.start", `{"field":"theme"}`); err == nil {
		t.Error("only the three hotkey fields may be recorded")
	}
	sid, _ := openEditor(t, r, "")
	_, err := r.call(t, "settings.capture.start", `{"field":"global_hotkey"}`)
	wantCode(t, err, CodeEditorBusy)
	if mute.n() != 0 {
		t.Errorf("a refused start must not touch the hotkeys: %v", mute.calls)
	}
	r.mustCall(t, "capture.hotkey.start", `{"sessionId":"`+sid+`","field":"toggle"}`)
	r.mustCall(t, "settings.capture.stop", `{}`)
	if mode, _ := r.b.capture.Active(); mode != CaptureHotkey {
		t.Fatalf("settings.capture.stop ended the editor's capture (mode %q)", mode)
	}
	if mute.n() != 0 {
		t.Errorf("an editor capture must not mute hotkeys: %v", mute.calls)
	}
}

func TestSettingsCaptureStaleCaptureIDIsNoop(t *testing.T) {
	r, mute := newCaptureRig(t)
	r.mustCall(t, "settings.capture.start", `{"field":"global_hotkey"}`)
	r.mustCall(t, "settings.capture.stop", `{"capture_id":999}`)
	if !mute.muted() {
		t.Fatal("a stale capture_id must not end the recording")
	}
}

func TestSettingsSaveKeepsOldValueOnConflictOrRegisterFailure(t *testing.T) {
	r, _ := newCaptureRig(t)
	before := r.cfg.Get()
	// conflict with another of the three
	_, err := r.call(t, "settings.update", `{"patch":{"global_hotkey":"`+DisplayHotkey(before.OverlayHotkey)+`"}}`)
	var be *Error
	if !errors.As(err, &be) || be.Code != CodeInvalidSetting || be.Field != "overlay_hotkey" && be.Field != "global_hotkey" {
		t.Fatalf("conflict: %v", err)
	}
	// unparsable
	if _, err := r.call(t, "settings.update", `{"patch":{"global_hotkey":"Ctrl + Banana"}}`); err == nil {
		t.Fatal("unparsable combo must be rejected")
	}
	// registration failure (another app owns it)
	r.host.failKind = HotkeyGlobal
	_, err = r.call(t, "settings.update", `{"patch":{"global_hotkey":"Ctrl + Alt + F8"}}`)
	if !errors.As(err, &be) || be.Code != CodeHotkeyRegisterFail {
		t.Fatalf("register failure: %v", err)
	}
	if got := r.cfg.Get(); got.GlobalHotkey.Key != before.GlobalHotkey.Key || len(got.GlobalHotkey.Modifiers) != len(before.GlobalHotkey.Modifiers) {
		t.Fatalf("old value lost: %+v", got.GlobalHotkey)
	}
}

type restoreFailHost struct {
	*fakeHost
	failRestore string
	restoring   bool
}

func (h *restoreFailHost) RebindHotkey(kind string, hk model.Hotkey) error {
	if kind == HotkeyOverlay && hk.Key == "f12" {
		h.restoring = true
		return errors.New("in use")
	}
	if h.restoring && kind == HotkeyGlobal {
		return errors.New("cannot restore")
	}
	return h.fakeHost.RebindHotkey(kind, hk)
}

func TestFailedRestoreOfThePreviousHotkeyIsReportedNotSilent(t *testing.T) {
	r, _ := newCaptureRig(t)
	r.b.d.Host = &restoreFailHost{fakeHost: r.host}
	_, err := r.call(t, "settings.update", `{"patch":{"global_hotkey":"Ctrl + Alt + F8","overlay_hotkey":"F12"}}`)
	var be *Error
	if !errors.As(err, &be) || be.Code != CodeHotkeyRestoreFail {
		t.Fatalf("want HOTKEY_RESTORE_FAILED, got %v", err)
	}
}

func TestSettingsCaptureSuspendGatesControllerAndPolledKeys(t *testing.T) {
	ctl := app.NewController(nil, nil, nil, nil)
	mute := &fakeMute{}
	b := New(Deps{Controller: ctl, HotkeyMute: mute}, &recEmitter{})
	b.capture.suspend(true)
	if ctl.RunHotkeyAction(func() {}) {
		t.Fatal("controller hotkey actions not gated while recording")
	}
	b.capture.suspend(false)
	if !ctl.RunHotkeyAction(func() {}) {
		t.Fatal("controller gate not released after recording")
	}
	mute.mu.Lock()
	defer mute.mu.Unlock()
	if len(mute.calls) != 2 || !mute.calls[0] || mute.calls[1] {
		t.Fatalf("poll mute calls = %v", mute.calls)
	}
}
