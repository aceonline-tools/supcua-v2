package bridge

import (
	"strings"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

const validStyle = `{"radius":16,"background":"#112233","text_color":"#ffeedd","background_opacity":0.5,"opacity":0.8,"width":220,"height":48,"x":300,"y":200}`

func TestOverlaySetStyleAppliesPersistsAndEmits(t *testing.T) {
	r := newRig(t)
	res := r.mustCall(t, "overlay.setStyle", validStyle)
	if res["radius"] != 16.0 || res["background"] != "#112233" || res["x"] != 300.0 || res["width"] != 220.0 {
		t.Fatalf("response: %v", res)
	}
	c := r.cfg.Get()
	if c.OverlayStyle.Radius != 16 || c.OverlayStyle.TextColor != "#FFEEDD" || c.OverlayStyle.Opacity != 0.8 ||
		c.OverlayStyle.BackgroundOpacity == nil || *c.OverlayStyle.BackgroundOpacity != 0.5 || c.OverlayPosition.X != 300 || c.OverlayPosition.Y != 200 {
		t.Fatalf("not persisted: %+v %+v", c.OverlayStyle, c.OverlayPosition)
	}
	if r.ov.applied != 1 || r.ov.style.Radius != 16 {
		t.Fatalf("host not applied live: %d %+v", r.ov.applied, r.ov.style)
	}
	if len(r.em.named(EvSettingsChanged)) == 0 {
		t.Fatal("settings.changed not emitted")
	}
	if got := settingsOf(c).OverlayStyle; got.Radius != 16 || got.Opacity != 0.8 || got.X != 300 {
		t.Fatalf("settings DTO: %+v", got)
	}
}

func TestOverlaySetStyleRejectsInvalidAndMissing(t *testing.T) {
	r := newRig(t)
	bad := map[string]string{
		"bad colour":        strings.Replace(validStyle, `"#112233"`, `"blue"`, 1),
		"bad text colour":   strings.Replace(validStyle, `"#ffeedd"`, `"#fff"`, 1),
		"bg opacity > 1":    strings.Replace(validStyle, `"background_opacity":0.5`, `"background_opacity":1.5`, 1),
		"negative opacity":  strings.Replace(validStyle, `"opacity":0.8`, `"opacity":-1`, 1),
		"negative radius":   strings.Replace(validStyle, `"radius":16`, `"radius":-1`, 1),
		"zero width":        strings.Replace(validStyle, `"width":220`, `"width":0`, 1),
		"absurd x":          strings.Replace(validStyle, `"x":300`, `"x":99999999`, 1),
		"unknown field":     strings.Replace(validStyle, `{`, `{"extra":1,`, 1),
		"null field":        strings.Replace(validStyle, `"radius":16`, `"radius":null`, 1),
		"missing field":     strings.Replace(validStyle, `"y":200`, `"z":1`, 1),
		"string for number": strings.Replace(validStyle, `"radius":16`, `"radius":"16"`, 1),
	}
	for name, p := range bad {
		if _, err := r.call(t, "overlay.setStyle", p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if r.ov.applied != 0 || r.cfg.Get().OverlayStyle != (model.OverlayStyle{}) {
		t.Fatal("a rejected request must change nothing")
	}
	_, err := r.call(t, "overlay.setStyle", strings.Replace(validStyle, `"#112233"`, `"blue"`, 1))
	wantCode(t, err, CodeInvalidSetting)
}

func TestOverlaySetStyleClampsToFloorsAndScreen(t *testing.T) {
	r := newRig(t)
	res := r.mustCall(t, "overlay.setStyle", `{"radius":500,"background":"#000000","text_color":"","background_opacity":0,"opacity":0.05,"width":10,"height":10,"x":30000,"y":-30000}`)
	if res["width"] != 80.0 || res["height"] != 24.0 || res["opacity"] != 0.2 || res["radius"] != 12.0 || res["background_opacity"] != 0.0 {
		t.Fatalf("floors not applied: %v", res)
	}
	if res["x"] != 1880.0 || res["y"] != -8.0 {
		t.Fatalf("position not clamped to the screen: %v", res)
	}
	if got := r.cfg.Get().OverlayPosition; got.X != 1880 || got.Y != -8 {
		t.Fatalf("clamped position must be what is persisted: %+v", got)
	}
}

func TestOverlayResetStyle(t *testing.T) {
	r := newRig(t)
	r.mustCall(t, "overlay.setStyle", validStyle)
	res := r.mustCall(t, "overlay.resetStyle", "")
	if res["radius"] != 0.0 || res["background"] != "#404040" || res["width"] != 136.0 || res["height"] != 32.0 || res["x"] != 10.0 || res["y"] != 10.0 || res["background_opacity"] != 1.0 {
		t.Fatalf("reset response: %v", res)
	}
	if c := r.cfg.Get(); c.OverlayStyle != (model.OverlayStyle{}) {
		t.Fatalf("reset must store the zero style: %+v", c.OverlayStyle)
	}
}

func TestOverlaySetStyleRollsBackWhenSaveFails(t *testing.T) {
	r := newRig(t)
	r.per.fail = true
	_, err := r.call(t, "overlay.setStyle", validStyle)
	wantCode(t, err, CodeSaveFailed)
	if r.ov.applied != 2 || r.ov.style != (model.OverlayStyle{}) {
		t.Fatalf("host must be rolled back to the old style: applied=%d %+v", r.ov.applied, r.ov.style)
	}
}

func TestOverlayDragPersistsPositionAndNotifies(t *testing.T) {
	r := newRig(t)
	r.ov.mu.Lock()
	r.ov.state.X, r.ov.state.Y = 640, 360
	r.ov.mu.Unlock()
	before := len(r.em.named(EvSettingsChanged))
	r.b.RefreshOverlay() // what the host calls when a drag ends
	if p := r.cfg.Get().OverlayPosition; p.X != 640 || p.Y != 360 {
		t.Fatalf("drag end must persist X/Y: %+v", p)
	}
	if len(r.em.named(EvSettingsChanged)) != before+1 {
		t.Fatal("frontend not notified of the new X/Y")
	}
	r.b.RefreshOverlay()
	if len(r.em.named(EvSettingsChanged)) != before+1 {
		t.Fatal("an unchanged position must not re-persist or re-emit")
	}
}

func TestOverlayStyleMethodsAreFenced(t *testing.T) {
	for _, n := range []string{"overlay.setStyle", "overlay.resetStyle"} {
		found := false
		for _, m := range mutating {
			found = found || m == n
		}
		if !found {
			t.Errorf("%s missing from the mutating list", n)
		}
	}
}
