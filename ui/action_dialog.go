//go:build windows

package ui

import (
	"strconv"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/model"
)

// ShowActionParamDialog shows a dialog with only the relevant parameters for the action type.
// The action's Type is already set — this dialog only edits parameters.
func ShowActionParamDialog(ownerWindow walk.Form, existingAction *model.Action) (model.Action, bool) {
	var dialogWindow *walk.Dialog
	var acceptedResult bool

	resultAction := *existingAction

	dialogTitle := "Edit: " + existingAction.Type
	var formWidgets []Widget

	switch existingAction.Type {
	case model.ActionMouseMove:
		var xEdit, yEdit *walk.LineEdit
		formWidgets = []Widget{
			Label{Text: "X:"},
			LineEdit{AssignTo: &xEdit, Text: strconv.Itoa(existingAction.X)},
			Label{Text: "Y:"},
			LineEdit{AssignTo: &yEdit, Text: strconv.Itoa(existingAction.Y)},
		}
		defer func() {
			if acceptedResult && xEdit != nil {
				resultAction.X, _ = strconv.Atoi(xEdit.Text())
				resultAction.Y, _ = strconv.Atoi(yEdit.Text())
			}
		}()

	case model.ActionKeyDown, model.ActionKeyUp:
		var keyEdit *walk.LineEdit
		formWidgets = []Widget{
			Label{Text: "Key:"},
			LineEdit{AssignTo: &keyEdit, Text: existingAction.Key},
		}
		defer func() {
			if acceptedResult && keyEdit != nil {
				resultAction.Key = keyEdit.Text()
			}
		}()

	case model.ActionDelay:
		var delayEdit *walk.LineEdit
		formWidgets = []Widget{
			Label{Text: "Delay (ms):"},
			LineEdit{AssignTo: &delayEdit, Text: strconv.Itoa(existingAction.DelayMs)},
		}
		defer func() {
			if acceptedResult && delayEdit != nil {
				resultAction.DelayMs, _ = strconv.Atoi(delayEdit.Text())
			}
		}()

	case model.ActionLoop:
		var countEdit *walk.LineEdit
		formWidgets = []Widget{
			Label{Text: "Count (0=infinite):"},
			LineEdit{AssignTo: &countEdit, Text: strconv.Itoa(existingAction.LoopCount)},
		}
		defer func() {
			if acceptedResult && countEdit != nil {
				resultAction.LoopCount, _ = strconv.Atoi(countEdit.Text())
			}
		}()

	case model.ActionWaitForImage:
		var timeoutEdit *walk.LineEdit
		var useFoundCheckbox *walk.CheckBox
		formWidgets = []Widget{
			Label{Text: "Timeout (ms):"},
			LineEdit{AssignTo: &timeoutEdit, Text: strconv.Itoa(existingAction.TimeoutMs)},
			Label{Text: "Use found position:"},
			CheckBox{AssignTo: &useFoundCheckbox, Checked: existingAction.UseFoundPosition},
		}
		defer func() {
			if acceptedResult && timeoutEdit != nil {
				resultAction.TimeoutMs, _ = strconv.Atoi(timeoutEdit.Text())
				resultAction.UseFoundPosition = useFoundCheckbox.Checked()
			}
		}()

	case model.ActionIfImage:
		var useFoundCheckbox *walk.CheckBox
		formWidgets = []Widget{
			Label{Text: "Use found position:"},
			CheckBox{AssignTo: &useFoundCheckbox, Checked: existingAction.UseFoundPosition},
		}
		defer func() {
			if acceptedResult && useFoundCheckbox != nil {
				resultAction.UseFoundPosition = useFoundCheckbox.Checked()
			}
		}()

	default:
		return resultAction, true
	}

	dialogDefinition := Dialog{
		AssignTo: &dialogWindow,
		Title:    dialogTitle,
		MinSize:  Size{Width: 300, Height: 150},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout:   Grid{Columns: 2},
				Children: formWidgets,
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

	return resultAction, true
}
