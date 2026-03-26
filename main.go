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
	"github.com/yanmad27/supcua-v2/storage"
	"github.com/yanmad27/supcua-v2/ui"
)

func main() {
	log.SetOutput(os.Stdout)
	log.Println("supcua-v2 starting...")

	executablePath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path: %v", err)
	}
	applicationDirectory := filepath.Dir(executablePath)

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
	mainWindow, _, err := ui.CreateMainWindow(appController, scriptStore)
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

	// Status change callback — refresh overlay and tray
	appController.OnStatusChange = func() {
		if overlayWindow != nil {
			overlayWindow.Refresh()
		}
		trayIcon.UpdateTooltip()
	}

	// Register global hotkey
	_, err = hotkeyManager.Register(appConfig.GlobalHotkey, func(hotkeyID int) {
		appController.ToggleGlobalStatus()
	})
	if err != nil {
		log.Printf("warning: failed to register global hotkey: %v", err)
	}

	// Start hotkey listener in background
	go hotkeyManager.ListenForHotkeys()

	// Register script hotkeys
	registerScriptHotkeys(appController, hotkeyManager, scriptStore)

	// Run the Walk message loop (blocks until window closed)
	mainWindow.Run()

	// Cleanup
	if overlayWindow != nil {
		overlayWindow.Destroy()
	}
	log.Println("supcua-v2 exiting")
}

func registerScriptHotkeys(
	appController *app.Controller,
	hotkeyManager *hotkey.Manager,
	scriptStore *storage.ScriptStore,
) {
	allScripts, err := scriptStore.List()
	if err != nil {
		log.Printf("warning: failed to list scripts for hotkey registration: %v", err)
		return
	}

	for _, script := range allScripts {
		if !script.Enabled {
			continue
		}
		currentScript := script // capture for closure

		switch script.HotkeyMode {
		case "toggle":
			if script.ToggleHotkey != nil {
				hotkeyManager.Register(*script.ToggleHotkey, func(hotkeyID int) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScript()
					} else {
						appController.StartScript(currentScript)
					}
				})
			}
		case "separate":
			if script.StartHotkey != nil {
				hotkeyManager.Register(*script.StartHotkey, func(hotkeyID int) {
					appController.StartScript(currentScript)
				})
			}
			if script.StopHotkey != nil {
				hotkeyManager.Register(*script.StopHotkey, func(hotkeyID int) {
					if appController.GetRunningScriptID() == currentScript.ID {
						appController.StopRunningScript()
					}
				})
			}
		}
	}
}
