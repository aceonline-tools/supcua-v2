package bridge

import "testing"

func TestUpdateCheckStates(t *testing.T) {
	const dl = "https://github.com/aceonline-tools/supcua-v2/releases/download/v2.0.0/supcua-v2.exe"
	cases := []struct {
		name            string
		tag, url, kind  string
		wantStatus      string
		wantTag, wantEr string
		wantURL         string
	}{
		{"up to date", "", "", "", "up_to_date", "", "", ""},
		{"update available", "2.0.0", dl, "", "available", "2.0.0", "", dl},
		{"offline", "", "", "offline", "error", "", "offline", ""},
		{"rate limited", "", "", "rate_limited", "error", "", "rate_limited", ""},
		{"other", "", "", "other", "error", "", "other", ""},
		{"tag without url is an error", "2.0.0", "", "", "error", "", "other", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.b.d.UpdateCheck = func(cur string) (string, string, string) {
				if cur != "v1" {
					t.Errorf("current = %q", cur)
				}
				return c.tag, c.url, c.kind
			}
			res := r.mustCall(t, "update.check", "")
			if res["status"] != c.wantStatus || res["current"] != "v1" {
				t.Fatalf("res = %v", res)
			}
			if got, _ := res["tag"].(string); got != c.wantTag {
				t.Errorf("tag = %q", got)
			}
			if got, _ := res["error"].(string); got != c.wantEr {
				t.Errorf("error = %q", got)
			}
			if r.host.updateURL != c.wantURL {
				t.Errorf("host update url = %q, want %q", r.host.updateURL, c.wantURL)
			}
			if n := len(r.em.named(EvUpdateAvailable)); n != 0 {
				t.Errorf("update.check must not raise the startup dialog event (%d)", n)
			}
		})
	}
}

func TestUpdateCheckUnwired(t *testing.T) {
	r := newRig(t)
	_, err := r.call(t, "update.check", "")
	wantCode(t, err, CodeInternal)
}
