package rpc

import (
	"testing"

	"github.com/yanmad27/supcua-v2/bridge"
)

// The real classification, through the real PRODUCTION matrix: the selftest-only gen-0 route
// (editor.close, and main's editor.open) is denied to both windows; main can call no editor-only
// method; the editor window no main-only one.
func TestRealRoleMatrixForEditorClose(t *testing.T) {
	r := bridge.Roles()
	m := NewMatrix(r.Both, r.MainOnly, r.EditorOnly, r.SelftestOnly)
	for _, c := range []struct {
		role   Role
		method string
		want   bool
	}{
		{RoleEditor, "editor.close", false}, {RoleMain, "editor.close", false},
		{RoleEditor, "editor.open", true}, {RoleMain, "editor.open", false},
		{RoleEditor, "editor.save", true}, {RoleMain, "editor.save", false}, {RoleMain, "capture.stop", false},
		{RoleEditor, "scripts.list", false}, {RoleEditor, "actions.catalog", true},
		{RoleEditor, "selftest.anything", false}, // default deny
	} {
		if got := m.Allow(c.role, c.method); got != c.want {
			t.Errorf("production %v %s = %v want %v", c.role, c.method, got, c.want)
		}
	}
	// Only a self-test run grants the gen-0 route to main, never to the editor window.
	st := m.GrantMain(bridge.SelftestMainAllow())
	for _, c := range []struct {
		role   Role
		method string
		want   bool
	}{
		{RoleMain, "editor.close", true}, {RoleMain, "editor.open", true},
		{RoleEditor, "editor.close", false}, {RoleEditor, "scripts.list", false},
	} {
		if got := st.Allow(c.role, c.method); got != c.want {
			t.Errorf("selftest %v %s = %v want %v", c.role, c.method, got, c.want)
		}
	}
	if !m.Allow(RoleMain, "scripts.list") || m.Allow(RoleMain, "editor.close") {
		t.Error("GrantMain must not change the production matrix")
	}
}
