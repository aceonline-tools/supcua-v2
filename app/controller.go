package app

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yanmad27/supcua-v2/app/status"
	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/hotkey"
	"github.com/yanmad27/supcua-v2/latency"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
)

// StatusChangeCallback is called when global or script status changes.
type StatusChangeCallback func()

// Controller coordinates the app's global state, executor, and hotkey manager.
type Controller struct {
	stateMutex          sync.RWMutex
	globalActive        bool
	editDialogOpen      bool
	hotkeyRecording     int    // active Settings hotkey recordings; > 0 blocks every hotkey action
	hotkeyEpoch         uint64 // bumped by every SetHotkeyRecording(true)
	hotkeyActionMu      sync.Mutex
	runningScriptID     string
	runningScriptName   string
	cancelRunningScript context.CancelFunc
	// stopping is set once the running script was cancelled: the slot stays held until dequeueNext
	// (dequeueDelay), but Snapshot already reports no running script so the overlay does not lag.
	stopping bool

	preview         *overlayPreview
	previewSeq      uint64
	previewNotifier func()
	scriptQueue         []model.Script

	scriptExecutor *executor.Executor
	hotkeyManager  *hotkey.Manager
	scriptStore    *storage.ScriptStore
	configStore    *storage.ConfigStore

	hub status.Hub

	// OnStatusChange is kept as an adapter for existing callers; new code subscribes via Subscribe.
	OnStatusChange StatusChangeCallback
}

// NewController creates a Controller with all dependencies.
func NewController(
	scriptExecutor *executor.Executor,
	hotkeyManager *hotkey.Manager,
	scriptStore *storage.ScriptStore,
	configStore *storage.ConfigStore,
) *Controller {
	return &Controller{
		scriptExecutor: scriptExecutor,
		hotkeyManager:  hotkeyManager,
		scriptStore:    scriptStore,
		configStore:    configStore,
		globalActive:   true,
	}
}

// IsGlobalActive returns the current global active status.
func (appController *Controller) IsGlobalActive() bool {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.globalActive
}

// ToggleGlobalStatus flips the global active state.
// When going inactive, cancels any running script.
func (appController *Controller) ToggleGlobalStatus() {
	appController.stateMutex.Lock()
	appController.globalActive = !appController.globalActive
	nowActive := appController.globalActive
	appController.stateMutex.Unlock()
	latency.Mark(latency.State)

	if !nowActive {
		appController.StopRunningScript()
	}

	appController.notify()
}

// SetGlobalActive sets the global active state; going inactive cancels any running script.
func (appController *Controller) SetGlobalActive(active bool) {
	appController.stateMutex.Lock()
	changed := appController.globalActive != active
	appController.globalActive = active
	appController.stateMutex.Unlock()
	if !changed {
		return
	}
	if !active {
		appController.StopRunningScript()
	}
	appController.notify()
}

// Snapshot returns the current status.
func (appController *Controller) Snapshot() status.Snapshot {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.snapshotLocked()
}

func (appController *Controller) snapshotLocked() status.Snapshot {
	snap := status.Snapshot{
		GlobalActive:      appController.globalActive,
		RunningScriptID:   appController.runningScriptID,
		RunningScriptName: appController.runningScriptName,
		EditDialogOpen:    appController.editDialogOpen,
		QueueLength:       len(appController.scriptQueue),
	}
	if appController.stopping {
		snap.RunningScriptID, snap.RunningScriptName = "", ""
	}
	return snap
}

// Subscribe registers a status subscriber; the latest snapshot is replayed. fn must not block.
func (appController *Controller) Subscribe(fn func(status.Snapshot)) {
	appController.hub.Subscribe(fn)
}

// publish sends the current snapshot to the hub. Must be called without stateMutex held.
func (appController *Controller) publish() {
	appController.hub.PublishFunc(appController.Snapshot)
}

// notify publishes to the hub and invokes the legacy OnStatusChange callback.
func (appController *Controller) notify() {
	appController.publish()
	if appController.OnStatusChange != nil {
		appController.OnStatusChange()
	}
}

// CanStartScript returns true if the script is allowed to run right now.
func (appController *Controller) CanStartScript(script model.Script) bool {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.globalActive && !appController.editDialogOpen && appController.hotkeyRecording == 0 && script.Enabled && appController.runningScriptID == ""
}

