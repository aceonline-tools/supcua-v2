package locale

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ui(id uint16, ok bool) func() (uint16, bool) { return func() (uint16, bool) { return id, ok } }
func pref(tags ...string) func() []string         { return func() []string { return tags } }

func TestParse(t *testing.T) {
	for in, want := range map[string]Locale{"vi": VI, "VI": VI, " vi-VN ": VI, "vi_VN": VI, "en": EN, "en-US": EN, "EN-gb": EN} {
		if got, ok := Parse(in); !ok || got != want {
			t.Errorf("Parse(%q) = %v,%v want %v,true", in, got, ok, want)
		}
	}
	for _, in := range []string{"", " ", "fr", "123", "vietnamese", "-vi", "x-vi", "vi.VN"} {
		if _, ok := Parse(in); ok {
			t.Errorf("Parse(%q) must be rejected", in)
		}
	}
}

func TestDetectTable(t *testing.T) {
	cases := []struct {
		name string
		d    Detector
		want Locale
	}{
		{"vi-VN 0x042A", Detector{UILanguage: ui(0x042A, true)}, VI},
		{"vi other sublang 0x082A", Detector{UILanguage: ui(0x082A, true)}, VI},
		{"en-US 0x0409", Detector{UILanguage: ui(0x0409, true)}, EN},
		{"en-US wins over a vi preferred list", Detector{UILanguage: ui(0x0409, true), Preferred: pref("vi-VN")}, EN},
		{"fr-FR", Detector{UILanguage: ui(0x040C, true)}, EN},
		{"detector failure", Detector{UILanguage: ui(0, false)}, EN},
		{"no detector at all", Detector{}, EN},
		{"id 0 without preferred", Detector{UILanguage: ui(0, true)}, EN},
		{"custom 0x1000, preferred vi-VN (Q2 fallback)", Detector{UILanguage: ui(0x1000, true), Preferred: pref("vi-VN", "en-US")}, VI},
		{"custom 0x1400, preferred vi (Q2 fallback)", Detector{UILanguage: ui(0x1400, true), Preferred: pref("vi")}, VI},
		{"custom 0x0C00, preferred vi_VN", Detector{UILanguage: ui(0x0C00, true), Preferred: pref("vi_VN")}, VI},
		{"custom range, first preferred is en", Detector{UILanguage: ui(0x1000, true), Preferred: pref("en-US", "vi-VN")}, EN},
		{"custom range, empty preferred", Detector{UILanguage: ui(0x1000, true), Preferred: pref()}, EN},
		{"custom range, unsupported preferred", Detector{UILanguage: ui(0x1400, true), Preferred: pref("fr-FR")}, EN},
		{"UILanguage failed, preferred vi", Detector{UILanguage: ui(0, false), Preferred: pref("vi-VN")}, VI},
		{"real non-vi id never consults preferred", Detector{UILanguage: ui(0x0407, true), Preferred: pref("vi-VN")}, EN},
	}
	for _, c := range cases {
		if got := c.d.Detect(); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestResolveOrder(t *testing.T) {
	vi := Detector{UILanguage: ui(0x042A, true)}
	en := Detector{UILanguage: ui(0x0409, true)}
	for _, c := range []struct {
		persisted string
		d         Detector
		want      Locale
	}{
		{"", vi, VI}, {"", en, EN}, {"en", vi, EN}, {"vi", en, VI}, {"VI-vn", en, VI},
		{"fr", vi, VI}, {"garbage", en, EN}, {"  ", vi, VI},
	} {
		if got := Resolve(c.persisted, c.d); got != c.want {
			t.Errorf("Resolve(%q) = %v want %v", c.persisted, got, c.want)
		}
	}
}

func writeCfg(t *testing.T, content string) string {
	dir := t.TempDir()
	if content != "\x00missing" {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestEarly(t *testing.T) {
	t.Cleanup(func() { Set(EN) })
	enWin := Detector{UILanguage: ui(0x0409, true)}
	viWin := Detector{UILanguage: ui(0x042A, true)}
	huge := `{"language":"vi","pad":"` + strings.Repeat("x", 2<<20) + `"}`
	for _, c := range []struct {
		name, cfg string
		d         Detector
		want      Locale
	}{
		{"persisted vi", `{"language":"vi"}`, enWin, VI},
		{"persisted en on vi windows", `{"language":"en"}`, viWin, EN},
		{"unset detects", `{"theme":"dark"}`, viWin, VI},
		{"missing file", "\x00missing", viWin, VI},
		{"corrupt json", `{"language":`, viWin, VI},
		{"not an object", `[1,2]`, enWin, EN},
		{"wrong type number", `{"language":5}`, viWin, VI},
		{"wrong type null", `{"language":null}`, viWin, VI},
		{"unsupported value", `{"language":"fr"}`, viWin, VI},
		{"other keys wrong typed are ignored", `{"theme":5,"language":"vi"}`, enWin, VI},
		{"oversized file", huge, enWin, EN},
	} {
		Set(EN)
		if got := Early(writeCfg(t, c.cfg), c.d); got != c.want || Current() != c.want {
			t.Errorf("%s: Early=%v Current=%v want %v", c.name, got, Current(), c.want)
		}
	}
}

func TestSetRejectsUnsupported(t *testing.T) {
	t.Cleanup(func() { Set(EN) })
	Set(VI)
	Set("fr")
	if Current() != VI {
		t.Errorf("Set(fr) changed the language to %v", Current())
	}
}

func TestSetStoresCanonicalLocale(t *testing.T) {
	t.Cleanup(func() { Set(EN) })
	Set("VI-vn")
	if Current() != VI {
		t.Errorf("Set(VI-vn): Current = %q, want vi", Current())
	}
	if got := T("tray.exit"); got != "Thoát" {
		t.Errorf("T after Set(VI-vn) = %q, want the Vietnamese text", got)
	}
}

func TestInterpolateAndPlural(t *testing.T) {
	if got := Interpolate("a {x} b {y} {z}", Params{"x": "{y}", "y": 3}); got != "a {y} b 3 {z}" {
		t.Errorf("single-pass interpolation: %q", got)
	}
	c := Catalog{"x.count": {EN: Text{Forms: map[string]string{"one": "{n} script", "other": "{n} scripts"}}, VI: Text{Forms: map[string]string{"other": "{n} script"}}}}
	e := c["x.count"]
	for _, tc := range []struct {
		l    Locale
		n    int
		want string
	}{{EN, 1, "1 script"}, {EN, 0, "0 scripts"}, {EN, 2, "2 scripts"}, {VI, 1, "1 script"}, {VI, 5, "5 script"}} {
		p := Params{"n": tc.n}
		if got := Interpolate(pick(e.text(tc.l), tc.l, p), p); got != tc.want {
			t.Errorf("%v n=%d: %q want %q", tc.l, tc.n, got, tc.want)
		}
	}
}

func TestTFallbacks(t *testing.T) {
	if got := TL(VI, "no.such.id"); got != "no.such.id" {
		t.Errorf("unknown id must return the id, got %q", got)
	}
	if got := TL(EN, "tray.exit"); got != "Exit" {
		t.Errorf("en tray.exit = %q", got)
	}
	if got := TL(VI, "tray.exit"); got != "Thoát" {
		t.Errorf("vi tray.exit = %q", got)
	}
	if got := TL(VI, "tray.tooltipRunning", Params{"script": "Farm"}); !strings.HasSuffix(got, "Farm") || strings.Contains(got, "{") {
		t.Errorf("vi tooltip = %q", got)
	}
}

func TestBootJSON(t *testing.T) {
	en, vi := BootJSON(EN), BootJSON(VI)
	if !strings.Contains(en, `"loader.starting":"Starting…"`) || !strings.Contains(vi, `"loader.starting":"Đang khởi động…"`) {
		t.Errorf("boot json: %s | %s", en, vi)
	}
	if strings.Contains(en, "tray.") {
		t.Errorf("boot json leaked non-boot ids: %s", en)
	}
}
