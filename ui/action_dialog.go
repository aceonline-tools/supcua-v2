//go:build windows

package ui

import (
	"strconv"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/model"
)

// ShowActionDialog shows a dialog to create or edit an action.
// Returns the action and true if OK was clicked, or zero-value and false if cancelled.
func ShowActionDialog(ownerWindow walk.Form, existingAction *model.Action) (model.Action, bool) {
	var dialogWindow *walk.Dialog
	var actionTypeComboBox *walk.ComboBox
	var parameterXEdit *walk.LineEdit
	var parameterYEdit *walk.LineEdit
	var keyNameEdit *walk.LineEdit
	var delayMsEdit *walk.LineEdit
	var loopCountEdit *walk.LineEdit
	var acceptedResult bool

	actionTypeNames := []string{
		"mouse_move", "mouse_left_down", "mouse_left_up",
		"mouse_right_down", "mouse_right_up",
		"key_down", "key_up", "delay", "loop",
		"wait_for_image", "if_image",
	}

	initialType := 0
	initialX := ""
	initialY := ""
	initialKey := ""
	initialDelay := ""
	initialLoopCount := ""

	if existingAction != nil {
		for typeIndex, typeName := range actionTypeNames {
			if typeName == existingAction.Type {
				initialType = typeIndex
				break
			}
		}
		initialX = strconv.Itoa(existingAction.X)
		initialY = strconv.Itoa(existingAction.Y)
		initialKey = existingAction.Key
		initialDelay = strconv.Itoa(existingAction.DelayMs)
		initialLoopCount = strconv.Itoa(existingAction.LoopCount)
	}

	dialogDefinition := Dialog{
		AssignTo: &dialogWindow,
		Title:    "Action Editor",
		MinSize:  Size{Width: 350, Height: 250},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "Type:"},
					ComboBox{
						AssignTo:     &actionTypeComboBox,
						Model:        actionTypeNames,
						CurrentIndex: initialType,
						Editable:     false,
					},
					Label{Text: "X:"},
					LineEdit{AssignTo: &parameterXEdit, Text: initialX},
					Label{Text: "Y:"},
					LineEdit{AssignTo: &parameterYEdit, Text: initialY},
					Label{Text: "Key:"},
					LineEdit{AssignTo: &keyNameEdit, Text: initialKey},
					Label{Text: "Delay (ms):"},
					LineEdit{AssignTo: &delayMsEdit, Text: initialDelay},
					Label{Text: "Loop Count (0=inf):"},
					LineEdit{AssignTo: &loopCountEdit, Text: initialLoopCount},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text: "OK",
						OnClicked: func() {
							acceptedResult = true
							dialogWindow.Accept()
						},
					},
					PushButton{
						Text: "Cancel",
						OnClicked: func() {
							dialogWindow.Cancel()
						},
					},
				},
			},
		},
	}

	dialogDefinition.Run(ownerWindow)

	if !acceptedResult {
		return model.Action{}, false
	}

	selectedType := actionTypeNames[actionTypeComboBox.CurrentIndex()]
	parsedX, _ := strconv.Atoi(parameterXEdit.Text())
	parsedY, _ := strconv.Atoi(parameterYEdit.Text())
	parsedDelay, _ := strconv.Atoi(delayMsEdit.Text())
	parsedLoopCount, _ := strconv.Atoi(loopCountEdit.Text())

	resultAction := model.Action{
		Type:      selectedType,
		X:         parsedX,
		Y:         parsedY,
		Key:       keyNameEdit.Text(),
		DelayMs:   parsedDelay,
		LoopCount: parsedLoopCount,
	}

	// Preserve nested actions from existing action if editing a loop/if_image
	if existingAction != nil {
		resultAction.Actions = existingAction.Actions
		resultAction.ThenActions = existingAction.ThenActions
		resultAction.ElseActions = existingAction.ElseActions
		resultAction.Image = existingAction.Image
		resultAction.SearchRegion = existingAction.SearchRegion
		resultAction.UseFoundPosition = existingAction.UseFoundPosition
		resultAction.TimeoutMs = existingAction.TimeoutMs
	}

	return resultAction, true
}
