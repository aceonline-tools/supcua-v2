package bridge

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yanmad27/supcua-v2/model"
)

const maxJSONText = 1 << 20

// editorSession is the only editor state kept: it lives until editor.close and holds at most the
// last draft whose save failed (for autosave-on-close).
type editorSession struct {
	id       string
	scriptID string
	draft    *model.Script
}

func (b *Bridge) editorOpen(p struct {
	ID *string `json:"id"`
}) (any, error) {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, newErr(CodeEditorClosed)
	}
	if b.session != nil {
		b.mu.Unlock()
		return nil, newErr(CodeEditorBusy)
	}
	var script model.Script
	if p.ID != nil {
		i := b.indexLocked(*p.ID)
		if i < 0 {
			b.mu.Unlock()
			return nil, newErr(CodeNotFound)
		}
		script = b.scripts[i]
	} else {
		name := "New Script"
		for n := 2; b.nameTakenLocked(name, ""); n++ {
			name = fmt.Sprintf("New Script %d", n)
		}
		script = model.Script{
			ID: uuid.New().String(), Name: name, Enabled: true,
			RunMode: model.RunModeOnce, HotkeyMode: model.HotkeyModeToggle, Actions: []model.Action{},
		}
	}
	sess := &editorSession{id: uuid.New().String(), scriptID: script.ID}
	b.session = sess
	b.mu.Unlock()

	b.d.Controller.SetEditDialogOpen(true)
	return EditorSession{SessionID: sess.id, Script: script, Catalog: buildCatalog()}, nil
}

func (b *Bridge) requireSession(id string) (*editorSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil || b.session.id != id || id == "" {
		return nil, newErr(CodeEditorClosed)
	}
	return b.session, nil
}

type saveResult struct {
	Script model.Script `json:"script"`
	Rev    uint64       `json:"rev"`
}

func (b *Bridge) editorSave(p struct {
	SessionID         string       `json:"sessionId"`
	Script            model.Script `json:"script"`
	OverrideConflicts bool         `json:"overrideConflicts"`
	IgnoreConflicts   bool         `json:"ignore_conflicts"`
}) (any, error) {
	sess, err := b.requireSession(p.SessionID)
	if err != nil {
		return nil, err
	}
	res, err := b.saveScript(sess, p.Script, p.OverrideConflicts, p.IgnoreConflicts)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// saveScript validates and persists script as the session's script (one QueueSave for everything
// changed, including hotkeys cleared on other scripts). The draft is remembered only on failure.
func (b *Bridge) saveScript(sess *editorSession, in model.Script, override, ignore bool) (saveResult, error) {
	script := in
	script.ID = sess.scriptID
	fail := func(e error) (saveResult, error) {
		b.mu.Lock()
		if b.session == sess {
			d := script
			sess.draft = &d
		}
		b.mu.Unlock()
		return saveResult{}, e
	}
	if e := validateScript(&script, b.known); e != nil {
		return fail(e)
	}

	b.mu.Lock()
	taken := b.nameTakenLocked(script.Name, script.ID)
	others := make([]model.Script, 0, len(b.scripts))
	for _, s := range b.scripts {
		if s.ID != script.ID {
			others = append(others, s)
		}
	}
	b.mu.Unlock()
	b.at("validated")
	if taken || b.d.Store.NameOrFileTakenExcept(script.Name, script.ID) {
		return fail(newErr(CodeNameTaken))
	}

	var conflicts []HotkeyConflict
	if !ignore { // close-time flush: keep duplicate hotkeys as is and touch no other script
		conflicts = b.findConflicts(script, others)
	}
	if len(conflicts) > 0 && !override {
		e := newErr(CodeHotkeyConflict)
		e.Conflicts = conflicts
		return fail(e)
	}
	changed := []model.Script{script}
	if len(conflicts) > 0 {
		cleared := map[string]int{}
		for i := range others {
			for _, c := range conflicts {
				if others[i].ID == c.ScriptID {
					b.checker.ClearConflictingHotkey(&others[i], c.Field)
					if _, ok := cleared[others[i].ID]; !ok {
						cleared[others[i].ID] = i
					}
				}
			}
		}
		for _, i := range cleared {
			changed = append(changed, others[i])
		}
	}

	b.at("commit")
	b.mu.Lock()
	if b.session != sess { // ResetSession (or close) invalidated this operation after validation
		b.mu.Unlock()
		return saveResult{}, newErr(CodeEditorClosed)
	}
	for _, s := range changed {
		if i := b.indexLocked(s.ID); i >= 0 {
			b.scripts[i] = s
		} else {
			b.scripts = append(b.scripts, s)
		}
	}
	b.rev++
	rev := b.rev
	if b.session == sess {
		sess.draft = nil
	}
	b.mu.Unlock()

	if !b.queue(changed) {
		return fail(newErr(CodeSaveFailed))
	}
	flushed := b.d.Store.FlushSaves(saveWait)
	b.mu.Lock()
	var failed bool
	for _, s := range changed {
		if _, bad := b.failed[s.ID]; bad {
			failed = true
		}
	}
	b.mu.Unlock()
	if failed || !flushed {
		return fail(newErr(CodeSaveFailed))
	}
	b.emit(EvScriptsChanged, revResult{rev})
	return saveResult{Script: script, Rev: rev}, nil
}

// findConflicts lists every hotkey of script that another script already uses.
func (b *Bridge) findConflicts(script model.Script, others []model.Script) []HotkeyConflict {
	var out []HotkeyConflict
	for _, mine := range []*model.Hotkey{script.ToggleHotkey, script.StartHotkey, script.StopHotkey} {
		if mine == nil {
			continue
		}
		for _, o := range others {
			for field, hk := range map[string]*model.Hotkey{
				"toggle_hotkey": o.ToggleHotkey, "start_hotkey": o.StartHotkey, "stop_hotkey": o.StopHotkey,
			} {
				if hk != nil && b.checker.HotkeysEqual(*mine, *hk) {
					out = append(out, HotkeyConflict{ScriptID: o.ID, Name: o.Name, Field: field, Hotkey: *hk})
				}
			}
		}
	}
	sortConflicts(out)
	return out
}

func sortConflicts(c []HotkeyConflict) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && (c[j].ScriptID < c[j-1].ScriptID ||
			(c[j].ScriptID == c[j-1].ScriptID && c[j].Field < c[j-1].Field)); j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}

