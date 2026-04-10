//go:build windows

package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
	"github.com/yanmad27/supcua-v2/version"
)

// ScriptRunner can start/stop scripts.
type ScriptRunner interface {
	IsGlobalActive() bool
	ToggleGlobalStatus()
	StartScript(script model.Script)
	StopRunningScript()
	GetRunningScriptID() string
	SetEditDialogOpen(open bool)
}

// MainWindowController manages the main application window with script list.
type MainWindowController struct {
	mainWindow     *walk.MainWindow
	scriptStore    *storage.ScriptStore
	configStore    *storage.ConfigStore
	captureVK      uint32
	tableView      *walk.TableView
	tableModel     *scriptTableModel
	activeCheckbox *walk.CheckBox
	scriptRunner   ScriptRunner

	// Called after scripts are created/edited/deleted to re-register hotkeys
	OnScriptsChanged func()

	// State
	allScripts []model.Script
}

// Exit closes the window (used by tray Exit action).
func (wc *MainWindowController) Exit() {
	wc.mainWindow.Close()
}

// SetScriptRunner sets the app controller for global active state.
func (wc *MainWindowController) SetScriptRunner(toggler ScriptRunner) {
	wc.scriptRunner = toggler
	if toggler != nil {
		wc.activeCheckbox.SetChecked(toggler.IsGlobalActive())
	}
}

// RefreshActiveState syncs the checkbox with the current global state.
func (wc *MainWindowController) RefreshActiveState() {
	if wc.scriptRunner != nil {
		wc.activeCheckbox.SetChecked(wc.scriptRunner.IsGlobalActive())
	}
}

// scriptTableModel implements walk.TableModel with check support.
type scriptTableModel struct {
	walk.TableModelBase
	items   []scriptRow
	checked map[int]bool
	wc      *MainWindowController
}

type scriptRow struct {
	name    string
	hotkey  string
	runMode string
}

func (m *scriptTableModel) RowCount() int { return len(m.items) }

func (m *scriptTableModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.items) {
		return ""
	}
	switch col {
	case 0:
		return m.items[row].name
	case 1:
		return m.items[row].hotkey
	case 2:
		return m.items[row].runMode
	}
	return ""
}

func (m *scriptTableModel) Checked(index int) bool {
	return m.checked[index]
}

func (m *scriptTableModel) SetChecked(index int, checked bool) error {
	m.checked[index] = checked
	if m.wc != nil && index >= 0 && index < len(m.wc.allScripts) {
		script := m.wc.allScripts[index]
		script.Enabled = checked
		m.wc.scriptStore.Save(script)
		m.wc.allScripts[index] = script
		m.wc.notifyScriptsChanged()
	}
	return nil
}

