//go:build windows

package ui

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/app"
)

// TrayIcon wraps the Walk system tray icon with context menu.
type TrayIcon struct {
	notifyIcon           *walk.NotifyIcon
	appController        *app.Controller
	mainWindow           *walk.MainWindow
	mainWindowController *MainWindowController
	overlayWindow        *OverlayWindow
}

// NewTrayIcon creates and configures the system tray icon.
func NewTrayIcon(mainWindow *walk.MainWindow, mainWindowController *MainWindowController, appController *app.Controller, overlayWindow *OverlayWindow) (*TrayIcon, error) {
	notifyIcon, err := walk.NewNotifyIcon(mainWindow)
	if err != nil {
		return nil, err
	}

	trayIcon := &TrayIcon{
		notifyIcon:           notifyIcon,
		appController:        appController,
		mainWindow:           mainWindow,
		mainWindowController: mainWindowController,
		overlayWindow:        overlayWindow,
	}

	// Load icon from embedded resource (compiled in via rsrc); try common IDs
	var appIcon *walk.Icon
	for _, id := range []int{1, 2, 3, 7} {
		if ico, err := walk.NewIconFromResourceId(id); err == nil {
			appIcon = ico
			break
		}
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
			if mainWindow.Visible() && !win.IsIconic(mainWindow.Handle()) {
				mainWindow.SetVisible(false)
			} else {
				showMainWindow(mainWindow)
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
		showMainWindow(trayIcon.mainWindow)
	})
	contextMenu.Actions().Add(showWindowAction)

	if trayIcon.overlayWindow != nil {
		toggleOverlayAction := walk.NewAction()
		toggleOverlayAction.SetText("Toggle Overlay")
		toggleOverlayAction.Triggered().Attach(func() {
			trayIcon.overlayWindow.ToggleVisibility()
		})
		contextMenu.Actions().Add(toggleOverlayAction)

		dragOverlayAction := walk.NewAction()
		dragOverlayAction.SetText("Overlay: Drag OFF")
		dragOverlayAction.Triggered().Attach(func() {
			trayIcon.overlayWindow.ToggleDraggable()
			if trayIcon.overlayWindow.IsDraggable() {
				dragOverlayAction.SetText("Overlay: Drag ON")
			} else {
				dragOverlayAction.SetText("Overlay: Drag OFF")
			}
		})
		contextMenu.Actions().Add(dragOverlayAction)
	}

	separatorAction := walk.NewSeparatorAction()
	contextMenu.Actions().Add(separatorAction)

	exitAction := walk.NewAction()
	exitAction.SetText("Exit")
	exitAction.Triggered().Attach(func() {
		trayIcon.Dispose()
		trayIcon.mainWindowController.Exit()
	})
	contextMenu.Actions().Add(exitAction)
}

// AddAutoActiveToggle inserts a checkable "Auto Active" item right after "Toggle Active".
func (trayIcon *TrayIcon) AddAutoActiveToggle(initiallyEnabled bool, onChange func(enabled bool) error) {
	autoActiveAction := walk.NewAction()
	autoActiveAction.SetText("Auto Active")
	autoActiveAction.SetCheckable(true)
	autoActiveAction.SetChecked(initiallyEnabled)
	autoActiveAction.Triggered().Attach(func() {
		enabled := autoActiveAction.Checked()
		if err := onChange(enabled); err != nil {
			autoActiveAction.SetChecked(!enabled)
			walk.MsgBox(trayIcon.mainWindow, "Auto Active", "Failed to save setting: "+err.Error(), walk.MsgBoxIconError)
		}
	})
	trayIcon.notifyIcon.ContextMenu().Actions().Insert(1, autoActiveAction)
}

// AddAutoActiveProcessItem inserts "Auto Active Process..." after the "Auto Active" item.
// currentName supplies the dialog's initial value; onChange receives the accepted name.
func (trayIcon *TrayIcon) AddAutoActiveProcessItem(currentName func() string, onChange func(processName string) error) {
	processAction := walk.NewAction()
	processAction.SetText("Auto Active Process...")
	processAction.Triggered().Attach(func() {
		if processName, accepted := ShowAutoActiveProcessDialog(trayIcon.mainWindow, currentName()); accepted {
			if err := onChange(processName); err != nil {
				walk.MsgBox(trayIcon.mainWindow, "Auto Active Process", "Failed to save setting: "+err.Error(), walk.MsgBoxIconError)
			}
		}
	})
	trayIcon.notifyIcon.ContextMenu().Actions().Insert(2, processAction)
}

// showMainWindow restores and activates the main window, handling minimized state.
func showMainWindow(mainWindow *walk.MainWindow) {
	if win.IsIconic(mainWindow.Handle()) {
		win.ShowWindow(mainWindow.Handle(), win.SW_RESTORE)
	}
	mainWindow.SetVisible(true)
	mainWindow.Activate()
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
