package autoactive

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultProcessName is the unverified executable name of the Ace Online client.
const DefaultProcessName = "ACEonline.exe"

// DefaultPollInterval is how often the foreground window is sampled.
const DefaultPollInterval = 500 * time.Millisecond

// ResolveProcessName returns configuredName, or DefaultProcessName when it is blank.
func ResolveProcessName(configuredName string) string {
	if strings.TrimSpace(configuredName) == "" {
		return DefaultProcessName
	}
	return strings.TrimSpace(configuredName)
}

// MatchesProcess reports whether imagePath's base name equals processName, ignoring case.
// Both Windows and Unix separators are recognised so the check is platform independent.
func MatchesProcess(imagePath, processName string) bool {
	if imagePath == "" || processName == "" {
		return false
	}
	baseName := imagePath
	if separatorIndex := strings.LastIndexAny(imagePath, `\/`); separatorIndex >= 0 {
		baseName = imagePath[separatorIndex+1:]
	}
	return strings.EqualFold(baseName, processName)
}

// Options configures a Watcher.
type Options struct {
	ProcessName           string
	ForegroundProcessName func() (string, error)
	IsActive              func() bool
	SetActive             func(bool)
	PollInterval          time.Duration
}

// Watcher turns global active on and off as the game process gains and loses focus.
type Watcher struct {
	options Options

	processName atomic.Value

	lifecycleMutex sync.Mutex
	stopSignal     chan struct{}
	finished       chan struct{}
}

// NewWatcher creates a stopped Watcher.
func NewWatcher(options Options) *Watcher {
	if options.PollInterval <= 0 {
		options.PollInterval = DefaultPollInterval
	}
	watcher := &Watcher{options: options}
	watcher.SetProcessName(options.ProcessName)
	return watcher
}

// SetProcessName changes the matched process; a blank name falls back to DefaultProcessName.
// It takes effect on the next observation and is safe to call while running.
func (watcher *Watcher) SetProcessName(processName string) {
	watcher.processName.Store(ResolveProcessName(processName))
}

func (watcher *Watcher) currentProcessName() string {
	return watcher.processName.Load().(string)
}

// Start begins polling. It does nothing if the watcher is already running.
// The first observation counts as a focus transition.
func (watcher *Watcher) Start() {
	watcher.lifecycleMutex.Lock()
	defer watcher.lifecycleMutex.Unlock()
	if watcher.stopSignal != nil {
		return
	}
	watcher.stopSignal = make(chan struct{})
	watcher.finished = make(chan struct{})
	go watcher.run(watcher.stopSignal, watcher.finished)
}

// Stop halts polling and blocks until the polling goroutine has exited.
// It is safe to call repeatedly and leaves the current global state untouched.
func (watcher *Watcher) Stop() {
	watcher.lifecycleMutex.Lock()
	defer watcher.lifecycleMutex.Unlock()
	if watcher.stopSignal == nil {
		return
	}
	close(watcher.stopSignal)
	<-watcher.finished
	watcher.stopSignal = nil
	watcher.finished = nil
}

func (watcher *Watcher) run(stopSignal <-chan struct{}, finished chan<- struct{}) {
	defer close(finished)
	ticker := time.NewTicker(watcher.options.PollInterval)
	defer ticker.Stop()

	var observation focusObservation
	observation.observe(watcher.options, watcher.currentProcessName(), stopSignal)
	for {
		select {
		case <-stopSignal:
			return
		case <-ticker.C:
			observation.observe(watcher.options, watcher.currentProcessName(), stopSignal)
		}
	}
}

type focusObservation struct {
	known   bool
	focused bool
}

func (observation *focusObservation) observe(options Options, processName string, stopSignal <-chan struct{}) {
	foregroundName, err := options.ForegroundProcessName()
	if err != nil || foregroundName == "" {
		return
	}
	focused := MatchesProcess(foregroundName, processName)
	if observation.known && observation.focused == focused {
		return
	}
	observation.known = true
	observation.focused = focused

	select {
	case <-stopSignal:
		return
	default:
	}
	if focused != options.IsActive() {
		options.SetActive(focused)
	}
}
