package bridge

import "github.com/yanmad27/supcua-v2/model"

var settingsCaptureFields = map[string]bool{"global_hotkey": true, "overlay_hotkey": true, "capture_hotkey": true}

func (b *Bridge) editorPresent() bool {
	b.mu.Lock()
	live := b.liveLocked() != nil
	b.mu.Unlock()
	if live {
		return true
	}
	w := b.winCtl()
	return w != nil && w.active()
}

// settingsCaptureStart begins recording one of the three Settings hotkeys. It is main-window only
// and refused while any editor session or window exists, so it can never share the capture slot
// with (or end) an editor capture; the backend additionally captures only while the main window is
// the foreground window. The recorded combo is delivered on settings.capture.hotkey and saved by
// the page through settings.update, i.e. with the normal validation and registration.
func (b *Bridge) settingsCaptureStart(p struct {
	Field string `json:"field"`
}) (any, error) {
	if !settingsCaptureFields[p.Field] {
		return nil, badRequest()
	}
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	if b.editorPresent() {
		return nil, newErr(CodeEditorBusy)
	}
	id, err := b.capture.Start(settingsCaptureSession, CaptureSettingsHotkey, p.Field, model.Hotkey{})
	if err != nil {
		return nil, err
	}
	if b.editorPresent() { // an editor window opened between the check and the start
		b.capture.EndSession(settingsCaptureSession, EndEditorClosed)
		return nil, newErr(CodeEditorBusy)
	}
	return captureStarted{CaptureID: id}, nil
}

// settingsCaptureStop ends the Settings recorder; with capture_id it only acts on that capture.
func (b *Bridge) settingsCaptureStop(p struct {
	CaptureID *uint64 `json:"capture_id"`
}) (any, error) {
	if p.CaptureID != nil && *p.CaptureID != b.capture.CurrentID() {
		return empty{}, nil
	}
	b.capture.EndSession(settingsCaptureSession, EndStopped)
	return empty{}, nil
}
