//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/hotkey"
	imagedetector "github.com/yanmad27/supcua-v2/image"
	"github.com/yanmad27/supcua-v2/license"
	"github.com/yanmad27/supcua-v2/storage"
	"github.com/yanmad27/supcua-v2/ui"
)

func main() {
	executablePath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path: %v", err)
	}
	applicationDirectory := filepath.Dir(executablePath)

	// Log to file since -H windowsgui hides console
	logFile, logErr := os.OpenFile(filepath.Join(applicationDirectory, "supcua-v2.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if logErr == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}
	log.Println("supcua-v2 starting...")

	// License check
	allowed, err := license.CheckLicense()
	if err != nil {
		log.Printf("license check error: %v", err)
	}
	if !allowed {
		log.Println("not licensed, showing MAC dialog")
		ui.ShowNotLicensedDialog(license.GetMacAddresses())
		return
	}

	// Storage
	configStore := storage.NewConfigStore(filepath.Join(applicationDirectory, "config.json"))
	appConfig, err := configStore.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	scriptsDirectory := appConfig.ScriptsDir
	if !filepath.IsAbs(scriptsDirectory) {
		scriptsDirectory = filepath.Join(applicationDirectory, scriptsDirectory)
	}
	scriptStore := storage.NewScriptStore(scriptsDirectory)

	// Core components
	inputSimulator := executor.NewInputSimulator()
	imageDetectorInstance := imagedetector.NewDetector(10)
	scriptExecutor := executor.NewExecutor(inputSimulator, imageDetectorInstance)
	hotkeyManager := hotkey.NewManager()

	// Application controller
	appController := app.NewController(scriptExecutor, hotkeyManager, scriptStore, configStore)

	// UI
	captureVK := hotkey.KeyNameToVirtualKeyCode(appConfig.CaptureHotkey.Key)
	mainWindow, mainWindowController, err := ui.CreateMainWindow(scriptStore, configStore, appConfig.MainWindowPos, captureVK)
	if err != nil {
		log.Fatalf("failed to create main window: %v", err)
	}

	// Overlay
	var overlayWindow *ui.OverlayWindow
	if appConfig.OverlayEnabled {
		overlayWindow, err = ui.NewOverlayWindow(
			appController,
			appConfig.OverlayPosition.X,
			appConfig.OverlayPosition.Y,
			true,
		)
		if err != nil {
			log.Printf("warning: failed to create overlay: %v", err)
		}
	}

	// System tray
	trayIcon, err := ui.NewTrayIcon(mainWindow, appController, overlayWindow)
	if err != nil {
		log.Fatalf("failed to create tray icon: %v", err)
	}
	defer trayIcon.Dispose()

	// Wire up global toggler
	mainWindowController.SetScriptRunner(appController)

	// Declare hotkey state before closures that reference it
	var scriptHotkeyIDs []int

	unregisterScriptHotkeys := func() {
		for _, id := range scriptHotkeyIDs {
			hotkeyManager.Unregister(id)
		}
		scriptHotkeyIDs = nil
	}

	// Track previous active state to only re-register hotkeys on global toggle, not script start/stop
	prevActive := true // initial state is active
	appController.OnStatusChange = func() {
		isActive := appController.IsGlobalActive()
		if isActive != prevActive {
			prevActive = isActive
			if isActive {
				scriptHotkeyIDs = registerScriptHotkeys(appController, hotkeyManager, scriptStore)
			} else {
				unregisterScriptHotkeys()
			}
		}
		if overlayWindow != nil {
			overlayWindow.Refresh()
		}
		trayIcon.UpdateTooltip()
		mainWindowController.RefreshActiveState()
	}

	// Start hotkey listener first (Register sends to its channel)
	go hotkeyManager.ListenForHotkeys()

	// Register global hotkey
	globalHotkeyID, err := hotkeyManager.Register(appConfig.GlobalHotkey, func(hotkeyID int) {
		appController.ToggleGlobalStatus()
	})
	if err != nil {
		log.Printf("warning: failed to register global hotkey: %v", err)
		globalHotkeyID = 0
	}

	// Register script hotkeys (only when global is active)
	scriptHotkeyIDs = registerScriptHotkeys(appController, hotkeyManager, scriptStore)

	// Re-register script hotkeys whenever scripts change
	mainWindowController.OnScriptsChanged = func() {
		unregisterScriptHotkeys()
		_ = globalHotkeyID // keep global hotkey alive
		if appController.IsGlobalActive() {
			scriptHotkeyIDs = registerScriptHotkeys(appController, hotkeyManager, scriptStore)
		}
	}

	// Run the Walk message loop (blocks until window closed)
	mainWindow.Run()

	// Save overlay position before destroying
	if overlayWindow != nil {
		cfg, err := configStore.Load()
		if err == nil {
			ox, oy := overlayWindow.GetPosition()
			cfg.OverlayPosition.X = ox
			cfg.OverlayPosition.Y = oy
			configStore.Save(cfg)
		}
		overlayWindow.Destroy()
	}
	log.Println("supcua-v2 exiting")
}

func registerScriptHotkeys(
	appController *app.Controller,
	hotkeyManager *hotkey.Manager,
	scriptStore *storage.ScriptStore,
) []int {
	allScripts, err := scriptStore.List()
	if err != nil {
		log.Printf("warning: failed to list scripts for hotkey registration: %v", err)
		return nil
	}

	var registeredIDs []int

	for _, script := range allScripts {
		if !script.Enabled {
			continue
		}
		currentScript := script // capture for closure

		switch script.HotkeyMode {
		case "toggle":
			if script.ToggleHotkey != nil {
				id, err := hotkeyManager.Register(*script.ToggleHotkey, func(hotkeyID int) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScript()
					} else {
						appController.StartScript(currentScript)
					}
				})
				if err != nil {
					log.Printf("warning: failed to register toggle hotkey for script %s: %v", currentScript.Name, err)
				} else {
					registeredIDs = append(registeredIDs, id)
				}
			}
		case "separate":
			if script.StartHotkey != nil {
				id, err := hotkeyManager.Register(*script.StartHotkey, func(hotkeyID int) {
					appController.StartScript(currentScript)
				})
				if err != nil {
					log.Printf("warning: failed to register start hotkey for script %s: %v", currentScript.Name, err)
				} else {
					registeredIDs = append(registeredIDs, id)
				}
			}
			if script.StopHotkey != nil {
				id, err := hotkeyManager.Register(*script.StopHotkey, func(hotkeyID int) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScript()
					}
				})
				if err != nil {
					log.Printf("warning: failed to register stop hotkey for script %s: %v", currentScript.Name, err)
				} else {
					registeredIDs = append(registeredIDs, id)
				}
			}
		}
	}
	return registeredIDs
}
