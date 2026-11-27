package rpc

import (
	"testing"

	"github.com/yanmad27/supcua-v2/bridge"
)

// The real classification, through the real matrix: the editor window can never call the legacy
// editor.close (or any main-only method), main keeps the transitional editor methods.
func TestRealRoleMatrixForEditorClose(t *testing.T) {
	r := bridge.Roles()
	m := NewMatrix(r.Both, r.MainOnly, r.EditorOnly, r.TransitionalMain)
	for _, c := range []struct {
		role   Role
		method string
		want   bool
	}{
		{RoleEditor, "editor.close", false}, {RoleMain, "editor.close", true},
		{RoleEditor, "editor.save", true}, {RoleMain, "editor.save", true}, // transitional for main
		{RoleEditor, "scripts.list", false}, {RoleEditor, "actions.catalog", true},
		{RoleEditor, "selftest.anything", false}, // default deny
		{RoleMain, "capture.stop", true},
	} {
		if got := m.Allow(c.role, c.method); got != c.want {
			t.Errorf("%v %s = %v want %v", c.role, c.method, got, c.want)
		}
	}
}
