//go:build windows

package ui

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
)

// MainWindowController manages the main application window and script editing.
type MainWindowController struct {
	mainWindow    *walk.MainWindow
	scriptStore   *storage.ScriptStore
	scriptListBox *walk.ListBox

	// Editor fields
	nameEdit           *walk.LineEdit
	enabledCheckbox    *walk.CheckBox
	runModeComboBox    *walk.ComboBox
	hotkeyModeComboBox *walk.ComboBox
	actionListBox      *walk.ListBox

	// State
	allScripts    []model.Script
	currentScript *model.Script
}

// CreateMainWindow builds and returns the main application window.
func CreateMainWindow(scriptStore *storage.ScriptStore) (*walk.MainWindow, *MainWindowController, error) {
	windowController := &MainWindowController{
		scriptStore: scriptStore,
	}

	var mainWindowInstance *walk.MainWindow

	err := (MainWindow{
		AssignTo: &mainWindowInstance,
		Title:    "supcua-v2 - Script Automation",
		MinSize:  Size{Width: 800, Height: 500},
		Size:     Size{Width: 1000, Height: 600},
		Layout:   HBox{},
		Visible:  true,
		Children: []Widget{
			// Left sidebar: script list + buttons
			Composite{
				MinSize: Size{Width: 220},
				MaxSize: Size{Width: 250},
				Layout:  VBox{},
				Children: []Widget{
					Label{Text: "Scripts"},
					ListBox{
						AssignTo: &windowController.scriptListBox,
						OnCurrentIndexChanged: func() {
							windowController.onScriptSelected()
						},
					},
					Composite{
						Layout: HBox{},
						Children: []Widget{
							PushButton{
								Text:      "New",
								OnClicked: func() { windowController.onNewScript() },
							},
							PushButton{
								Text:      "Delete",
								OnClicked: func() { windowController.onDeleteScript() },
							},
						},
					},
				},
			},
			// Right side: script editor
			Composite{
				Layout: VBox{},
				Children: []Widget{
					GroupBox{
						Title:  "Script Properties",
						Layout: Grid{Columns: 2},
						Children: []Widget{
							Label{Text: "Name:"},
							LineEdit{AssignTo: &windowController.nameEdit},

							Label{Text: "Enabled:"},
							CheckBox{AssignTo: &windowController.enabledCheckbox},

							Label{Text: "Run Mode:"},
							ComboBox{
								AssignTo: &windowController.runModeComboBox,
								Model:    []string{"once", "infinite"},
								Editable: false,
							},

							Label{Text: "Hotkey Mode:"},
							ComboBox{
								AssignTo: &windowController.hotkeyModeComboBox,
								Model:    []string{"toggle", "separate"},
								Editable: false,
							},
						},
					},
					GroupBox{
						Title:  "Actions",
						Layout: VBox{},
						Children: []Widget{
							ListBox{
								AssignTo: &windowController.actionListBox,
								MinSize:  Size{Height: 200},
							},
							// Add action buttons - one per type
							GroupBox{
								Title:  "Add Action",
								Layout: Grid{Columns: 5},
								Children: []Widget{
									PushButton{
										Text:      "Mouse Move",
										OnClicked: func() { windowController.addActionOfType(model.ActionMouseMove) },
									},
									PushButton{
										Text:      "Left Down",
										OnClicked: func() { windowController.addSimpleAction(model.ActionMouseLeftDown) },
									},
									PushButton{
										Text:      "Left Up",
										OnClicked: func() { windowController.addSimpleAction(model.ActionMouseLeftUp) },
									},
									PushButton{
										Text:      "Right Down",
										OnClicked: func() { windowController.addSimpleAction(model.ActionMouseRightDown) },
									},
									PushButton{
										Text:      "Right Up",
										OnClicked: func() { windowController.addSimpleAction(model.ActionMouseRightUp) },
									},
									PushButton{
										Text:      "Key Down",
										OnClicked: func() { windowController.addActionOfType(model.ActionKeyDown) },
									},
									PushButton{
										Text:      "Key Up",
										OnClicked: func() { windowController.addActionOfType(model.ActionKeyUp) },
									},
									PushButton{
										Text:      "Delay",
										OnClicked: func() { windowController.addActionOfType(model.ActionDelay) },
									},
									PushButton{
										Text:      "Loop",
										OnClicked: func() { windowController.addActionOfType(model.ActionLoop) },
									},
									PushButton{
										Text:      "Wait Image",
										OnClicked: func() { windowController.addActionOfType(model.ActionWaitForImage) },
									},
									PushButton{
										Text:      "If Image",
										OnClicked: func() { windowController.addActionOfType(model.ActionIfImage) },
									},
								},
							},
							Composite{
								Layout: HBox{},
								Children: []Widget{
									PushButton{
										Text:      "Edit",
										OnClicked: func() { windowController.onEditAction() },
									},
									PushButton{
										Text:      "Delete",
										OnClicked: func() { windowController.onDeleteAction() },
									},
									PushButton{
										Text:      "Up",
										OnClicked: func() { windowController.onMoveActionUp() },
									},
									PushButton{
										Text:      "Down",
										OnClicked: func() { windowController.onMoveActionDown() },
									},
								},
							},
						},
					},
					Composite{
						Layout: HBox{},
						Children: []Widget{
							HSpacer{},
							PushButton{
								Text:      "Save Script",
								OnClicked: func() { windowController.onSave() },
							},
						},
					},
				},
			},
		},
	}).Create()

	if err != nil {
		return nil, nil, err
	}

	windowController.mainWindow = mainWindowInstance
	windowController.refreshScriptList()

	return mainWindowInstance, windowController, nil
}

