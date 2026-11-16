package rpc

import (
	"encoding/json"
	"testing"

	"github.com/yanmad27/supcua-v2/bridge"
)

// The dispatcher must carry the REAL *bridge.Error (code, message, field, conflicts) unchanged.
func TestRealBridgeErrorWireForm(t *testing.T) {
	want := &bridge.Error{
		Code: "HOTKEY_CONFLICT", Message: "conflict", Field: "toggle_hotkey",
		Conflicts: []bridge.HotkeyConflict{{ScriptID: "s1", Name: "Other", Field: "toggle_hotkey"}},
	}
	d, out := newStarted(t, map[string]Handler{
		"x": func(json.RawMessage) (any, error) { return nil, want },
		"y": func(json.RawMessage) (any, error) { return nil, &bridge.Error{Code: "NAME_TAKEN", Message: "taken"} },
	}, 4)
	d.Submit(`{"id":1,"method":"x"}`)
	var got struct {
		ID    int64
		OK    bool
		Error bridge.Error
	}
	if err := json.Unmarshal([]byte(next(t, out)), &got); err != nil {
		t.Fatal(err)
	}
	if got.OK || got.ID != 1 || got.Error.Code != want.Code || got.Error.Message != want.Message ||
		got.Error.Field != want.Field || len(got.Error.Conflicts) != 1 || got.Error.Conflicts[0].ScriptID != "s1" || got.Error.Conflicts[0].Name != "Other" {
		t.Fatalf("lost fields: %+v", got)
	}
	d.Submit(`{"id":2,"method":"y"}`)
	if s := next(t, out); s != `{"id":2,"ok":false,"error":{"code":"NAME_TAKEN","message":"taken"}}` {
		t.Fatal(s)
	}
}
