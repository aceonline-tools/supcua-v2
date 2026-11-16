package bridge

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

func sync_Once() sync.Once { return sync.Once{} }
func os_ErrInvalid() error { return errors.New("boom") }

func TestCaptureManagerLifecycle(t *testing.T) {
	be := &fakeBackend{}
	em := &recEmitter{}
	m := newCaptureManager(be, em.Emit, time.Hour)

	if err := m.Start("s1", CaptureHotkey, "toggle", model.Hotkey{Key: "p"}); err != nil {
		t.Fatal(err)
	}
	if mode, sid := m.Active(); mode != CaptureHotkey || sid != "s1" {
		t.Fatalf("active = %v %v", mode, sid)
	}
	old := be.sink
	be.sink.Hotkey(model.Hotkey{Modifiers: []string{"ctrl"}, Key: "f5"})
	ev := em.named(EvCaptureHotkey)
	if len(ev) != 1 || ev[0].payload.(map[string]any)["display"] != "Ctrl + F5" || ev[0].payload.(map[string]any)["field"] != "toggle" {
		t.Fatalf("capture.hotkey = %v", ev)
	}
	be.sink.Key("a")
	be.sink.Mouse(1, 2)
	if len(em.named(EvCaptureKey))+len(em.named(EvCaptureMouse)) != 0 {
		t.Error("events of another mode must be dropped")
	}

	if err := m.Start("s1", CaptureMouse, "", model.Hotkey{Key: "p"}); err != nil {
		t.Fatal(err)
	}
	if got := em.named(EvCaptureEnded); len(got) != 1 || got[0].payload.(map[string]string)["reason"] != EndStopped {
		t.Errorf("replacing a capture must end the old one as stopped: %v", got)
	}
	old.Hotkey(model.Hotkey{Key: "z"})
	if len(em.named(EvCaptureHotkey)) != 1 {
		t.Error("stale events from a replaced session must be dropped")
	}
	be.sink.Mouse(3, 4)
	if len(em.named(EvCaptureMouse)) != 1 {
		t.Error("mouse event lost")
	}
	m.End(EndBlur)
	m.End(EndBlur)
	if got := em.named(EvCaptureEnded); len(got) != 2 || got[1].payload.(map[string]string)["reason"] != EndBlur {
		t.Errorf("End must be idempotent and emit once: %v", got)
	}
	be.sink.Mouse(5, 6)
	if len(em.named(EvCaptureMouse)) != 1 {
		t.Error("events after End must be dropped")
	}
}

