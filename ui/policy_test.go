package ui

import (
	"strings"
	"testing"

	"github.com/yanmad27/supcua-v2/bridge"
)

// -selftest must not be usable as a licence bypass: it cannot start scripts, register
// hotkeys, touch user data, update, or stay running; production keeps every gate.
func TestSelfTestPolicyCannotBypassLicenceOrRunScripts(t *testing.T) {
	st := NewPolicy(true)
	if st.CheckLicense || st.RegisterHotkeys || st.CheckForUpdates || st.StartAutoActive || st.UseUserData {
		t.Fatalf("self-test policy grants production capabilities: %+v", st)
	}
	if !st.ExitWhenDone || !st.SelfTestMethods {
		t.Fatalf("self-test must exit when done: %+v", st)
	}
	p := NewPolicy(false)
	if !p.CheckLicense || !p.RegisterHotkeys || !p.UseUserData || p.SelfTestMethods || p.ExitWhenDone {
		t.Fatalf("production policy wrong: %+v", p)
	}
}

// Structural guard behind the self-test policy: scripts only start from registered hotkeys, so
// the API surface must never offer a way to run one. Adding such a method must fail this test
// (and force a review of the self-test policy).
func TestAPIOffersNoWayToRunScripts(t *testing.T) {
	for _, name := range bridge.MethodNames() {
		l := strings.ToLower(name)
		if strings.Contains(l, "run") || strings.Contains(l, "execute") || strings.Contains(l, "scripts.start") || strings.Contains(l, "script.start") {
			t.Errorf("API method %q can start scripts: self-test policy must be reviewed", name)
		}
	}
}