// CreateMainWindow builds and returns the main application window.
func CreateMainWindow(scriptStore *storage.ScriptStore, configStore *storage.ConfigStore, savedPos model.WindowPos, captureVK uint32) (*walk.MainWindow, *MainWindowController, error) {
	wc := &MainWindowController{
		scriptStore: scriptStore,
		configStore: configStore,
		captureVK:   captureVK,
		tableModel:  &scriptTableModel{checked: map[int]bool{}, wc: nil},
	}

	var mainWindowInstance *walk.MainWindow

	err := (MainWindow{
		AssignTo: &mainWindowInstance,
		Title:    "supcua-v2 v" + version.Version,
		MinSize:  Size{Width: 390, Height: 200},
		Size:     Size{Width: 390, Height: 300}, // overridden below if saved size exists
		Layout:   VBox{Spacing: 2, Margins: Margins{Left: 5, Top: 5, Right: 5, Bottom: 2}},
		Visible:  true,
		Children: []Widget{
			TableView{
				AssignTo:         &wc.tableView,
				AlternatingRowBG: true,
				ColumnsOrderable: false,
				MultiSelection:   false,
				CheckBoxes:       true,
				Model:            wc.tableModel,
				Columns: []TableViewColumn{
					{Title: "Name", Width: 200},
					{Title: "Hotkey", Width: 75},
					{Title: "Mode", Width: 50},
				},
				OnItemActivated: func() { wc.onEditScript() },
				OnSizeChanged: func() {
					wc.resizeTableColumns()
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					CheckBox{
						AssignTo: &wc.activeCheckbox,
						Text:     "Global Active",
						OnCheckedChanged: func() {
							if wc.scriptRunner != nil {
								isActive := wc.scriptRunner.IsGlobalActive()
								wantActive := wc.activeCheckbox.Checked()
								if isActive != wantActive {
									wc.scriptRunner.ToggleGlobalStatus()
								}
							}
						},
					},
					HSpacer{},
					PushButton{
						Text:      "New",
						OnClicked: func() { wc.onNewScript() },
					},
					PushButton{
						Text:      "Edit",
						OnClicked: func() { wc.onEditScript() },
					},
					PushButton{
						Text:      "Delete",
						OnClicked: func() { wc.onDeleteScript() },
					},
					PushButton{
						Text:      "Hide",
						OnClicked: func() { mainWindowInstance.SetVisible(false) },
					},
				},
			},
		},
	}).Create()

	if err != nil {
		return nil, nil, err
	}

	wc.mainWindow = mainWindowInstance
	wc.tableModel.wc = wc
	wc.tableView.SetSelectionHiddenWithoutFocus(false)
	wc.refreshScriptList()

	// Auto-select first item
	if len(wc.allScripts) > 0 {
		wc.tableView.SetCurrentIndex(0)
	}

	// Restore saved position and size
	if savedPos.X != 0 || savedPos.Y != 0 || savedPos.Width != 0 || savedPos.Height != 0 {
		b := mainWindowInstance.Bounds()
		w, h := b.Width, b.Height
		if savedPos.Width > 0 {
			w = savedPos.Width
		}
		if savedPos.Height > 0 {
			h = savedPos.Height
		}
		x, y := b.X, b.Y
		// -32000 is Windows' sentinel for minimized window position; skip off-screen coords
		if savedPos.X > -1000 && savedPos.Y > -1000 {
			x, y = savedPos.X, savedPos.Y
		}
		mainWindowInstance.SetBounds(walk.Rectangle{X: x, Y: y, Width: w, Height: h})
	}

	// Save position on close, then exit for real
	mainWindowInstance.Closing().Attach(func(cancelled *bool, reason walk.CloseReason) {
		// Save position (skip if minimized to avoid storing -32000,-32000)
		if configStore != nil && !win.IsIconic(mainWindowInstance.Handle()) {
			cfg, err := configStore.Load()
			if err == nil {
				b := mainWindowInstance.Bounds()
				cfg.MainWindowPos = model.WindowPos{X: b.X, Y: b.Y, Width: b.Width, Height: b.Height}
				configStore.Save(cfg)
			}
		}
	})

	return mainWindowInstance, wc, nil
}

func (wc *MainWindowController) resizeTableColumns() {
	if wc.tableView == nil {
		return
	}
	totalWidth := wc.tableView.Width()
	if totalWidth <= 0 {
		return
	}
	hotkeyWidth := 75
	modeWidth := 50
	nameWidth := totalWidth - hotkeyWidth - modeWidth - 4
	if nameWidth < 80 {
		nameWidth = 80
	}
	cols := wc.tableView.Columns()
	if cols.Len() >= 3 {
		cols.At(0).SetWidth(nameWidth)
		cols.At(1).SetWidth(hotkeyWidth)
		cols.At(2).SetWidth(modeWidth)
	}
}

func (wc *MainWindowController) notifyScriptsChanged() {
	if wc.OnScriptsChanged != nil {
		wc.OnScriptsChanged()
	}
}

// refreshScriptList reloads scripts from disk and updates the table.
func (wc *MainWindowController) refreshScriptList() {
	allScripts, err := wc.scriptStore.List()
	if err != nil {
		return
	}
	wc.allScripts = allScripts

	sort.Slice(wc.allScripts, func(i, j int) bool {
		return strings.ToLower(wc.allScripts[i].Name) < strings.ToLower(wc.allScripts[j].Name)
	})

	rows := make([]scriptRow, len(wc.allScripts))
	checked := map[int]bool{}
	for i, script := range wc.allScripts {
		rows[i] = scriptRow{
			name:    script.Name,
			hotkey:  formatScriptHotkeys(script),
			runMode: script.RunMode,
		}
		if script.Enabled {
			checked[i] = true
		}
	}
	wc.tableModel.items = rows
	wc.tableModel.checked = checked
	wc.tableModel.PublishRowsReset()
}

