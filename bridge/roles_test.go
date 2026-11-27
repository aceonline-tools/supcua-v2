package bridge

import "testing"

// Every registered method must be classified exactly once, so the editor's default-deny matrix
// can never silently expose (or hide) a method.
func TestEveryRegisteredMethodIsClassifiedOnce(t *testing.T) {
	r := Roles()
	class := map[string]string{}
	for kind, l := range map[string][]string{"both": r.Both, "main": r.MainOnly, "editor": r.EditorOnly} {
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
	for _, n := range r.TransitionalMain {
		if class[n] != "editor" {
			t.Errorf("transitional %s must be editor-only, is %q", n, class[n])
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