// SetEditDialogOpen blocks or unblocks script execution while an edit dialog is open.
// It also stops any running script when the dialog opens.
func (appController *Controller) SetEditDialogOpen(open bool) {
	appController.stateMutex.Lock()
	appController.editDialogOpen = open
	appController.stateMutex.Unlock()
	if open {
		appController.StopRunningScript()
	}
	appController.publish()
}

// SetHotkeyRecording marks a Settings hotkey recording as started or ended; calls nest, so the
// end of an older recording never clears the gate of a newer one. While any recording is active
// no hotkey action runs: script start and queue launch are refused under stateMutex, the global
// toggle via ToggleGlobalFromHotkey, and other actions via RunHotkeyAction. Starting returns only
// after an action already inside RunHotkeyAction has finished. A running script is not stopped.
func (appController *Controller) SetHotkeyRecording(on bool) {
	if on {
		appController.hotkeyActionMu.Lock()
		defer appController.hotkeyActionMu.Unlock()
	}
	appController.stateMutex.Lock()
	if on {
		appController.hotkeyRecording++
		appController.hotkeyEpoch++
	} else if appController.hotkeyRecording > 0 {
		appController.hotkeyRecording--
	}
	cleared := appController.clearPreviewLocked()
	appController.stateMutex.Unlock()
	appController.previewCleared(cleared)
}

// HotkeyEpoch is stamped on each hotkey event when it is detected (listener thread). An action
// carrying a stamp runs only if no recording is active and none started since the stamp, so input
// detected before or during a recording can never run after it ended.
func (appController *Controller) HotkeyEpoch() uint64 {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.hotkeyEpoch
}

// hotkeyAllowedLocked must be called with stateMutex held.
func (appController *Controller) hotkeyAllowedLocked(epoch uint64) bool {
	return appController.hotkeyRecording == 0 && epoch == appController.hotkeyEpoch
}

func (appController *Controller) hotkeyAllowed(epoch uint64) bool {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.hotkeyAllowedLocked(epoch)
}

// ToggleGlobalFromHotkey is the global hotkey's action: ToggleGlobalStatus unless a Settings
// recording is active (checked in the same critical section as the flip).
func (appController *Controller) ToggleGlobalFromHotkey(epoch uint64) {
	appController.stateMutex.Lock()
	cleared := appController.clearPreviewLocked()
	if !appController.hotkeyAllowedLocked(epoch) {
		appController.stateMutex.Unlock()
		appController.previewCleared(cleared)
		return
	}
	appController.globalActive = !appController.globalActive
	nowActive := appController.globalActive
	appController.stateMutex.Unlock()
	latency.Mark(latency.State)
	if !nowActive {
		appController.StopRunningScript()
	}
	appController.notify()
}

// RunHotkeyAction runs action unless a Settings recording is active, and reports whether it ran.
// action must be short and must not start a recording (SetHotkeyRecording(true) waits for it).
func (appController *Controller) RunHotkeyAction(epoch uint64, action func()) bool {
	appController.hotkeyActionMu.Lock()
	defer appController.hotkeyActionMu.Unlock()
	if !appController.hotkeyAllowed(epoch) {
		return false
	}
	action()
	return true
}

// GetRunningScriptID returns the ID of the currently running script, or empty.
func (appController *Controller) GetRunningScriptID() string {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.runningScriptID
}

// GetRunningScriptName returns the camelCase name of the currently running script, or empty.
func (appController *Controller) GetRunningScriptName() string {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.runningScriptName
}

// toSnakeCase converts a string to snake_case.
func toSnakeCase(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, "_")
}

// SetRunningScriptID marks a script as currently running.
func (appController *Controller) SetRunningScriptID(scriptID string) {
	appController.stateMutex.Lock()
	defer appController.stateMutex.Unlock()
	appController.runningScriptID = scriptID
}

// ClearRunningScript marks no script as running.
func (appController *Controller) ClearRunningScript() {
	appController.stateMutex.Lock()
	defer appController.stateMutex.Unlock()
	appController.runningScriptID = ""
	appController.runningScriptName = ""
	appController.cancelRunningScript = nil
	appController.stopping = false
}

// GetQueueLength returns the number of scripts waiting in the queue.
func (appController *Controller) GetQueueLength() int {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return len(appController.scriptQueue)
}

// launchLocked starts a script immediately. Must be called with stateMutex held.
func (appController *Controller) launchLocked(script model.Script) {
	ctx, cancel := context.WithCancel(context.Background())
	appController.runningScriptID = script.ID
	appController.runningScriptName = toSnakeCase(script.Name)
	appController.cancelRunningScript = cancel
	appController.stopping = false
	go func() {
		defer appController.dequeueNext()
		appController.scriptExecutor.Execute(ctx, script)
	}()
}

