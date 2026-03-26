package app

import (
	"context"
	"sync"

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
	runningScriptID     string
	cancelRunningScript context.CancelFunc

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
	return appController.globalActive && script.Enabled && appController.runningScriptID == ""
}

// GetRunningScriptID returns the ID of the currently running script, or empty.
func (appController *Controller) GetRunningScriptID() string {
	appController.stateMutex.RLock()
	defer appController.stateMutex.RUnlock()
	return appController.runningScriptID
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
	appController.cancelRunningScript = nil
}

// StartScript launches the executor for a script in a goroutine.
func (appController *Controller) StartScript(script model.Script) {
	if !appController.CanStartScript(script) {
		return
	}

	executionContext, cancelFunction := context.WithCancel(context.Background())

	appController.stateMutex.Lock()
	appController.runningScriptID = script.ID
	appController.cancelRunningScript = cancelFunction
	appController.stateMutex.Unlock()

	if appController.OnStatusChange != nil {
		appController.OnStatusChange()
	}

	go func() {
		defer func() {
			appController.ClearRunningScript()
			if appController.OnStatusChange != nil {
				appController.OnStatusChange()
			}
		}()
		appController.scriptExecutor.Execute(executionContext, script)
	}()
}

// StopRunningScript cancels the currently running script.
func (appController *Controller) StopRunningScript() {
	appController.stateMutex.Lock()
	if appController.cancelRunningScript != nil {
		appController.cancelRunningScript()
	}
	appController.stateMutex.Unlock()
}
