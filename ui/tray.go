//go:build windows

package ui

import (
	"os"

	"github.com/lxn/walk"

	"github.com/yanmad27/supcua-v2/app"
)

// TrayIcon wraps the Walk system tray icon with context menu.
type TrayIcon struct {
	notifyIcon    *walk.NotifyIcon
	appController *app.Controller
	mainWindow    *walk.MainWindow
	overlayWindow *OverlayWindow
}

// NewTrayIcon creates and configures the system tray icon.
func NewTrayIcon(mainWindow *walk.MainWindow, appController *app.Controller, overlayWindow *OverlayWindow) (*TrayIcon, error) {
	notifyIcon, err := walk.NewNotifyIcon(mainWindow)
	if err != nil {
		return nil, err
	}

	trayIcon := &TrayIcon{
		notifyIcon:    notifyIcon,
		appController: appController,
		mainWindow:    mainWindow,
		overlayWindow: overlayWindow,
	}

	// Set icon from the executable itself, fallback to a stock icon
	executablePath, _ := os.Executable()
	appIcon, iconErr := walk.NewIconFromFile(executablePath)
	if iconErr != nil {
		appIcon, _ = walk.NewIconFromResourceId(3)
	}
	if appIcon != nil {
		notifyIcon.SetIcon(appIcon)
		mainWindow.SetIcon(appIcon)
	}

	notifyIcon.SetToolTip("supcua-v2 - Inactive")
	notifyIcon.SetVisible(true)

	trayIcon.buildContextMenu()

	notifyIcon.MouseDown().Attach(func(mouseX, mouseY int, mouseButton walk.MouseButton) {
		if mouseButton == walk.LeftButton {
			mainWindow.SetVisible(!mainWindow.Visible())
			if mainWindow.Visible() {
				mainWindow.Activate()
			}
		}
	})

	return trayIcon, nil
}

func (trayIcon *TrayIcon) buildContextMenu() {
	contextMenu := trayIcon.notifyIcon.ContextMenu()

	toggleStatusAction := walk.NewAction()
	toggleStatusAction.SetText("Toggle Active")
	toggleStatusAction.Triggered().Attach(func() {
		trayIcon.appController.ToggleGlobalStatus()
		trayIcon.UpdateTooltip()
	})
	contextMenu.Actions().Add(toggleStatusAction)

	showWindowAction := walk.NewAction()
	showWindowAction.SetText("Show Window")
	showWindowAction.Triggered().Attach(func() {
		trayIcon.mainWindow.SetVisible(true)
		trayIcon.mainWindow.Activate()
	})
	contextMenu.Actions().Add(showWindowAction)

	if trayIcon.overlayWindow != nil {
		toggleOverlayAction := walk.NewAction()
		toggleOverlayAction.SetText("Toggle Overlay")
		toggleOverlayAction.Triggered().Attach(func() {
			trayIcon.overlayWindow.ToggleVisibility()
		})
		contextMenu.Actions().Add(toggleOverlayAction)
	}

	separatorAction := walk.NewSeparatorAction()
	contextMenu.Actions().Add(separatorAction)

	exitAction := walk.NewAction()
	exitAction.SetText("Exit")
	exitAction.Triggered().Attach(func() {
		trayIcon.Dispose()
		trayIcon.mainWindow.Close()
	})
	contextMenu.Actions().Add(exitAction)
}

// UpdateTooltip refreshes the tooltip text based on current status.
func (trayIcon *TrayIcon) UpdateTooltip() {
	statusText := "Inactive"
	if trayIcon.appController.IsGlobalActive() {
		statusText = "Active"
		runningScriptID := trayIcon.appController.GetRunningScriptID()
		if runningScriptID != "" {
			statusText += " - Running: " + runningScriptID
		}
	}
	trayIcon.notifyIcon.SetToolTip("supcua-v2 - " + statusText)
}

// Dispose cleans up the tray icon.
func (trayIcon *TrayIcon) Dispose() {
	trayIcon.notifyIcon.Dispose()
}
