package bridge

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/updater"
)

const (
	dlA = "https://github.com/aceonline-tools/supcua-v2/releases/download/v2.0.0/supcua-v2.exe"
	dlB = "https://github.com/aceonline-tools/supcua-v2/releases/download/v2.0.1/supcua-v2.exe"
)

func runCheck(t *testing.T, r *rig) map[string]any {
	t.Helper()
	out, err := r.b.checkUpdate()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(out)
	var res map[string]any
	_ = json.Unmarshal(data, &res)
	return res
}

func TestUpdateCheckStates(t *testing.T) {
	cases := []struct {
		name            string
		tag, url, kind  string
		wantStatus      string
		wantTag, wantEr string
		wantOffer       updater.Offer
	}{
		{"up to date", "", "", "", "up_to_date", "", "", updater.Offer{}},
		{"update available", "2.0.0", dlA, "", "available", "2.0.0", "", updater.Offer{Version: "2.0.0", URL: dlA}},
		{"offline", "", "", "offline", "error", "", "offline", updater.Offer{}},
		{"rate limited", "", "", "rate_limited", "error", "", "rate_limited", updater.Offer{}},
		{"other", "", "", "other", "error", "", "other", updater.Offer{}},
		{"tag without url is an error", "2.0.0", "", "", "error", "", "other", updater.Offer{}},
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
			res := runCheck(t, r)
			if res["status"] != c.wantStatus || res["current"] != "v1" {
				t.Fatalf("res = %v", res)
			}
			if got, _ := res["tag"].(string); got != c.wantTag {
				t.Errorf("tag = %q", got)
			}
			if got, _ := res["error"].(string); got != c.wantEr {
				t.Errorf("error = %q", got)
			}
			if got := r.b.d.Offers.Current(); got != c.wantOffer {
				t.Errorf("offer = %+v, want %+v", got, c.wantOffer)
			}
			if n := len(r.em.named(EvUpdateAvailable)); n != 0 {
				t.Errorf("update.check must not raise the startup dialog event (%d)", n)
			}
		})
	}
}

// An older check finishing after a newer one must neither overwrite the offer nor report its own.
func TestUpdateCheckOlderCheckCannotOverwriteNewer(t *testing.T) {
	r := newRig(t)
	releaseOld := make(chan struct{})
	started := make(chan struct{}, 2)
	n := 0
	r.b.d.UpdateCheck = func(string) (string, string, string) {
		n++
		if n == 1 {
			started <- struct{}{}
			<-releaseOld
			return "2.0.0", dlA, ""
		}
		return "2.0.1", dlB, ""
	}
	oldRes := make(chan map[string]any, 1)
	go func() { oldRes <- runCheck(t, r) }()
	<-started
	if res := runCheck(t, r); res["tag"] != "2.0.1" {
		t.Fatalf("newer check = %v", res)
	}
	close(releaseOld)
	if res := <-oldRes; res["tag"] != "2.0.1" || res["status"] != "available" {
		t.Fatalf("older check reported %v, want the newer offer", res)
	}
	if got := r.b.d.Offers.Current(); got.URL != dlB || got.Version != "2.0.1" {
		t.Fatalf("offer = %+v, want B", got)
	}
}

func TestUpdateApplyAndSkipCarryTheOfferedVersion(t *testing.T) {
	r := newRig(t)
	r.mustCall(t, "update.apply", `{"version":"2.0.1"}`)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.host.mu.Lock()
		got := append([]string(nil), r.host.applied...)
		r.host.mu.Unlock()
		if len(got) == 1 {
			if got[0] != "2.0.1" {
				t.Fatalf("applied %v", got)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.mustCall(t, "update.skip", `{"version":"2.0.1"}`)
	for _, m := range []string{"update.apply", "update.skip"} {
		for _, params := range []string{"", "{}", `{"version":""}`, `{"version":null}`} {
			_, err := r.call(t, m, params)
			wantCode(t, err, CodeBadRequest)
		}
	}
}

func TestUpdateApplyStaleOfferEmitsSpecificMessage(t *testing.T) {
	r := newRig(t)
	r.host.updateErr = updater.ErrStaleOffer
	r.mustCall(t, "update.apply", `{"version":"2.0.0"}`)
	deadline := time.Now().Add(2 * time.Second)
	for len(r.em.named(EvUpdateFailed)) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := r.em.named(EvUpdateFailed); len(got) != 1 || got[0].payload.(updateFailedEvent).Message != UpdateStaleMessage {
		t.Fatalf("events = %+v", got)
	}
}

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

func TestUpdateCheckPanicResolvesWithAnError(t *testing.T) {
	r := newRig(t)
	r.b.d.UpdateCheck = func(string) (string, string, string) { panic("boom") }
	type result struct {
		out any
		err error
	}
	resolved := make(chan result, 1)
	r.b.d.NewDeferred = func() (any, func(any, error)) {
		return struct{}{}, func(out any, err error) { resolved <- result{out, err} }
	}
	if _, err := r.m["update.check"](nil); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-resolved:
		wantCode(t, res.err, CodeInternal)
	case <-time.After(2 * time.Second):
		t.Fatal("a panic in the check left the reply unresolved")
	}
}

func TestUpdateCheckUnwired(t *testing.T) {
	r := newRig(t)
	_, err := r.call(t, "update.check", "")
	wantCode(t, err, CodeInternal)
	r.b.d.UpdateCheck = func(string) (string, string, string) { return "", "", "" }
	r.b.d.Offers = nil
	_, err = r.call(t, "update.check", "")
	wantCode(t, err, CodeInternal)
}
