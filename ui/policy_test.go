package ui

import "testing"

// -selftest must not be usable as a licence bypass: it cannot start scripts, register
// hotkeys, touch user data, update, or stay running; production keeps every gate.
func TestSelfTestPolicyCannotBypassLicenceOrRunScripts(t *testing.T) {
	st := NewPolicy(true)
	if st.CheckLicense || st.RegisterHotkeys || st.ScriptsMayRun || st.CheckForUpdates || st.StartAutoActive || st.UseUserData {
		t.Fatalf("self-test policy grants production capabilities: %+v", st)
	}
	if !st.ExitWhenDone || !st.SelfTestMethods {
		t.Fatalf("self-test must exit when done: %+v", st)
	}
	p := NewPolicy(false)
	if !p.CheckLicense || !p.RegisterHotkeys || !p.ScriptsMayRun || !p.UseUserData || p.SelfTestMethods || p.ExitWhenDone {
		t.Fatalf("production policy wrong: %+v", p)
	}
}
