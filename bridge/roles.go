package bridge

// RoleLists classifies every method for the role matrix (editor-window contract §3.2). The host
// builds its matrix from these lists; the editor window is default-deny, so a method missing from
// Both and EditorOnly is unreachable from it.
//
//	Both       callable by the main and the editor window
//	MainOnly   main window only
//	EditorOnly editor window only (a main-side call is FORBIDDEN_ROLE)
//
// TransitionalMain is kept empty: the in-main editor route is gone (E2), so main can call no editor-only method.
type RoleLists struct {
	Both, MainOnly, EditorOnly, TransitionalMain []string
}

// SelftestMainAllow lists the editor-only methods the Windows self-test harness (ui/selftest_windows.go)
// still drives from the main page as a raw-RPC leak test. main.go passes it as the matrix's transitional
// set only under -selftest; the shipped app allows main none of them.
func SelftestMainAllow() []string {
	return []string{
		"editor.open", "editor.save", "editor.describeActions", "editor.toJSON", "editor.fromJSON",
		"capture.hotkey.start", "capture.key.start", "capture.mouse.start", "capture.mouse.stop", "capture.stop",
	}
}

// Roles returns the method classification. Names for methods that do not exist yet (E1b) are
// listed so their registration needs no matrix change.
func Roles() RoleLists {
	legacyEditor := []string{
		"editor.open", "editor.save", "editor.describeActions", "editor.toJSON", "editor.fromJSON",
		"capture.hotkey.start", "capture.key.start", "capture.mouse.start", "capture.mouse.stop", "capture.stop",
	}
	return RoleLists{
		Both: []string{
			"app.getBootState", "app.ready", "state.get", "actions.catalog", "clipboard.write", "settings.get", "app.closingReady",
		},
		MainOnly: []string{
			"app.setGlobalActive", "app.exit", "window.hide", "window.show", "window.reportBounds",
			"scripts.list", "scripts.get", "scripts.setEnabled", "scripts.setGroupEnabled", "scripts.setGroupCollapsed",
			"scripts.clone", "scripts.delete", "scripts.retrySave",
			"settings.update", "settings.setSidebarCollapsed",
			"overlay.set", "overlay.setStyle", "overlay.resetStyle", "autoactive.set", "update.apply", "update.skip",
			"editor.openWindow",
			// Legacy in-main close: main-only (the editor window gets FORBIDDEN_ROLE, §3.1). Becomes
			// host-internal in E1b and is removed with the in-main route in E2.
			"editor.close",
		},
		EditorOnly: append([]string{
			"editor.windowReady", "editor.measured", "editor.measureAck", "editor.checkpoint", "editor.guardActive", "editor.closeDecision", "editor.requestClose",
		}, legacyEditor...),
	}
}