// dequeueDelay is how long a finished script holds the running slot before the next one starts.
var dequeueDelay = 100 * time.Millisecond

// dequeueNext is called when a script finishes. It clears running state and
// starts the next queued script if any.
func (appController *Controller) dequeueNext() {
	time.Sleep(dequeueDelay)
	appController.stateMutex.Lock()
	appController.runningScriptID = ""
	appController.runningScriptName = ""
	appController.cancelRunningScript = nil
	appController.stopping = false
	latency.Mark(latency.State)
	if len(appController.scriptQueue) > 0 && appController.globalActive && !appController.editDialogOpen && appController.hotkeyRecording == 0 {
		next, _ := appController.popQueueLocked()
		if next.Enabled {
			appController.launchLocked(next)
		}
	}
	appController.stateMutex.Unlock()
	appController.notify()
}

// enqueueLocked appends script unless one with the same ID is already queued.
// Must be called with stateMutex held.
func (appController *Controller) enqueueLocked(script model.Script) {
	for _, queued := range appController.scriptQueue {
		if queued.ID == script.ID {
			return
		}
	}
	appController.scriptQueue = append(appController.scriptQueue, script)
}

// popQueueLocked removes and returns the head of the queue, zeroing the
// vacated slot and dropping the backing array once empty.
// Must be called with stateMutex held.
func (appController *Controller) popQueueLocked() (model.Script, bool) {
	if len(appController.scriptQueue) == 0 {
		return model.Script{}, false
	}
	next := appController.scriptQueue[0]
	appController.scriptQueue[0] = model.Script{}
	appController.scriptQueue = appController.scriptQueue[1:]
	if len(appController.scriptQueue) == 0 {
		appController.scriptQueue = nil
	}
	return next, true
}

// StartScriptFromHotkey is StartScript for a hotkey event stamped with epoch (see HotkeyEpoch).
func (appController *Controller) StartScriptFromHotkey(script model.Script, epoch uint64) {
	appController.startScript(script, func() bool { return appController.hotkeyAllowedLocked(epoch) })
}

// StopRunningScriptFromHotkey is StopRunningScript for a hotkey event stamped with epoch; the
// emergency stop and the tray keep using the ungated StopRunningScript.
func (appController *Controller) StopRunningScriptFromHotkey(epoch uint64) {
	appController.stateMutex.Lock()
	cleared := appController.clearPreviewLocked()
	if !appController.hotkeyAllowedLocked(epoch) {
		appController.stateMutex.Unlock()
		appController.previewCleared(cleared)
		return
	}
	appController.scriptQueue = nil
	if appController.cancelRunningScript != nil {
		appController.cancelRunningScript()
		appController.stopping = true
	}
	appController.stateMutex.Unlock()
	latency.Mark(latency.State)
	appController.publish()
}

// StartScript launches a script or queues it if one is already running.
func (appController *Controller) StartScript(script model.Script) {
	appController.startScript(script, func() bool { return true })
}

// startScript launches or queues script; allowed is evaluated with stateMutex held.
func (appController *Controller) startScript(script model.Script, allowed func() bool) {
	log.Printf("StartScript called: id=%s name=%s globalActive=%v enabled=%v runningID=%q",
		script.ID, script.Name, appController.globalActive, script.Enabled, appController.runningScriptID)

	appController.stateMutex.Lock()
	if !appController.globalActive || appController.editDialogOpen || appController.hotkeyRecording > 0 || !script.Enabled || !allowed() {
		appController.stateMutex.Unlock()
		return
	}
	if appController.runningScriptID != "" {
		log.Printf("StartScript queued: id=%s name=%s queueLen=%d", script.ID, script.Name, len(appController.scriptQueue)+1)
		appController.enqueueLocked(script)
		appController.stateMutex.Unlock()
		return
	}
	appController.launchLocked(script)
	appController.stateMutex.Unlock()

	appController.notify()
}

// StopRunningScript cancels the currently running script and clears the queue.
func (appController *Controller) StopRunningScript() {
	appController.stateMutex.Lock()
	appController.scriptQueue = nil
	if appController.cancelRunningScript != nil {
		appController.cancelRunningScript()
		appController.stopping = true
	}
	appController.stateMutex.Unlock()
	appController.publish()
}
