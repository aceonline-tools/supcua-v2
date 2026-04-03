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

// StartScript launches the executor for a script in a goroutine.
func (appController *Controller) StartScript(script model.Script) {
	log.Printf("StartScript called: id=%s name=%s globalActive=%v enabled=%v runningID=%q",
		script.ID, script.Name, appController.globalActive, script.Enabled, appController.runningScriptID)
	if !appController.CanStartScript(script) {
		log.Printf("StartScript blocked: globalActive=%v enabled=%v runningID=%q",
			appController.globalActive, script.Enabled, appController.runningScriptID)
		return
	}

	executionContext, cancelFunction := context.WithCancel(context.Background())

	appController.stateMutex.Lock()
	appController.runningScriptID = script.ID
	appController.runningScriptName = toSnakeCase(script.Name)
	appController.cancelRunningScript = cancelFunction
	appController.stateMutex.Unlock()

	if appController.OnStatusChange != nil {
		appController.OnStatusChange()
	}

	go func() {
		defer func() {
			time.Sleep(100 * time.Millisecond)
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
