package bridge

// Target is the window set an event is delivered to (editor-window contract §2.4).
type Target uint8

const (
	TargetMain Target = iota
	TargetEditor
	TargetBoth
)

// TargetEmitter is implemented by a host with more than one window. An Emitter that does not
// implement it is the single-window host: every event goes to its one page.
type TargetEmitter interface {
	EmitTo(t Target, event string, payload any)
}

// TargetFor is the routing table. Unknown events go to main (the safe, previous behaviour).
func TargetFor(event string) Target {
	switch event {
	case EvState, EvScriptsChanged, EvSettingsChanged, EvSaveFailed, EvAppClosing:
		return TargetBoth
	case EvEditorReset, EvCaptureHotkey, EvCaptureKey, EvCaptureMouse, EvCaptureEnded,
		"editor.closeRequested", "editor.measureRequest", "editor.measureAbort":
		return TargetEditor
	}
	return TargetMain // update.*, nav.goto, editor.windowFailed, editor.windowClosed
}
