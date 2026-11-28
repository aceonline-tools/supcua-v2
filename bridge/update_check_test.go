package bridge

import (
	"encoding/json"
	"testing"
	"time"
)

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
			out, err := r.b.checkUpdate()
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(out)
			var res map[string]any
			_ = json.Unmarshal(data, &res)
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

// The handler must hand back a Deferred immediately: a slow network check may not occupy the
// single RPC worker that every other page call shares.
func TestUpdateCheckHandlerDoesNotHoldTheWorker(t *testing.T) {
	r := newRig(t)
	release := make(chan struct{})
	r.b.d.UpdateCheck = func(string) (string, string, string) { <-release; return "", "", "" }
	type token struct{}
	resolved := make(chan UpdateCheckResult, 1)
	r.b.d.NewDeferred = func() (any, func(any, error)) {
		return token{}, func(out any, _ error) { resolved <- out.(UpdateCheckResult) }
	}
	done := make(chan any, 1)
	go func() { out, _ := r.m["update.check"](nil); done <- out }()
	select {
	case out := <-done:
		if _, ok := out.(token); !ok {
			t.Fatalf("handler returned %T, want the deferred reply", out)
		}
	case <-time.After(time.Second):
		t.Fatal("update.check blocked the caller while the check was in flight")
	}
	close(release)
	select {
	case res := <-resolved:
		if res.Status != "up_to_date" {
			t.Fatalf("resolved %+v", res)
		}
	case <-time.After(time.Second):
		t.Fatal("deferred reply never resolved")
	}
}

func TestUpdateCheckUnwired(t *testing.T) {
	r := newRig(t)
	_, err := r.call(t, "update.check", "")
	wantCode(t, err, CodeInternal)
}
