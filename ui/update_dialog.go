//go:build windows

package ui

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// ShowUpdateDialog asks the user whether to update. Returns true if user accepts.
// Must be called from the UI thread.
func ShowUpdateDialog(owner walk.Form, latestTag string) bool {
	accepted := false
	var dlg *walk.Dialog

	Dialog{
		AssignTo: &dlg,
		Title:    "Update Available",
		MinSize:  Size{Width: 280, Height: 80},
		Layout:   HBox{Margins: Margins{Left: 8, Right: 8, Top: 8, Bottom: 8}, Spacing: 6},
		Children: []Widget{
			Label{Text: "New version " + latestTag + " available.", MinSize: Size{Width: 180}},
			PushButton{
				Text: "Update",
				OnClicked: func() {
					accepted = true
					dlg.Accept()
				},
			},
			PushButton{
				Text: "Skip",
				OnClicked: func() {
					dlg.Cancel()
				},
			},
		},
	}.Run(owner)

	return accepted
}
