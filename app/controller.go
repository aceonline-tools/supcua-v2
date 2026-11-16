package app

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/hotkey"
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
	runningScriptID     string
	runningScriptName   string
	cancelRunningScript context.CancelFunc
	scriptQueue         []model.Script

	scriptExecutor *executor.Executor
	hotkeyManager  *hotkey.Manager
	scriptStore    *storage.ScriptStore
	configStore    *storage.ConfigStore

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

	if !nowActive {
		appController.StopRunningScript()
	}

	if appController.OnStatusChange != nil {
		appController.OnStatusChange()
	}
}

// CanStartScript returns true if the script is allowed to run right now.
func (appController *Controller) CanStartScript(script model.Script) bool {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.globalActive && !appController.editDialogOpen && script.Enabled && appController.runningScriptID == ""
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
	go func() {
		defer appController.dequeueNext()
		appController.scriptExecutor.Execute(ctx, script)
	}()
}

// dequeueNext is called when a script finishes. It clears running state and
// starts the next queued script if any.
func (appController *Controller) dequeueNext() {
	time.Sleep(100 * time.Millisecond)
	appController.stateMutex.Lock()
	appController.runningScriptID = ""
	appController.runningScriptName = ""
	appController.cancelRunningScript = nil
	if len(appController.scriptQueue) > 0 && appController.globalActive && !appController.editDialogOpen {
		next, _ := appController.popQueueLocked()
		if next.Enabled {
			appController.launchLocked(next)
		}
	}
	appController.stateMutex.Unlock()
	if appController.OnStatusChange != nil {
		appController.OnStatusChange()
	}
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

// StartScript launches a script or queues it if one is already running.
func (appController *Controller) StartScript(script model.Script) {
	log.Printf("StartScript called: id=%s name=%s globalActive=%v enabled=%v runningID=%q",
		script.ID, script.Name, appController.globalActive, script.Enabled, appController.runningScriptID)

	appController.stateMutex.Lock()
	if !appController.globalActive || appController.editDialogOpen || !script.Enabled {
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

	if appController.OnStatusChange != nil {
		appController.OnStatusChange()
	}
}

// StopRunningScript cancels the currently running script and clears the queue.
func (appController *Controller) StopRunningScript() {
	appController.stateMutex.Lock()
	appController.scriptQueue = nil
	if appController.cancelRunningScript != nil {
		appController.cancelRunningScript()
	}
	appController.stateMutex.Unlock()
}
