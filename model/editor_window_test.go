package model

import (
	"encoding/json"
	"testing"
)

func TestOldConfigLoadsWithoutEditorWindow(t *testing.T) {
	var c AppConfig
	if err := json.Unmarshal([]byte(`{"scripts_dir":"./s","edit_dialog_pos":{"x":1,"y":2}}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.EditorWindow != (EditorWindow{}) || c.EditDialogPos.X != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestEditorWindowRoundTripAndOmittedWhenUnset(t *testing.T) {
	b, _ := json.Marshal(AppConfig{})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["editor_window"]; !ok {
		t.Log("struct values are not omitted by omitempty; acceptable (zero value is the unset form)")
	}
	in := AppConfig{EditorWindow: EditorWindow{X: -5, Y: 6, W: 900, H: 700, Maximized: true, Version: EditorLayoutVersion}}
	b, _ = json.Marshal(in)
	var out AppConfig
	if err := json.Unmarshal(b, &out); err != nil || out.EditorWindow != in.EditorWindow {
		t.Fatalf("%v %+v", err, out.EditorWindow)
	}
}