// refreshScriptList reloads scripts from disk and updates the list.
func (windowController *MainWindowController) refreshScriptList() {
	allScripts, err := windowController.scriptStore.List()
	if err != nil {
		return
	}
	windowController.allScripts = allScripts

	scriptNames := make([]string, len(allScripts))
	for index, script := range allScripts {
		statusPrefix := "[ ] "
		if script.Enabled {
			statusPrefix = "[x] "
		}
		scriptNames[index] = statusPrefix + script.Name
	}

	windowController.scriptListBox.SetModel(scriptNames)
}

func (windowController *MainWindowController) onScriptSelected() {
	selectedIndex := windowController.scriptListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(windowController.allScripts) {
		return
	}
	script := windowController.allScripts[selectedIndex]
	scriptCopy := script
	windowController.currentScript = &scriptCopy
	windowController.loadScriptIntoEditor()
}

func (windowController *MainWindowController) loadScriptIntoEditor() {
	if windowController.currentScript == nil {
		return
	}
	script := windowController.currentScript

	windowController.nameEdit.SetText(script.Name)
	windowController.enabledCheckbox.SetChecked(script.Enabled)

	runModeIndex := 0
	if script.RunMode == model.RunModeInfinite {
		runModeIndex = 1
	}
	windowController.runModeComboBox.SetCurrentIndex(runModeIndex)

	hotkeyModeIndex := 0
	if script.HotkeyMode == model.HotkeyModeSeparate {
		hotkeyModeIndex = 1
	}
	windowController.hotkeyModeComboBox.SetCurrentIndex(hotkeyModeIndex)

	windowController.refreshActionList()
}

func (windowController *MainWindowController) refreshActionList() {
	if windowController.currentScript == nil {
		windowController.actionListBox.SetModel([]string{})
		return
	}
	actionDescriptions := make([]string, len(windowController.currentScript.Actions))
	for index, action := range windowController.currentScript.Actions {
		actionDescriptions[index] = describeAction(action)
	}
	windowController.actionListBox.SetModel(actionDescriptions)
}

func (windowController *MainWindowController) onNewScript() {
	newScript := model.Script{
		ID:         uuid.New().String(),
		Name:       "New Script",
		Enabled:    false,
		RunMode:    model.RunModeOnce,
		HotkeyMode: model.HotkeyModeToggle,
		Actions:    []model.Action{},
	}
	windowController.currentScript = &newScript
	windowController.loadScriptIntoEditor()
}

func (windowController *MainWindowController) onDeleteScript() {
	selectedIndex := windowController.scriptListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(windowController.allScripts) {
		return
	}

	scriptToDelete := windowController.allScripts[selectedIndex]
	confirmResult := walk.MsgBox(
		windowController.mainWindow,
		"Delete Script",
		fmt.Sprintf("Delete script '%s'?", scriptToDelete.Name),
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion,
	)
	if confirmResult != walk.DlgCmdYes {
		return
	}

	windowController.scriptStore.Delete(scriptToDelete.ID)
	windowController.currentScript = nil
	windowController.nameEdit.SetText("")
	windowController.enabledCheckbox.SetChecked(false)
	windowController.actionListBox.SetModel([]string{})
	windowController.refreshScriptList()
}

func (windowController *MainWindowController) onSave() {
	if windowController.currentScript == nil {
		walk.MsgBox(windowController.mainWindow, "No Script", "Create or select a script first.", walk.MsgBoxIconWarning)
		return
	}

	windowController.currentScript.Name = windowController.nameEdit.Text()
	windowController.currentScript.Enabled = windowController.enabledCheckbox.Checked()

	if windowController.runModeComboBox.CurrentIndex() == 0 {
		windowController.currentScript.RunMode = model.RunModeOnce
	} else {
		windowController.currentScript.RunMode = model.RunModeInfinite
	}

	if windowController.hotkeyModeComboBox.CurrentIndex() == 0 {
		windowController.currentScript.HotkeyMode = model.HotkeyModeToggle
	} else {
		windowController.currentScript.HotkeyMode = model.HotkeyModeSeparate
	}

	err := windowController.scriptStore.Save(*windowController.currentScript)
	if err != nil {
		walk.MsgBox(windowController.mainWindow, "Error", "Failed to save: "+err.Error(), walk.MsgBoxIconError)
		return
	}
	windowController.refreshScriptList()
}

