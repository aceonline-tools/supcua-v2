package bridge

import "testing"

// Every registered method must be classified exactly once, so the editor's default-deny matrix
// can never silently expose (or hide) a method.
func TestEveryRegisteredMethodIsClassifiedOnce(t *testing.T) {
	r := Roles()
	class := map[string]string{}
	for kind, l := range map[string][]string{"both": r.Both, "main": r.MainOnly, "editor": r.EditorOnly, "selftest": r.SelftestOnly} {
		for _, n := range l {
			if prev, dup := class[n]; dup {
				t.Errorf("%s classified as %s and %s", n, prev, kind)
			}
			class[n] = kind
		}
	}
	for n := range (&Bridge{}).Methods() {
		if class[n] == "" {
			t.Errorf("registered method %s has no role classification", n)
		}
	}
}

type fakeEmitter struct{ got []string }

func (f *fakeEmitter) Emit(e string, _ any) { f.got = append(f.got, "plain:"+e) }

type fakeTargetEmitter struct {
	fakeEmitter
	routed map[string]Target
}

func (f *fakeTargetEmitter) EmitTo(t Target, e string, _ any) {
	if f.routed == nil {
		f.routed = map[string]Target{}
	}
	f.routed[e] = t
}

func TestEventRoutingTable(t *testing.T) {
	want := map[string]Target{
		EvState: TargetBoth, EvScriptsChanged: TargetBoth, EvSettingsChanged: TargetBoth, EvSaveFailed: TargetBoth, EvAppClosing: TargetBoth,
		EvEditorReset: TargetEditor, EvCaptureHotkey: TargetEditor, EvCaptureKey: TargetEditor, EvCaptureMouse: TargetEditor, EvCaptureEnded: TargetEditor,
		"editor.closeRequested": TargetEditor, "editor.measureRequest": TargetEditor, "editor.measureAbort": TargetEditor,
		EvUpdateAvailable: TargetMain, EvUpdateProgress: TargetMain, EvUpdateFailed: TargetMain, EvNavGoto: TargetMain,
		"editor.windowFailed": TargetMain, "editor.windowClosed": TargetMain, "unknown.event": TargetMain,
	}
	for e, w := range want {
		if got := TargetFor(e); got != w {
			t.Errorf("%s -> %v want %v", e, got, w)
		}
	}
}

func TestEmitDeliversThroughTargetEmitterAndFallsBackForSingleWindowHosts(t *testing.T) {
	te := &fakeTargetEmitter{}
	b := &Bridge{emitter: te}
	b.emit(EvCaptureKey, nil)
	b.emit(EvState, nil)
	if te.routed[EvCaptureKey] != TargetEditor || te.routed[EvState] != TargetBoth || len(te.got) != 0 {
		t.Fatalf("routed=%v plain=%v", te.routed, te.got)
	}
	pe := &fakeEmitter{}
	(&Bridge{emitter: pe}).emit(EvCaptureKey, nil)
	if len(pe.got) != 1 {
		t.Fatalf("single-window emitter got %v", pe.got)
	}
}

func TestLegacyEditorCloseIsMainOnly(t *testing.T) {
	r := Roles()
	in := func(l []string, n string) bool {
		for _, x := range l {
			if x == n {
				return true
			}
		}
		return false
	}
	if !in(r.SelftestOnly, "editor.close") || in(r.MainOnly, "editor.close") || in(r.EditorOnly, "editor.close") || in(r.Both, "editor.close") {
		t.Fatalf("editor.close must be classified selftest-only only: %+v", r)
	}
}

func TestSelftestMainAllowIsEditorOnlyAndNotShipped(t *testing.T) {
	r := Roles()
	restricted := map[string]bool{}
	for _, n := range append(append([]string(nil), r.EditorOnly...), r.SelftestOnly...) {
		restricted[n] = true
	}
	for _, n := range SelftestMainAllow() {
		if !restricted[n] {
			t.Errorf("%s in SelftestMainAllow must be editor-only or selftest-only", n)
		}
	}
}

func TestSettingsCaptureIsMainOnlyAndRoutedToMain(t *testing.T) {
	r := Roles()
	in := func(list []string, m string) bool {
		for _, x := range list {
			if x == m {
				return true
			}
		}
		return false
	}
	for _, m := range []string{"settings.capture.start", "settings.capture.stop"} {
		if !in(r.MainOnly, m) || in(r.Both, m) || in(r.EditorOnly, m) || in(r.SelftestOnly, m) || in(SelftestMainAllow(), m) {
			t.Errorf("%s must be main-only and not in any editor list", m)
		}
	}
	for _, m := range []string{"capture.hotkey.start", "capture.key.start", "capture.mouse.start", "capture.stop"} {
		if in(r.MainOnly, m) || in(r.Both, m) {
			t.Errorf("%s must stay editor-only", m)
		}
	}
	for _, e := range []string{EvSettingsCaptureHotkey, EvSettingsCaptureEnded} {
		if TargetFor(e) != TargetMain {
			t.Errorf("%s must route to main only", e)
		}
	}
}

func TestUpdateCheckIsMainOnlyAndFenced(t *testing.T) {
	r := Roles()
	for _, n := range r.Both {
		if n == "update.check" {
			t.Fatal("update.check must not be callable by the editor window")
		}
	}
	for _, n := range r.EditorOnly {
		if n == "update.check" {
			t.Fatal("update.check is not editor-only")
		}
	}
	found := false
	for _, n := range r.MainOnly {
		found = found || n == "update.check"
	}
	if !found {
		t.Fatal("update.check missing from MainOnly")
	}
	inMutating := false
	for _, n := range mutating {
		inMutating = inMutating || n == "update.check"
	}
	if !inMutating {
		t.Fatal("update.check must be fenced like the other update methods")
	}
}
