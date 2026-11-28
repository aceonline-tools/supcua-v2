package bridge

import (
	"reflect"
	"sort"
	"testing"

	"github.com/yanmad27/supcua-v2/locale"
)

func withDetector(t *testing.T, d locale.Detector) {
	prev, prevCur := locale.DefaultDetector, locale.Current()
	locale.DefaultDetector = d
	t.Cleanup(func() { locale.DefaultDetector = prev; locale.Set(prevCur) })
}

func fixedUI(id uint16) locale.Detector {
	return locale.Detector{UILanguage: func() (uint16, bool) { return id, true }}
}

func TestSettingsLanguageDefaultsFromWindowsAndIsNotPersisted(t *testing.T) {
	withDetector(t, fixedUI(0x042A))
	r := newRig(t)
	if got := r.mustCall(t, "settings.get", "")["language"]; got != "vi" {
		t.Errorf("effective language on Vietnamese Windows = %v, want vi", got)
	}
	if r.cfg.Get().Language != "" {
		t.Errorf("detection must not be persisted, got %q", r.cfg.Get().Language)
	}
	withDetector(t, fixedUI(0x0409))
	if got := r.mustCall(t, "settings.get", "")["language"]; got != "en" {
		t.Errorf("effective language on English Windows = %v, want en", got)
	}
}

func TestSettingsLanguageUpdatePersistsEmitsAndSetsCurrent(t *testing.T) {
	withDetector(t, fixedUI(0x0409))
	r := newRig(t)
	res := r.mustCall(t, "settings.update", `{"patch":{"language":" VI-vn "}}`)
	applied := toStrings(res["applied"])
	sort.Strings(applied)
	if !reflect.DeepEqual(applied, []string{"language"}) {
		t.Errorf("applied = %v", applied)
	}
	if got := r.cfg.Get().Language; got != "vi" {
		t.Errorf("persisted language = %q, want the normalised vi", got)
	}
	if locale.Current() != locale.VI {
		t.Errorf("locale.Current = %v", locale.Current())
	}
	evs := r.em.named(EvSettingsChanged)
	if len(evs) != 1 {
		t.Fatalf("settings.changed events = %d", len(evs))
	}
	if ev, ok := evs[0].payload.(settingsChangedEvent); !ok || ev.Language != "vi" || !reflect.DeepEqual(ev.Fields, []string{"language"}) {
		t.Errorf("event = %#v", evs[0].payload)
	}
	if got := r.mustCall(t, "settings.get", "")["language"]; got != "vi" {
		t.Errorf("settings.get language = %v", got)
	}
	res = r.mustCall(t, "settings.update", `{"patch":{"language":"vi"}}`)
	if len(toStrings(res["applied"])) != 0 {
		t.Errorf("re-selecting the same language must be a no-op: %v", res)
	}
}

func TestSettingsLanguageExplicitChoiceEqualToDetectedIsPersisted(t *testing.T) {
	withDetector(t, fixedUI(0x042A))
	r := newRig(t)
	r.mustCall(t, "settings.update", `{"patch":{"language":"vi"}}`)
	if r.cfg.Get().Language != "vi" {
		t.Errorf("an explicit choice must be persisted even when it equals the detected language")
	}
}

func TestSettingsLanguageRejectsUnsupportedAndKeepsCorruptValues(t *testing.T) {
	withDetector(t, fixedUI(0x0409))
	r := newRig(t)
	for _, v := range []string{`"fr"`, `""`, `"vietnamese"`, `"x-vi"`} {
		_, err := r.call(t, "settings.update", `{"patch":{"language":`+v+`}}`)
		if e := wantCode(t, err, CodeInvalidSetting); e.Field != "language" {
			t.Errorf("%s: field = %q", v, e.Field)
		}
	}
	if r.cfg.Get().Language != "" {
		t.Errorf("a rejected value must not be persisted: %q", r.cfg.Get().Language)
	}
	if _, err := r.call(t, "settings.update", `{"patch":{"language":5}}`); err == nil {
		t.Error("a non-string language must be rejected")
	}
}
