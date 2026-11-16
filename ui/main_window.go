//go:build windows

package ui

import (
	"fmt"
	"sort"
	"strings"
	"syscall"

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

	// Called after scripts change; receives the current in-memory script list
	OnScriptsChanged func([]model.Script)

	// State
	allScripts  []model.Script
	rowToScript map[int]int // row index → allScripts index (excludes header rows)
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
	items    []scriptRow
	checked  map[int]bool
	wc       *MainWindowController
	boldFont *walk.Font
}

func (m *scriptTableModel) StyleCell(style *walk.CellStyle) {
	if style.Row() >= 0 && style.Row() < len(m.items) && m.items[style.Row()].isHeader {
		style.Font = m.boldFont
	}
}

type scriptRow struct {
	name       string
	hotkey     string
	runMode    string
	isHeader   bool
	group      string // set for header rows to identify the group
	treePrefix string // "├── " or "└── " for grouped scripts
}

func (m *scriptTableModel) RowCount() int { return len(m.items) }

func (m *scriptTableModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.items) {
		return ""
	}
	item := m.items[row]
	if item.isHeader {
		if col == 0 {
			return item.name
		}
		return ""
	}
	switch col {
	case 0:
		return item.treePrefix + item.name
	case 1:
		return item.hotkey
	case 2:
		return item.runMode
	}
	return ""
}

func (m *scriptTableModel) Checked(index int) bool {
	if index < 0 || index >= len(m.items) {
		return false
	}
	item := m.items[index]
	if item.isHeader {
		// Checked if all scripts in the group are enabled
		hasAny := false
		for _, script := range m.wc.allScripts {
			if script.Group == item.group {
				hasAny = true
				if !script.Enabled {
					return false
				}
			}
		}
		return hasAny
	}
	return m.checked[index]
}

func (m *scriptTableModel) SetChecked(index int, checked bool) error {
	if index < 0 || index >= len(m.items) {
		return nil
	}
	item := m.items[index]
	if item.isHeader {
		// Update in-memory state and queue the group's saves on the store's single worker
		var changed []model.Script
		for i, script := range m.wc.allScripts {
			if script.Group == item.group {
				script.Enabled = checked
				m.wc.allScripts[i] = script
				changed = append(changed, script)
			}
		}
		m.wc.scriptStore.QueueSave(changed...)
		// Update UI immediately
		for row, scriptIdx := range m.wc.rowToScript {
			if m.wc.allScripts[scriptIdx].Group == item.group {
				m.checked[row] = checked
				m.PublishRowChanged(row)
			}
		}
		m.wc.notifyScriptsChanged()
		return nil
	}
	m.checked[index] = checked
	if m.wc != nil {
		scriptIdx, ok := m.wc.rowToScript[index]
		if !ok {
			return nil
		}
		script := m.wc.allScripts[scriptIdx]
		script.Enabled = checked
		m.wc.allScripts[scriptIdx] = script
		m.wc.scriptStore.QueueSave(script)
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
						Text:      "Clone",
						OnClicked: func() { wc.onCloneScript() },
					},
					PushButton{
						Text:      "Delete",
						OnClicked: func() { wc.onDeleteScript() },
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

	if f := wc.tableView.Font(); f != nil {
		if bf, err := walk.NewFont(f.Family(), f.PointSize(), walk.FontBold); err == nil {
			wc.tableModel.boldFont = bf
		}
	}

	wc.refreshScriptList()

	// Auto-select first script row (skip header rows)
	for i, row := range wc.tableModel.items {
		if !row.isHeader {
			wc.tableView.SetCurrentIndex(i)
			break
		}
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

	// Minimize button (—) hides to tray instead of minimizing
	var origProc uintptr
	hideCb := syscall.NewCallback(func(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
		if msg == win.WM_SYSCOMMAND && (wParam&0xFFF0) == win.SC_MINIMIZE {
			mainWindowInstance.SetVisible(false)
			return 0
		}
		return win.CallWindowProc(origProc, hwnd, msg, wParam, lParam)
	})
	origProc = win.SetWindowLongPtr(mainWindowInstance.Handle(), win.GWL_WNDPROC, hideCb)

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
		wc.OnScriptsChanged(wc.allScripts)
	}
}

// Scripts returns the current in-memory script list.
func (wc *MainWindowController) Scripts() []model.Script {
	return wc.allScripts
}

// refreshScriptList reloads scripts from disk and updates the table.
func (wc *MainWindowController) refreshScriptList() {
	allScripts, err := wc.scriptStore.List()
	if err != nil {
		return
	}
	wc.allScripts = allScripts

	sort.Slice(wc.allScripts, func(i, j int) bool {
		gi := strings.ToLower(wc.allScripts[i].Group)
		gj := strings.ToLower(wc.allScripts[j].Group)
		if gi == "" && gj != "" {
			return true
		}
		if gi != "" && gj == "" {
			return false
		}
		if gi != gj {
			return gi < gj
		}
		return strings.ToLower(wc.allScripts[i].Name) < strings.ToLower(wc.allScripts[j].Name)
	})

	// Build rows with group header rows inserted before each group.
	// Ungrouped scripts have no header row.
	var rows []scriptRow
	rowToScript := map[int]int{}
	checked := map[int]bool{}
	prevGroup := ""

	for scriptIdx, script := range wc.allScripts {
		g := script.Group
		if g != "" && g != prevGroup {
			rows = append(rows, scriptRow{name: g, group: g, isHeader: true})
		}
		prevGroup = g

		// Determine tree prefix for grouped scripts
		treePrefix := ""
		if g != "" {
			isLast := true
			for j := scriptIdx + 1; j < len(wc.allScripts); j++ {
				if wc.allScripts[j].Group == g {
					isLast = false
					break
				}
			}
			if isLast {
				treePrefix = "└─ "
			} else {
				treePrefix = "├─ "
			}
		}

		rowIdx := len(rows)
		rowToScript[rowIdx] = scriptIdx
		rows = append(rows, scriptRow{
			name:       script.Name,
			hotkey:     formatScriptHotkeys(script),
			runMode:    script.RunMode,
			treePrefix: treePrefix,
		})
		if script.Enabled {
			checked[rowIdx] = true
		}
	}

	wc.rowToScript = rowToScript
	wc.tableModel.items = rows
	wc.tableModel.checked = checked
	wc.tableModel.PublishRowsReset()
}

// findRowByScriptID returns the row index for a given script ID, or -1 if not found.
func (wc *MainWindowController) findRowByScriptID(id string) int {
	for row, scriptIdx := range wc.rowToScript {
		if wc.allScripts[scriptIdx].ID == id {
			return row
		}
	}
	return -1
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
	newID := newScript.ID

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
		if row := wc.findRowByScriptID(newID); row >= 0 {
			wc.mainWindow.Synchronize(func() {
				wc.tableView.SetCurrentIndex(row)
			})
		}
	}
}