type closeResult struct {
	Saved  bool   `json:"saved"`
	Rev    uint64 `json:"rev"`
	Reason string `json:"reason,omitempty"`
}

// editorClose (autosave defaults to true when omitted, §9.1) always releases the session, stops captures and re-enables execution. With autosave,
// the supplied (or last failed) draft is saved when valid; the result reports Saved / Not saved: reason.
func (b *Bridge) editorClose(p struct {
	SessionID string        `json:"sessionId"`
	Autosave  *bool         `json:"autosave"`
	Script    *model.Script `json:"script"`
}) (any, error) {
	sess, err := b.requireSession(p.SessionID)
	if err != nil {
		return nil, err
	}
	b.capture.End(EndEditorClosed)

	res := closeResult{}
	if p.Autosave == nil || *p.Autosave {
		draft := p.Script
		if draft == nil {
			b.mu.Lock()
			draft = sess.draft
			b.mu.Unlock()
		}
		if draft != nil {
			saved, serr := b.saveScript(sess, *draft, false, false)
			if serr != nil {
				var e *Error
				if errAs(serr, &e) {
					res.Reason = e.Message
				} else {
					res.Reason = messages[CodeInternal]
				}
			} else {
				res.Saved, res.Rev = true, saved.Rev
			}
		}
	}
	b.sessMu.Lock()
	b.mu.Lock()
	if b.session == sess {
		b.session = nil
	}
	if !res.Saved {
		res.Rev = b.rev
	}
	b.mu.Unlock()
	b.d.Controller.SetEditDialogOpen(false)
	b.sessMu.Unlock()
	return res, nil
}

func errAs(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

func (b *Bridge) editorDescribe(p struct {
	Actions []model.Action `json:"actions"`
}) (any, error) {
	if len(p.Actions) > maxActions {
		return nil, badRequest()
	}
	out := make([]string, len(p.Actions))
	for i, a := range p.Actions {
		out[i] = describeAction(a)
	}
	return out, nil
}

func (b *Bridge) editorToJSON(p struct {
	Script model.Script `json:"script"`
}) (any, error) {
	data, err := json.MarshalIndent(p.Script, "", "  ")
	if err != nil {
		return nil, newErr(CodeInternal)
	}
	return struct {
		Text string `json:"text"`
	}{string(data)}, nil
}

func (b *Bridge) editorFromJSON(p struct {
	Text string `json:"text"`
}) (any, error) {
	if len(p.Text) > maxJSONText {
		return nil, badRequest()
	}
	var s model.Script
	dec := json.NewDecoder(strings.NewReader(p.Text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil || dec.More() {
		return nil, badRequest()
	}
	if validateScript(&s, b.known) != nil {
		return nil, badRequest()
	}
	return struct {
		Script model.Script `json:"script"`
	}{s}, nil
}

func (b *Bridge) captureStart(sessionID string, mode CaptureMode, field string) (any, error) {
	// sessMu makes "session still open" and the capture start one step against ResetSession.
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	if _, err := b.requireSession(sessionID); err != nil {
		return nil, err
	}
	id, err := b.capture.Start(sessionID, mode, field, b.d.Config.Get().CaptureHotkey)
	if err != nil {
		return nil, err
	}
	return captureStarted{CaptureID: id}, nil
}

type captureStarted struct {
	CaptureID uint64 `json:"capture_id"`
}

func (b *Bridge) captureHotkeyStart(p struct {
	SessionID string `json:"sessionId"`
	Field     string `json:"field"`
}) (any, error) {
	switch p.Field {
	case "toggle", "start", "stop":
	default:
		return nil, badRequest()
	}
	return b.captureStart(p.SessionID, CaptureHotkey, p.Field)
}

type sessionParam struct {
	SessionID string `json:"sessionId"`
}

func (b *Bridge) captureKeyStart(p sessionParam) (any, error) {
	return b.captureStart(p.SessionID, CaptureKey, "")
}

func (b *Bridge) captureMouseStart(p sessionParam) (any, error) {
	return b.captureStart(p.SessionID, CaptureMouse, "")
}

type stopParam struct {
	SessionID string  `json:"sessionId"`
	CaptureID *uint64 `json:"capture_id"`
}

// captureStop ends the active capture. With capture_id present it only acts on that capture;
// a stale or unknown id is a no-op, so the page never ends a newer capture by accident.
func (b *Bridge) captureStop(p stopParam) (any, error) {
	if _, err := b.requireSession(p.SessionID); err != nil {
		return nil, err
	}
	if p.CaptureID != nil && *p.CaptureID != b.capture.CurrentID() {
		return empty{}, nil
	}
	b.capture.End(EndStopped)
	return empty{}, nil
}
