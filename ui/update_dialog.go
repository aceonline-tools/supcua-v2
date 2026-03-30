//go:build windows

package ui

import (
	. "github.com/lxn/walk/declarative"
)

// ShowUpdateDialog asks the user whether to update. Returns true if user accepts.
func ShowUpdateDialog(latestTag string) bool {
	accepted := false

	Dialog{
		Title:   "Update Available",
		MinSize: Size{Width: 280, Height: 80},
		Layout:  HBox{Margins: Margins{Left: 8, Right: 8, Top: 8, Bottom: 8}, Spacing: 6},
		Children: []Widget{
			Label{Text: "New version " + latestTag + " available.", MinSize: Size{Width: 180}},
			PushButton{
				Text: "Update",
				OnClicked: func() {
					accepted = true
				},
			},
			PushButton{Text: "Skip"},
		},
	}.Run(nil)

	return accepted
}