func (wc *MainWindowController) onEditScript() {
	selectedIndex := wc.tableView.CurrentIndex()
	scriptIdx, ok := wc.rowToScript[selectedIndex]
	if !ok {
		return // header row or nothing selected
	}
	scriptCopy := wc.allScripts[scriptIdx]
	scriptID := scriptCopy.ID

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
		if row := wc.findRowByScriptID(scriptID); row >= 0 {
			wc.mainWindow.Synchronize(func() {
				wc.tableView.SetCurrentIndex(row)
			})
		}
	}
}

func (wc *MainWindowController) onCloneScript() {
	selectedIndex := wc.tableView.CurrentIndex()
	scriptIdx, ok := wc.rowToScript[selectedIndex]
	if !ok {
		return // header row or nothing selected
	}

	clonedScript := model.CloneScript(wc.allScripts[scriptIdx], wc.scriptStore.NameOrFileTaken)
	if err := wc.scriptStore.Save(clonedScript); err != nil {
		walk.MsgBox(wc.mainWindow, "Clone Script", fmt.Sprintf("Failed to clone script: %v", err), walk.MsgBoxIconError)
		return
	}
	wc.refreshScriptList()
	wc.notifyScriptsChanged()
	if row := wc.findRowByScriptID(clonedScript.ID); row >= 0 {
		wc.mainWindow.Synchronize(func() {
			wc.tableView.SetCurrentIndex(row)
		})
	}
}

func (wc *MainWindowController) onDeleteScript() {
	selectedIndex := wc.tableView.CurrentIndex()
	scriptIdx, ok := wc.rowToScript[selectedIndex]
	if !ok {
		return // header row or nothing selected
	}

	scriptToDelete := wc.allScripts[scriptIdx]
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

	// Select nearest script row at or after the deleted position
	totalRows := len(wc.tableModel.items)
	for i := selectedIndex; i < totalRows; i++ {
		if _, ok := wc.rowToScript[i]; ok {
			wc.tableView.SetCurrentIndex(i)
			wc.tableView.SetFocus()
			return
		}
	}
	for i := selectedIndex - 1; i >= 0; i-- {
		if _, ok := wc.rowToScript[i]; ok {
			wc.tableView.SetCurrentIndex(i)
			break
		}
	}
	wc.tableView.SetFocus()
}
