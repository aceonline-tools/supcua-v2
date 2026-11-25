package ui

// Policy is what the process may do. The self-test mode exists only to prove the UI host; it
// must never double as a licence bypass or run scripts, so its policy is fixed here and
// main.go reads every capability from it.
type Policy struct {
	CheckLicense    bool // production: licence gate before any UI
	RegisterHotkeys bool // global/overlay/script hotkeys (the only way scripts start)
	ScriptsMayRun   bool
	CheckForUpdates bool
	StartAutoActive bool
	UseUserData     bool // real config.json and scripts directory
	SelfTestMethods bool // register the __selftest.* driver channel
	ExitWhenDone    bool
}

// NewPolicy returns the production policy, or the restricted self-test policy.
func NewPolicy(selftest bool) Policy {
	if selftest {
		return Policy{SelfTestMethods: true, ExitWhenDone: true}
	}
	return Policy{CheckLicense: true, RegisterHotkeys: true, CheckForUpdates: true, StartAutoActive: true, UseUserData: true}
}

// NewProductionSelfTestPolicy is -selftest-production: the production startup sequence (tray,
// hotkey listener and real registrations, auto-active, overlay) against an isolated copy of a
// fixture data directory. Still no licence capability, no update network and no user data.
func NewProductionSelfTestPolicy() Policy {
	return Policy{RegisterHotkeys: true, StartAutoActive: true, SelfTestMethods: true, ExitWhenDone: true}
}