// formatScriptHotkeys returns a combined hotkey display string for the table.
func formatScriptHotkeys(script model.Script) string {
	switch script.HotkeyMode {
	case model.HotkeyModeToggle:
		if script.ToggleHotkey != nil {
			return formatHotkey(script.ToggleHotkey)
		}
	case model.HotkeyModeSeparate:
		start, stop := "", ""
		if script.StartHotkey != nil {
			start = formatHotkey(script.StartHotkey)
		}
		if script.StopHotkey != nil {
			stop = formatHotkey(script.StopHotkey)
		}
		if start != "" && stop != "" {
			return start + " / " + stop
		}
		return start + stop
	}
	return ""
}

func formatHotkey(hk *model.Hotkey) string {
	result := ""
	for _, mod := range hk.Modifiers {
		result += mod + "+"
	}
	return result + hk.Key
}

func (wc *MainWindowController) onNewScript() {
	newScript := model.Script{
		ID:         uuid.New().String(),
		Name:       "New Script",
		Enabled:    true,
		RunMode:    model.RunModeOnce,
		HotkeyMode: model.HotkeyModeToggle,
		Actions:    []model.Action{},
	}

	if wc.scriptRunner != nil {
		wc.scriptRunner.SetEditDialogOpen(true)
	}
	wc.mainWindow.Hide()
	saved := ShowScriptEditDialog(wc.mainWindow, &newScript, wc.scriptStore, wc.configStore, wc.captureVK, wc.allScripts)
	wc.mainWindow.Show()
	if wc.scriptRunner != nil {
		wc.scriptRunner.SetEditDialogOpen(false)
	}
	if saved {
		wc.refreshScriptList()
		wc.notifyScriptsChanged()
		idx := len(wc.allScripts) - 1
		if idx >= 0 {
			wc.mainWindow.Synchronize(func() {
				wc.tableView.SetCurrentIndex(idx)
			})
		}
	}
}

func (wc *MainWindowController) onEditScript() {
	selectedIndex := wc.tableView.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(wc.allScripts) {
		return
	}
	scriptCopy := wc.allScripts[selectedIndex]

	if wc.scriptRunner != nil {
		wc.scriptRunner.SetEditDialogOpen(true)
	}
	wc.mainWindow.Hide()
	saved := ShowScriptEditDialog(wc.mainWindow, &scriptCopy, wc.scriptStore, wc.configStore, wc.captureVK, wc.allScripts)
	wc.mainWindow.Show()
	if wc.scriptRunner != nil {
		wc.scriptRunner.SetEditDialogOpen(false)
	}
	if saved {
		wc.refreshScriptList()
		wc.notifyScriptsChanged()
		idx := selectedIndex
		if idx >= len(wc.allScripts) {
			idx = len(wc.allScripts) - 1
		}
		if idx >= 0 {
			wc.mainWindow.Synchronize(func() {
				wc.tableView.SetCurrentIndex(idx)
			})
		}
	}
}

func (wc *MainWindowController) onDeleteScript() {
	selectedIndex := wc.tableView.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(wc.allScripts) {
		return
	}

	scriptToDelete := wc.allScripts[selectedIndex]
	confirmResult := walk.MsgBox(
		wc.mainWindow,
		"Delete Script",
		fmt.Sprintf("Delete script '%s'?", scriptToDelete.Name),
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion,
	)
	if confirmResult != walk.DlgCmdYes {
		return
	}

	wc.scriptStore.Delete(scriptToDelete.ID)
	wc.refreshScriptList()
	wc.notifyScriptsChanged()

	// Select nearest item
	count := len(wc.allScripts)
	if count > 0 {
		if selectedIndex >= count {
			selectedIndex = count - 1
		}
		wc.tableView.SetCurrentIndex(selectedIndex)
	}
	wc.tableView.SetFocus()
}
