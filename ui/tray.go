//go:build windows

package ui

import (
	"sync"

	"github.com/lxn/walk"

	"github.com/yanmad27/supcua-v2/app"
)

// TrayHost is the visible main window as the tray sees it (the WebView2 host). UI thread only.
type TrayHost interface {
	Show()
	Hide()
	Visible() bool
}

// TrayOverlay is the overlay as the tray sees it.
type TrayOverlay interface {
	ToggleVisibility()
	ToggleDraggable()
	IsDraggable() bool
}

// TrayIcon wraps the walk system tray icon with the context menu. The NotifyIcon needs a
// walk window as owner: the hidden pump window of the WebView2 host.
type TrayIcon struct {
	notifyIcon    *walk.NotifyIcon
	appController *app.Controller
	host          TrayHost
	overlay       TrayOverlay
	onExit        func()

	dragAction *walk.Action
	autoAction *walk.Action
	once       sync.Once
	disposed   bool
}

// NewTrayIcon creates and configures the system tray icon. onExit must run the ordered
// application shutdown (it replaces closing the old walk main window).
func NewTrayIcon(owner walk.Form, host TrayHost, appController *app.Controller, overlay TrayOverlay, onExit func()) (*TrayIcon, error) {
	notifyIcon, err := walk.NewNotifyIcon(owner)
	if err != nil {
		return nil, err
	}

	trayIcon := &TrayIcon{
		notifyIcon:    notifyIcon,
		appController: appController,
		host:          host,
		overlay:       overlay,
		onExit:        onExit,
	}

	// Load icon from embedded resource (compiled in via rsrc); try common IDs
	for _, id := range []int{1, 2, 3, 7} {
		if ico, err := walk.NewIconFromResourceId(id); err == nil {
			notifyIcon.SetIcon(ico)
			break
		}
	}

	notifyIcon.SetToolTip("supcua-v2 - Inactive")
	notifyIcon.SetVisible(true)

	trayIcon.buildContextMenu()

	notifyIcon.MouseDown().Attach(func(mouseX, mouseY int, mouseButton walk.MouseButton) {
		if mouseButton == walk.LeftButton {
			if host.Visible() {
				host.Hide()
			} else {
				host.Show()
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
	showWindowAction.Triggered().Attach(func() { trayIcon.host.Show() })
	contextMenu.Actions().Add(showWindowAction)

	toggleOverlayAction := walk.NewAction()
	toggleOverlayAction.SetText("Toggle Overlay")
	toggleOverlayAction.Triggered().Attach(func() { trayIcon.overlay.ToggleVisibility() })
	contextMenu.Actions().Add(toggleOverlayAction)

	trayIcon.dragAction = walk.NewAction()
	trayIcon.dragAction.SetText("Overlay: Drag OFF")
	trayIcon.dragAction.Triggered().Attach(func() {
		trayIcon.overlay.ToggleDraggable()
		trayIcon.SyncOverlayLabel()
	})
	contextMenu.Actions().Add(trayIcon.dragAction)

	contextMenu.Actions().Add(walk.NewSeparatorAction())

	exitAction := walk.NewAction()
	exitAction.SetText("Exit")
	exitAction.Triggered().Attach(func() { trayIcon.onExit() })
	contextMenu.Actions().Add(exitAction)
}

// SyncOverlayLabel refreshes "Overlay: Drag ON/OFF" from the overlay state.
func (trayIcon *TrayIcon) SyncOverlayLabel() {
	if trayIcon.disposed {
		return
	}
	if trayIcon.overlay.IsDraggable() {
		trayIcon.dragAction.SetText("Overlay: Drag ON")
	} else {
		trayIcon.dragAction.SetText("Overlay: Drag OFF")
	}
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
			autoActiveAction.SetChecked(!enabled) // save failed: the page also reports it; no walk dialog
		}
	})
	trayIcon.autoAction = autoActiveAction
	trayIcon.notifyIcon.ContextMenu().Actions().Insert(1, autoActiveAction)
}

// SetAutoActiveChecked mirrors the setting when it is changed from the Settings page.
func (trayIcon *TrayIcon) SetAutoActiveChecked(enabled bool) {
	if trayIcon.autoAction != nil && !trayIcon.disposed {
		trayIcon.autoAction.SetChecked(enabled)
	}
}

// AddAutoActiveProcessItem inserts "Auto Active Process..." after "Auto Active"; the process
// name is edited on the Settings page, so the item shows the window and navigates there.
func (trayIcon *TrayIcon) AddAutoActiveProcessItem(openSettings func()) {
	processAction := walk.NewAction()
	processAction.SetText("Auto Active Process...")
	processAction.Triggered().Attach(func() {
		trayIcon.host.Show()
		openSettings()
	})
	trayIcon.notifyIcon.ContextMenu().Actions().Insert(2, processAction)
}

// UpdateTooltip refreshes the tooltip text based on current status.
func (trayIcon *TrayIcon) UpdateTooltip() {
	if trayIcon.disposed {
		return
	}
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

// Dispose removes the tray icon (idempotent).
func (trayIcon *TrayIcon) Dispose() {
	trayIcon.once.Do(func() {
		trayIcon.disposed = true
		trayIcon.notifyIcon.Dispose()
	})
}

// Disposed reports whether the icon was removed.
func (trayIcon *TrayIcon) Disposed() bool { return trayIcon.disposed }
