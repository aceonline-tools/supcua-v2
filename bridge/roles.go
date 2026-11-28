package bridge

// RoleLists classifies every method for the role matrix (editor-window contract §3.2). The host
// builds its matrix from these lists; the editor window is default-deny, so a method missing from
// Both and EditorOnly is unreachable from it.
//
//	Both       callable by the main and the editor window
//	MainOnly   main window only
//	EditorOnly editor window only (a main-side call is FORBIDDEN_ROLE)
//
//	SelftestOnly denied to both windows; only the self-test runs grant them to main (the
//	           selftest-only gen-0 route that the leak check drives from the main page)
type RoleLists struct {
	Both, MainOnly, EditorOnly, SelftestOnly []string
}

// SelftestMainAllow lists the editor-only methods the Windows self-test harness (ui/selftest_windows.go)
// drives from the main page as a raw-RPC leak test. A normal invocation grants main none of them;
// the diagnostic -selftest and -selftest-production runs widen the matrix with this list, in their
// isolated temp data directory.
func SelftestMainAllow() []string {
	return []string{
		"editor.open", "editor.close", "editor.save", "editor.describeActions", "editor.toJSON", "editor.fromJSON",
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
			"settings.update", "settings.setSidebarCollapsed", "settings.capture.start", "settings.capture.stop",
			"overlay.set", "overlay.setStyle", "overlay.resetStyle", "autoactive.set", "update.apply", "update.skip", "update.check",
			"editor.openWindow",
		},
		// The selftest-only gen-0 route's close (the leak check's main-page cycles): no production caller.
		SelftestOnly: []string{"editor.close"},
		EditorOnly: append([]string{
			"editor.windowReady", "editor.measured", "editor.measureAck", "editor.painted", "editor.checkpoint", "editor.guardActive", "editor.closeDecision", "editor.requestClose",
		}, legacyEditor...),
	}
}
