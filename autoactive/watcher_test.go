package autoactive

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeEnvironment struct {
	mutex      sync.Mutex
	foreground string
	err        error
	active     bool
	setCalls   []bool
	polled     chan struct{}
}

func newFakeEnvironment(initialActive bool) *fakeEnvironment {
	return &fakeEnvironment{active: initialActive, polled: make(chan struct{}, 1024)}
}

func (environment *fakeEnvironment) foregroundProcessName() (string, error) {
	environment.mutex.Lock()
	name, err := environment.foreground, environment.err
	environment.mutex.Unlock()
	select {
	case environment.polled <- struct{}{}:
	default:
	}
	return name, err
}

func (environment *fakeEnvironment) isActive() bool {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return environment.active
}

func (environment *fakeEnvironment) setActive(active bool, generation uint64) {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	environment.setCalls = append(environment.setCalls, active)
	environment.active = active
}

func (environment *fakeEnvironment) setForeground(name string, err error) {
	environment.mutex.Lock()
	environment.foreground, environment.err = name, err
	environment.mutex.Unlock()
}

func (environment *fakeEnvironment) calls() []bool {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return append([]bool(nil), environment.setCalls...)
}

func (environment *fakeEnvironment) options() Options {
	return Options{
		ProcessName:           DefaultProcessName,
		ForegroundProcessName: environment.foregroundProcessName,
		SetActive:             environment.setActive,
		PollInterval:          time.Millisecond,
	}
}

func (environment *fakeEnvironment) awaitPolls(t *testing.T, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		select {
		case <-environment.polled:
		case <-time.After(5 * time.Second):
			t.Fatal("watcher did not poll")
		}
	}
}

func step(observation *focusObservation, environment *fakeEnvironment) {
	observation.observe(environment.options(), DefaultProcessName, 1, make(chan struct{}))
}

func TestProcessNameChangeAppliesOnNextObservation(t *testing.T) {
	environment := newFakeEnvironment(false)
	environment.setForeground(`C:\Games\other.exe`, nil)
	watcher := NewWatcher(environment.options())
	watcher.Start()
	defer watcher.Stop()
	environment.awaitPolls(t, 2)
	if calls := environment.calls(); len(calls) != 1 || calls[0] {
		t.Fatalf("want one deactivate for non-game foreground, got %v", calls)
	}

	watcher.SetProcessName("OTHER.exe")
	deadline := time.After(5 * time.Second)
	for len(environment.calls()) < 2 {
		select {
		case <-deadline:
			t.Fatal("name change not applied")
		case <-time.After(time.Millisecond):
		}
	}
	if calls := environment.calls(); !calls[1] {
		t.Fatalf("want activate, got %v", calls)
	}
}

func TestBlankProcessNameFallsBackToDefault(t *testing.T) {
	watcher := NewWatcher(Options{ProcessName: "custom.exe"})
	watcher.SetProcessName("   ")
	if got := watcher.currentProcessName(); got != DefaultProcessName {
		t.Fatalf("got %q, want default", got)
	}
	watcher.SetProcessName("")
	if got := watcher.currentProcessName(); got != DefaultProcessName {
		t.Fatalf("got %q, want default", got)
	}
}

func TestMatchesProcess(t *testing.T) {
	cases := []struct {
		path, name string
		want       bool
	}{
		{`C:\Games\Ace\ACEonline.exe`, "ACEonline.exe", true},
		{`C:\Games\Ace\aceonline.EXE`, "ACEonline.exe", true},
		{"/opt/ACEonline.exe", "aceonline.exe", true},
		{"ACEonline.exe", "ACEonline.exe", true},
		{`C:\ACEonline.exe\other.exe`, "ACEonline.exe", false},
		{`C:\Games\xACEonline.exe`, "ACEonline.exe", false},
		{"", "ACEonline.exe", false},
		{`C:\a.exe`, "", false},
	}
	for _, testCase := range cases {
		if got := MatchesProcess(testCase.path, testCase.name); got != testCase.want {
			t.Errorf("MatchesProcess(%q, %q) = %v, want %v", testCase.path, testCase.name, got, testCase.want)
		}
	}
}

func TestResolveProcessName(t *testing.T) {
	if ResolveProcessName("  ") != DefaultProcessName || ResolveProcessName("") != DefaultProcessName {
		t.Fatal("blank name should resolve to default")
	}
	if ResolveProcessName("game.exe") != "game.exe" {
		t.Fatal("configured name should be kept")
	}
}

