//go:build windows

package ui

import (
	"encoding/json"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/model"
)

// RawJSONEditor provides a text area for direct JSON editing of a script.
type RawJSONEditor struct {
	textEdit      *walk.TextEdit
	currentScript *model.Script
}

// NewRawJSONEditor creates the raw JSON editor inside the given parent.
func NewRawJSONEditor(parentContainer *walk.Composite) (*RawJSONEditor, error) {
	rawEditor := &RawJSONEditor{}

	builder := NewBuilder(parentContainer)

	err := (TextEdit{
		AssignTo: &rawEditor.textEdit,
		VScroll:  true,
		HScroll:  true,
	}).Create(builder)

	if err != nil {
		return nil, err
	}

	return rawEditor, nil
}

// LoadScript serializes the script to formatted JSON and displays it.
func (rawEditor *RawJSONEditor) LoadScript(script *model.Script) {
	rawEditor.currentScript = script
	jsonBytes, err := json.MarshalIndent(script, "", "  ")
	if err != nil {
		rawEditor.textEdit.SetText("Error: " + err.Error())
		return
	}
	rawEditor.textEdit.SetText(string(jsonBytes))
}

// GetEditedScript parses the current JSON text back into a Script.
func (rawEditor *RawJSONEditor) GetEditedScript() (*model.Script, error) {
	var editedScript model.Script
	err := json.Unmarshal([]byte(rawEditor.textEdit.Text()), &editedScript)
	if err != nil {
		return nil, err
	}
	return &editedScript, nil
}