// addSimpleAction adds a parameterless action (mouse down/up) directly without dialog.
func (windowController *MainWindowController) addSimpleAction(actionType string) {
	if windowController.currentScript == nil {
		return
	}
	windowController.currentScript.Actions = append(windowController.currentScript.Actions, model.Action{Type: actionType})
	windowController.refreshActionList()
}

// addActionOfType opens a parameter dialog for the given action type.
func (windowController *MainWindowController) addActionOfType(actionType string) {
	if windowController.currentScript == nil {
		return
	}
	newAction := model.Action{Type: actionType}
	editedAction, accepted := ShowActionParamDialog(windowController.mainWindow, &newAction)
	if !accepted {
		return
	}
	windowController.currentScript.Actions = append(windowController.currentScript.Actions, editedAction)
	windowController.refreshActionList()
}

func (windowController *MainWindowController) onEditAction() {
	if windowController.currentScript == nil {
		return
	}
	selectedIndex := windowController.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(windowController.currentScript.Actions) {
		return
	}
	existingAction := &windowController.currentScript.Actions[selectedIndex]

	// Simple actions with no params — nothing to edit
	switch existingAction.Type {
	case model.ActionMouseLeftDown, model.ActionMouseLeftUp,
		model.ActionMouseRightDown, model.ActionMouseRightUp:
		return
	}

	editedAction, accepted := ShowActionParamDialog(windowController.mainWindow, existingAction)
	if !accepted {
		return
	}
	windowController.currentScript.Actions[selectedIndex] = editedAction
	windowController.refreshActionList()
}

func (windowController *MainWindowController) onDeleteAction() {
	if windowController.currentScript == nil {
		return
	}
	selectedIndex := windowController.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(windowController.currentScript.Actions) {
		return
	}
	windowController.currentScript.Actions = append(
		windowController.currentScript.Actions[:selectedIndex],
		windowController.currentScript.Actions[selectedIndex+1:]...,
	)
	windowController.refreshActionList()
}

func (windowController *MainWindowController) onMoveActionUp() {
	if windowController.currentScript == nil {
		return
	}
	selectedIndex := windowController.actionListBox.CurrentIndex()
	if selectedIndex <= 0 {
		return
	}
	actions := windowController.currentScript.Actions
	actions[selectedIndex], actions[selectedIndex-1] = actions[selectedIndex-1], actions[selectedIndex]
	windowController.refreshActionList()
	windowController.actionListBox.SetCurrentIndex(selectedIndex - 1)
}

func (windowController *MainWindowController) onMoveActionDown() {
	if windowController.currentScript == nil {
		return
	}
	selectedIndex := windowController.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(windowController.currentScript.Actions)-1 {
		return
	}
	actions := windowController.currentScript.Actions
	actions[selectedIndex], actions[selectedIndex+1] = actions[selectedIndex+1], actions[selectedIndex]
	windowController.refreshActionList()
	windowController.actionListBox.SetCurrentIndex(selectedIndex + 1)
}

func describeAction(action model.Action) string {
	switch action.Type {
	case model.ActionMouseMove:
		if action.Source == "found" {
			return "Mouse Move -> found position"
		}
		return fmt.Sprintf("Mouse Move -> (%d, %d)", action.X, action.Y)
	case model.ActionMouseLeftDown:
		return "Mouse Left Down"
	case model.ActionMouseLeftUp:
		return "Mouse Left Up"
	case model.ActionMouseRightDown:
		return "Mouse Right Down"
	case model.ActionMouseRightUp:
		return "Mouse Right Up"
	case model.ActionKeyDown:
		return "Key Down: " + action.Key
	case model.ActionKeyUp:
		return "Key Up: " + action.Key
	case model.ActionDelay:
		return fmt.Sprintf("Delay: %dms", action.DelayMs)
	case model.ActionLoop:
		countText := "infinite"
		if action.LoopCount > 0 {
			countText = fmt.Sprintf("%d times", action.LoopCount)
		}
		return fmt.Sprintf("Loop (%s, %d actions)", countText, len(action.Actions))
	case model.ActionWaitForImage:
		return fmt.Sprintf("Wait For Image (timeout: %dms)", action.TimeoutMs)
	case model.ActionIfImage:
		return fmt.Sprintf("If Image (then: %d, else: %d)", len(action.ThenActions), len(action.ElseActions))
	default:
		return action.Type
	}
}
