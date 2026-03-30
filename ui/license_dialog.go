//go:build windows

package ui

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// ShowNotLicensedDialog shows a compact dialog with MAC address and a Copy button.
func ShowNotLicensedDialog(macs []string) {
	macText := ""
	if len(macs) > 0 {
		macText = macs[0]
	}

	var dlg *walk.Dialog

	Dialog{
		AssignTo: &dlg,
		Title:    "Not Licensed",
		MinSize:  Size{Width: 320, Height: 80},
		Layout:   HBox{Margins: Margins{Left: 8, Right: 8, Top: 8, Bottom: 8}, Spacing: 6},
		Children: []Widget{
			LineEdit{
				Text:     macText,
				ReadOnly: true,
			},
			PushButton{
				Text:      "Copy",
				MaxSize:   Size{Width: 50},
				OnClicked: func() { walk.Clipboard().SetText(macText) },
			},
		},
	}.Run(nil)
}