func TestTransitionOnlyToggling(t *testing.T) {
	environment := newFakeEnvironment(false)
	var observation focusObservation

	environment.setForeground(`C:\x\aceonline.exe`, nil)
	for i := 0; i < 5; i++ {
		step(&observation, environment)
	}
	if calls := environment.calls(); len(calls) != 1 || !calls[0] {
		t.Fatalf("want one activate call, got %v", calls)
	}

	environment.setForeground(`C:\x\notepad.exe`, nil)
	for i := 0; i < 5; i++ {
		step(&observation, environment)
	}
	if calls := environment.calls(); len(calls) != 2 || calls[1] {
		t.Fatalf("want activate then deactivate, got %v", calls)
	}
}

func TestManualToggleWhileGameFocusedIsRespected(t *testing.T) {
	environment := newFakeEnvironment(false)
	var observation focusObservation
	environment.setForeground(`C:\ACEonline.exe`, nil)
	step(&observation, environment)

	environment.setActive(false, 0)
	before := len(environment.calls())
	for i := 0; i < 5; i++ {
		step(&observation, environment)
	}
	if len(environment.calls()) != before {
		t.Fatal("manual toggle was overridden while game stayed focused")
	}
	if environment.isActive() {
		t.Fatal("global should stay off")
	}
}

func TestTransitionDispatchedEvenWhenStateLooksUnchanged(t *testing.T) {
	environment := newFakeEnvironment(true)
	var observation focusObservation
	environment.setForeground(`C:\ACEonline.exe`, nil)
	step(&observation, environment)
	if calls := environment.calls(); len(calls) != 1 || !calls[0] {
		t.Fatalf("transition must dispatch regardless of current state, got %v", calls)
	}
}

func TestStaleGenerationDetectedAfterStop(t *testing.T) {
	environment := newFakeEnvironment(false)
	environment.setForeground(`C:\ACEonline.exe`, nil)
	dispatchedGenerations := make(chan uint64, 16)
	options := environment.options()
	options.SetActive = func(active bool, generation uint64) { dispatchedGenerations <- generation }
	watcher := NewWatcher(options)

	watcher.Start()
	var firstGeneration uint64
	select {
	case firstGeneration = <-dispatchedGenerations:
	case <-time.After(5 * time.Second):
		t.Fatal("no dispatch after Start")
	}
	if firstGeneration != watcher.Generation() {
		t.Fatalf("live callback generation %d must match current %d", firstGeneration, watcher.Generation())
	}

	watcher.Stop()
	if firstGeneration == watcher.Generation() {
		t.Fatalf("callback generation %d must be stale after Stop", firstGeneration)
	}

	watcher.Start()
	defer watcher.Stop()
	if firstGeneration == watcher.Generation() {
		t.Fatal("restart must not revive old generation")
	}
}

func TestErrorsAndEmptyNamesAreIgnored(t *testing.T) {
	environment := newFakeEnvironment(true)
	var observation focusObservation
	environment.setForeground("", nil)
	step(&observation, environment)
	environment.setForeground(`C:\notepad.exe`, errors.New("access denied"))
	step(&observation, environment)
	if len(environment.calls()) != 0 || observation.known {
		t.Fatal("errors/empty must not count as transitions")
	}

	environment.setForeground(`C:\ACEonline.exe`, nil)
	step(&observation, environment)
	environment.setForeground("", nil)
	step(&observation, environment)
	environment.setForeground(`C:\ACEonline.exe`, errors.New("boom"))
	step(&observation, environment)
	if calls := environment.calls(); len(calls) != 1 || !calls[0] {
		t.Fatalf("want only the initial activate, got %v", calls)
	}
}

func TestStartStopStartAndIdempotentStop(t *testing.T) {
	environment := newFakeEnvironment(false)
	environment.setForeground(`C:\ACEonline.exe`, nil)
	watcher := NewWatcher(environment.options())

	watcher.Stop()
	watcher.Start()
	watcher.Start()
	environment.awaitPolls(t, 3)
	watcher.Stop()
	watcher.Stop()
	if calls := environment.calls(); len(calls) != 1 || !calls[0] {
		t.Fatalf("want one activate, got %v", calls)
	}

	for len(environment.polled) > 0 {
		<-environment.polled
	}
	environment.setForeground(`C:\notepad.exe`, nil)
	time.Sleep(20 * time.Millisecond)
	if len(environment.polled) != 0 || len(environment.calls()) != 1 {
		t.Fatal("watcher acted after Stop")
	}

	watcher.Start()
	environment.awaitPolls(t, 2)
	watcher.Stop()
	if calls := environment.calls(); len(calls) != 2 || calls[1] {
		t.Fatalf("restart should treat first observation as transition, got %v", calls)
	}
}

func TestRepeatedStartStopDoesNotLeak(t *testing.T) {
	environment := newFakeEnvironment(false)
	watcher := NewWatcher(environment.options())
	for i := 0; i < 50; i++ {
		watcher.Start()
		watcher.Stop()
	}
}
