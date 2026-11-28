package ui

// Policy is what the process may do. The self-test mode exists only to prove the UI host; it
// must never double as a licence bypass or run scripts, so its policy is fixed here and
// main.go reads every capability from it.
type Policy struct {
	CheckLicense    bool // production: licence gate before any UI
	RegisterHotkeys bool // global/overlay/script hotkeys (the only way scripts start)
	CheckForUpdates bool
	UpdateCheckStub bool // self-test only: update.check is wired, and refuses unless the loopback stub is installed
	StartAutoActive bool
	UseUserData     bool // real config.json and scripts directory
	SelfTestMethods bool // register the __selftest.* driver channel
	ExitWhenDone    bool
}

// NewPolicy returns the production policy, or the restricted self-test policy.
func NewPolicy(selftest bool) Policy {
	if selftest {
		return Policy{SelfTestMethods: true, ExitWhenDone: true, UpdateCheckStub: true}
	}
	return Policy{CheckLicense: true, RegisterHotkeys: true, CheckForUpdates: true, StartAutoActive: true, UseUserData: true}
}

// NewProductionSelfTestPolicy is -selftest-production: the production startup sequence (tray,
// hotkey listener and real registrations, auto-active, overlay) against an isolated copy of a
// fixture data directory (a temp dir that is deleted afterwards; the user's data is never used).
// Still no licence capability and no update network. It exits after its bounded run (the hard
// timeout flag plus the driver's own deadlines); it only starts the fixture's input-free probe script.
func NewProductionSelfTestPolicy() Policy {
	return Policy{RegisterHotkeys: true, StartAutoActive: true, SelfTestMethods: true, ExitWhenDone: true}
}
