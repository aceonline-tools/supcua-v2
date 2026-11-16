//go:build windows

package ui

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// ShowAutoActiveProcessDialog edits the game process name. Returns the entered
// name and true if the user accepted. Must be called from the UI thread.
func ShowAutoActiveProcessDialog(owner walk.Form, currentName string) (string, bool) {
	var dlg *walk.Dialog
	var nameEdit *walk.LineEdit
	accepted := false

	Dialog{
		AssignTo: &dlg,
		Title:    "Auto Active Process",
		MinSize:  Size{Width: 320, Height: 90},
		Layout:   VBox{Margins: Margins{Left: 8, Right: 8, Top: 8, Bottom: 8}, Spacing: 6},
		Children: []Widget{
			Label{Text: "Game process name (e.g. ACEonline.exe):"},
			LineEdit{AssignTo: &nameEdit, Text: currentName},
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text: "OK",
						OnClicked: func() {
							accepted = true
							dlg.Accept()
						},
					},
					PushButton{
						Text:      "Cancel",
						OnClicked: func() { dlg.Cancel() },
					},
				},
			},
		},
	}.Run(owner)

	if !accepted {
		return "", false
	}
	return nameEdit.Text(), true
}
