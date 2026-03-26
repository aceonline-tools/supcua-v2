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

// ScriptEditor provides the form-based script editing UI.
type ScriptEditor struct {
	container           *walk.Composite
	scriptStore         *storage.ScriptStore
	currentScript       *model.Script
	refreshListCallback func()

	// Form fields
	nameEdit           *walk.LineEdit
	enabledCheckbox    *walk.CheckBox
	runModeComboBox    *walk.ComboBox
	hotkeyModeComboBox *walk.ComboBox
	actionListBox      *walk.ListBox
}

// NewScriptEditor creates the form editor inside the given parent container.
func NewScriptEditor(parentContainer *walk.Composite, scriptStore *storage.ScriptStore, refreshListCallback func()) (*ScriptEditor, error) {
	scriptEditor := &ScriptEditor{
		scriptStore:         scriptStore,
		refreshListCallback: refreshListCallback,
	}

	var editorComposite *walk.Composite

	builder := NewBuilder(parentContainer)

	err := (Composite{
		AssignTo: &editorComposite,
		Layout:   VBox{},
		Children: []Widget{
			GroupBox{
				Title:  "Script Properties",
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "Name:"},
					LineEdit{AssignTo: &scriptEditor.nameEdit},

					Label{Text: "Enabled:"},
					CheckBox{AssignTo: &scriptEditor.enabledCheckbox},

					Label{Text: "Run Mode:"},
					ComboBox{
						AssignTo: &scriptEditor.runModeComboBox,
						Model:    []string{"once", "infinite"},
						Editable: false,
					},

					Label{Text: "Hotkey Mode:"},
					ComboBox{
						AssignTo: &scriptEditor.hotkeyModeComboBox,
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
						AssignTo: &scriptEditor.actionListBox,
						MinSize:  Size{Height: 200},
					},
					Composite{
						Layout: HBox{},
						Children: []Widget{
							PushButton{
								Text:      "Add Action",
								OnClicked: func() { scriptEditor.onAddAction() },
							},
							PushButton{
								Text:      "Edit Action",
								OnClicked: func() { scriptEditor.onEditAction() },
							},
							PushButton{
								Text:      "Delete Action",
								OnClicked: func() { scriptEditor.onDeleteAction() },
							},
							PushButton{
								Text:      "Move Up",
								OnClicked: func() { scriptEditor.onMoveActionUp() },
							},
							PushButton{
								Text:      "Move Down",
								OnClicked: func() { scriptEditor.onMoveActionDown() },
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
						Text:      "Save",
						OnClicked: func() { scriptEditor.onSave() },
					},
				},
			},
		},
	}).Create(builder)

	if err != nil {
		return nil, err
	}

	scriptEditor.container = editorComposite
	return scriptEditor, nil
}

// LoadScript populates the editor with the given script's data.
func (scriptEditor *ScriptEditor) LoadScript(script *model.Script) {
	scriptEditor.currentScript = script
	scriptEditor.nameEdit.SetText(script.Name)
	scriptEditor.enabledCheckbox.SetChecked(script.Enabled)

	runModeIndex := 0
	if script.RunMode == model.RunModeInfinite {
		runModeIndex = 1
	}
	scriptEditor.runModeComboBox.SetCurrentIndex(runModeIndex)

	hotkeyModeIndex := 0
	if script.HotkeyMode == model.HotkeyModeSeparate {
		hotkeyModeIndex = 1
	}
	scriptEditor.hotkeyModeComboBox.SetCurrentIndex(hotkeyModeIndex)

	scriptEditor.refreshActionList()
}

// NewEmptyScript creates a new script with defaults and loads it.
func (scriptEditor *ScriptEditor) NewEmptyScript() {
	newScript := &model.Script{
		ID:         uuid.New().String(),
		Name:       "New Script",
		Enabled:    false,
		RunMode:    model.RunModeOnce,
		HotkeyMode: model.HotkeyModeToggle,
		Actions:    []model.Action{},
	}
	scriptEditor.LoadScript(newScript)
}

func (scriptEditor *ScriptEditor) refreshActionList() {
	if scriptEditor.currentScript == nil {
		return
	}
	actionDescriptions := make([]string, len(scriptEditor.currentScript.Actions))
	for index, action := range scriptEditor.currentScript.Actions {
		actionDescriptions[index] = describeAction(action)
	}
	scriptEditor.actionListBox.SetModel(actionDescriptions)
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

func (scriptEditor *ScriptEditor) onSave() {
	if scriptEditor.currentScript == nil {
		return
	}

	scriptEditor.currentScript.Name = scriptEditor.nameEdit.Text()
	scriptEditor.currentScript.Enabled = scriptEditor.enabledCheckbox.Checked()

	if scriptEditor.runModeComboBox.CurrentIndex() == 0 {
		scriptEditor.currentScript.RunMode = model.RunModeOnce
	} else {
		scriptEditor.currentScript.RunMode = model.RunModeInfinite
	}

	if scriptEditor.hotkeyModeComboBox.CurrentIndex() == 0 {
		scriptEditor.currentScript.HotkeyMode = model.HotkeyModeToggle
	} else {
		scriptEditor.currentScript.HotkeyMode = model.HotkeyModeSeparate
	}

	scriptEditor.scriptStore.Save(*scriptEditor.currentScript)
	if scriptEditor.refreshListCallback != nil {
		scriptEditor.refreshListCallback()
	}
}

func (scriptEditor *ScriptEditor) onAddAction()  { /* Dialog in later task */ }
func (scriptEditor *ScriptEditor) onEditAction()  { /* Dialog in later task */ }
func (scriptEditor *ScriptEditor) onDeleteAction() {
	if scriptEditor.currentScript == nil {
		return
	}
	selectedIndex := scriptEditor.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(scriptEditor.currentScript.Actions) {
		return
	}
	scriptEditor.currentScript.Actions = append(
		scriptEditor.currentScript.Actions[:selectedIndex],
		scriptEditor.currentScript.Actions[selectedIndex+1:]...,
	)
	scriptEditor.refreshActionList()
}

func (scriptEditor *ScriptEditor) onMoveActionUp() {
	if scriptEditor.currentScript == nil {
		return
	}
	selectedIndex := scriptEditor.actionListBox.CurrentIndex()
	if selectedIndex <= 0 {
		return
	}
	actions := scriptEditor.currentScript.Actions
	actions[selectedIndex], actions[selectedIndex-1] = actions[selectedIndex-1], actions[selectedIndex]
	scriptEditor.refreshActionList()
	scriptEditor.actionListBox.SetCurrentIndex(selectedIndex - 1)
}

func (scriptEditor *ScriptEditor) onMoveActionDown() {
	if scriptEditor.currentScript == nil {
		return
	}
	selectedIndex := scriptEditor.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(scriptEditor.currentScript.Actions)-1 {
		return
	}
	actions := scriptEditor.currentScript.Actions
	actions[selectedIndex], actions[selectedIndex+1] = actions[selectedIndex+1], actions[selectedIndex]
	scriptEditor.refreshActionList()
	scriptEditor.actionListBox.SetCurrentIndex(selectedIndex + 1)
}
