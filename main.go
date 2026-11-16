//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/hotkey"
	imagedetector "github.com/yanmad27/supcua-v2/image"
	"github.com/yanmad27/supcua-v2/license"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
	"github.com/yanmad27/supcua-v2/ui"
	"github.com/yanmad27/supcua-v2/updater"
	"github.com/yanmad27/supcua-v2/version"
)

func main() {
	// Single instance check via named mutex
	mutexName, _ := windows.UTF16PtrFromString("Local\\supcua-v2-instance")
	mutex, err := windows.CreateMutex(nil, true, mutexName)
	if err == windows.ERROR_ALREADY_EXISTS {
		msg, _ := windows.UTF16PtrFromString("supcua-v2 is already running.")
		title, _ := windows.UTF16PtrFromString("Already Running")
		windows.MessageBox(0, msg, title, windows.MB_OK|windows.MB_ICONINFORMATION)
		os.Exit(0)
	}
	defer windows.CloseHandle(mutex)

	executablePath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path: %v", err)
	}
	applicationDirectory := filepath.Dir(executablePath)

	// Log to file since -H windowsgui hides console
	logFile, logErr := newRotatingWriter(filepath.Join(applicationDirectory, "supcua-v2.log"), 5<<20)
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
	scriptExecutor := executor.NewExecutor(inputSimulator, imageDetectorInstance, appConfig.KeyPressDurationMs)
	hotkeyManager := hotkey.NewManager()

	// Application controller
	appController := app.NewController(scriptExecutor, hotkeyManager, scriptStore, configStore)

	// UI
	captureVK := hotkey.KeyNameToVirtualKeyCode(appConfig.CaptureHotkey.Key)
	mainWindow, mainWindowController, err := ui.CreateMainWindow(scriptStore, configStore, appConfig.MainWindowPos, captureVK)
	if err != nil {
		log.Fatalf("failed to create main window: %v", err)
	}

	// Auto-update check (runs in background, shows dialog on UI thread)
	go func() {
		latestTag, dlURL, err := updater.CheckUpdate(version.Version)
		if err != nil {
			log.Printf("update check error: %v", err)
			return
		}
		if latestTag == "" {
			return
		}
		log.Printf("update available: %s", latestTag)
		mainWindow.Synchronize(func() {
			if ui.ShowUpdateDialog(mainWindow, latestTag) && dlURL != "" {
				if err := updater.DownloadAndReplace(dlURL, executablePath); err != nil {
					log.Printf("update failed: %v", err)
				}
			}
		})
	}()

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
	trayIcon, err := ui.NewTrayIcon(mainWindow, mainWindowController, appController, overlayWindow)
	if err != nil {
		log.Fatalf("failed to create tray icon: %v", err)
	}
	defer trayIcon.Dispose()

	// Wire up global toggler
	mainWindowController.SetScriptRunner(appController)

	// Allow rctrl to toggle global active even during keyboard lock
	executor.SetEmergencyToggle(appController.ToggleGlobalStatus)

	// Hotkey bookkeeping is serialized in scriptHotkeys. It exists before any
	// callback is wired and registers nothing until scripts are set below.
	scriptHotkeys := newScriptHotkeySet(
		appController.IsGlobalActive,
		func(scripts []model.Script) []int {
			return registerScriptHotkeys(appController, hotkeyManager, scripts)
		},
		hotkeyManager.Unregister,
	)

	appController.OnStatusChange = func() {
		scriptHotkeys.Reconcile()
		mainWindow.Synchronize(func() {
			if overlayWindow != nil {
				overlayWindow.Refresh()
			}
			trayIcon.UpdateTooltip()
			mainWindowController.RefreshActiveState()
		})
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

	// Register overlay toggle hotkey
	if overlayWindow != nil && appConfig.OverlayHotkey.Key != "" {
		_, err := hotkeyManager.Register(appConfig.OverlayHotkey, func(hotkeyID int) {
			overlayWindow.ToggleVisibility()
		})
		if err != nil {
			log.Printf("warning: failed to register overlay hotkey: %v", err)
		}
	}

	// Register script hotkeys (only when global is active)
	scriptHotkeys.SetScripts(mainWindowController.Scripts())

	// Re-register script hotkeys whenever scripts change (uses in-memory list, no disk read)
	mainWindowController.OnScriptsChanged = func(scripts []model.Script) {
		_ = globalHotkeyID // keep global hotkey alive
		scriptHotkeys.SetScripts(scripts)
	}

	// Run the Walk message loop (blocks until window closed)
	mainWindow.Run()

	if !scriptStore.FlushSaves(2 * time.Second) {
		log.Println("warning: pending script saves not flushed before exit")
	}

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
	scripts []model.Script,
) []int {
	var registeredIDs []int

	for _, script := range scripts {
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