func TestCaptureIdleTimeoutAndStartFailure(t *testing.T) {
	be := &fakeBackend{}
	em := &recEmitter{}
	m := newCaptureManager(be, em.Emit, 30*time.Millisecond)
	if err := m.Start("s", CaptureKey, "", model.Hotkey{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(em.named(EvCaptureEnded)) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	got := em.named(EvCaptureEnded)
	if len(got) != 1 || got[0].payload.(map[string]string)["reason"] != EndTimeout {
		t.Fatalf("want timeout, got %v", got)
	}
	if mode, _ := m.Active(); mode != CaptureNone {
		t.Error("manager must be idle after timeout")
	}

	be.err = errors.New("hook failed")
	if err := m.Start("s", CaptureKey, "", model.Hotkey{}); err == nil {
		t.Fatal("backend failure must surface")
	}
	if mode, _ := m.Active(); mode != CaptureNone {
		t.Error("failed start must not leave an active session")
	}
	if len(em.named(EvCaptureEnded)) != 1 {
		t.Error("failed start must not emit capture.ended")
	}
}

func TestBridgeCaptureFlowAndBlur(t *testing.T) {
	r := newRig(t, script("s1", "A", "", true))
	sid, _ := openEditor(t, r, "s1")
	r.mustCall(t, "capture.mouse.start", `{"sessionId":"`+sid+`"}`)
	r.cap.mu.Lock()
	pick := r.cap.pickKey
	r.cap.mu.Unlock()
	if pick.Key != "p" {
		t.Errorf("pick key should come from capture_hotkey config, got %+v", pick)
	}
	r.cap.sink.Mouse(10, 20)
	if len(r.em.named(EvCaptureMouse)) != 1 {
		t.Error("capture.mouse not emitted")
	}
	r.b.NotifyBlur()
	if got := r.em.named(EvCaptureEnded); len(got) != 1 || got[0].payload.(map[string]string)["reason"] != EndBlur {
		t.Errorf("blur: %v", got)
	}
	r.mustCall(t, "capture.key.start", `{"sessionId":"`+sid+`"}`)
	r.mustCall(t, "capture.stop", `{"sessionId":"`+sid+`"}`)
	r.mustCall(t, "capture.mouse.stop", `{"sessionId":"`+sid+`"}`)
	if got := r.em.named(EvCaptureEnded); len(got) != 2 || got[1].payload.(map[string]string)["reason"] != EndStopped {
		t.Errorf("stop: %v", got)
	}
}

func TestConfigServiceRollsBackOnSaveFailureAndIsolatesCopies(t *testing.T) {
	per := &memPersister{cfg: modelDefault()}
	svc, err := NewConfigService(per)
	if err != nil {
		t.Fatal(err)
	}
	per.fail = true
	if err := svc.Update(func(c *model.AppConfig) { c.Theme = "dark"; c.CollapsedGroups = []string{"x"} }); err == nil {
		t.Fatal("expected save error")
	}
	if svc.Get().Theme != "system" || len(svc.Get().CollapsedGroups) != 0 {
		t.Error("failed update must not change in-memory config")
	}
	per.fail = false
	_ = svc.Update(func(c *model.AppConfig) { c.CollapsedGroups = []string{"x"} })
	got := svc.Get()
	got.CollapsedGroups[0] = "mutated"
	if svc.Get().CollapsedGroups[0] != "x" {
		t.Error("Get must return an isolated copy")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = svc.Update(func(c *model.AppConfig) { c.KeyPressDurationMs++ }) }()
	}
	wg.Wait()
	if svc.Get().KeyPressDurationMs != 30 {
		t.Errorf("serialised updates lost writes: %d", svc.Get().KeyPressDurationMs)
	}
}

func modelDefault() model.AppConfig { return model.DefaultConfig() }

func TestHotkeyDisplayParseRoundTrip(t *testing.T) {
	for _, in := range []model.Hotkey{
		{Modifiers: []string{}, Key: "rctrl"}, {Modifiers: []string{"ctrl"}, Key: "o"},
		{Modifiers: []string{"ctrl", "shift"}, Key: "f12"}, {Modifiers: []string{"alt"}, Key: "space"},
	} {
		out, ok := ParseHotkey(DisplayHotkey(in))
		if !ok || DisplayHotkey(out) != DisplayHotkey(in) || out.Key != in.Key {
			t.Errorf("round trip %v -> %q -> %v", in, DisplayHotkey(in), out)
		}
	}
	for _, bad := range []string{"", "Ctrl +", "Hyper + A", "A + B", "ctrl + a b"} {
		if _, ok := ParseHotkey(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
	if DisplayHotkey(model.Hotkey{Key: "rctrl"}) != "RCtrl" {
		t.Error("RCtrl display")
	}
}

func TestAllDTOFieldsAreExplicitlyTagged(t *testing.T) {
	for _, v := range []any{
		OverlayDTO{}, AutoActiveDTO{}, StateDTO{}, ScriptSummary{}, GroupDTO{}, ScriptList{}, BootState{}, SettingsDTO{},
		SettingsPatch{}, SettingsUpdateResult{}, HotkeyConflict{}, ParamSpec{}, ActionSpec{}, EditorSession{}, Error{},
		saveResult{}, closeResult{},
	} {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			if f := typ.Field(i); f.Tag.Get("json") == "" {
				t.Errorf("%s.%s has no json tag (garble would rename it)", typ.Name(), f.Name)
			}
		}
	}
}
