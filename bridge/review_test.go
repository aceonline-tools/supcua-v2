package bridge

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
)

// Finding 1: renames go through an exclude-ID-aware check and can never overwrite another file.
func TestRenameOntoAnotherScriptsFileIsRejectedWithRealStore(t *testing.T) {
	dir := t.TempDir()
	store := storage.NewScriptStore(filepath.Join(dir, "s"))
	_ = store.Save(script("a", "Alpha", "", true))
	_ = store.Save(script("b", "Beta", "", true))
	cfg, _ := NewConfigService(storage.NewConfigStore(filepath.Join(dir, "c.json")))
	b := New(Deps{Controller: app.NewController(nil, nil, nil, nil), Store: store, Config: cfg}, &recEmitter{})
	m := b.Methods()
	open, err := m["editor.open"]([]byte(`{"id":"b"}`))
	if err != nil {
		t.Fatal(err)
	}
	sess := open.(EditorSession)
	sess.Script.Name = "alpha"
	body, _ := json.Marshal(map[string]any{"sessionId": sess.SessionID, "script": sess.Script})
	_, err = m["editor.save"](body)
	wantCode(t, err, CodeNameTaken)
	sess.Script.Name = "Beta Renamed"
	body, _ = json.Marshal(map[string]any{"sessionId": sess.SessionID, "script": sess.Script})
	if _, err = m["editor.save"](body); err != nil {
		t.Fatalf("free rename must work: %v", err)
	}
	if list, _ := store.List(); len(list) != 2 {
		t.Fatalf("no script may be lost: %+v", list)
	}
}

// Finding 2: a flush timeout is never reported as saved, and the draft is kept.
func TestFlushTimeoutIsSaveFailedAndKeepsDraft(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	sid, edit := openEditor(t, r, "s1")
	edit["name"] = "Alpha 2"
	r.store.flushOK = false
	_, err := r.call(t, "editor.save", mustJSON(map[string]any{"sessionId": sid, "script": edit}))
	wantCode(t, err, CodeSaveFailed)
	r.store.flushOK = true
	res := r.mustCall(t, "editor.close", `{"sessionId":"`+sid+`"}`)
	if res["saved"] != true {
		t.Errorf("kept draft should autosave once flushing works: %v", res)
	}
	sid, edit = openEditor(t, r, "s1")
	edit["name"] = "Alpha 3"
	r.store.flushOK = false
	res = r.mustCall(t, "editor.close", mustJSON(map[string]any{"sessionId": sid, "script": edit}))
	if res["saved"] != false || res["reason"] != "could not write script file" {
		t.Errorf("timeout on close must report Not saved: %v", res)
	}
}

type queueUI struct {
	mu   sync.Mutex
	jobs []func()
}

func (q *queueUI) Post(fn func()) { q.mu.Lock(); q.jobs = append(q.jobs, fn); q.mu.Unlock() }
func (q *queueUI) runAll() {
	q.mu.Lock()
	jobs := q.jobs
	q.jobs = nil
	q.mu.Unlock()
	for _, j := range jobs {
		j()
	}
}

// Finding 3: a timed-out UI job is cancelled and never runs later; a started job is awaited.
func TestCallUICancelsTimedOutJobAndAwaitsStartedOne(t *testing.T) {
	q := &queueUI{}
	b := &Bridge{d: Deps{UI: q}}
	var ran atomic.Bool
	old := uiCallTimeoutForTest(20 * time.Millisecond)
	defer old()
	err := b.callUI(func() error { ran.Store(true); return nil })
	wantCode(t, err, CodeBusy)
	q.runAll()
	if ran.Load() {
		t.Fatal("a cancelled job must never run")
	}
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		time.Sleep(5 * time.Millisecond)
		q.runAll()
	}()
	res := make(chan error, 1)
	go func() {
		res <- b.callUI(func() error { close(started); <-release; return newErr(CodeInternal) })
	}()
	<-started
	time.Sleep(60 * time.Millisecond) // past the timeout while the job is running
	close(release)
	wantCode(t, <-res, CodeInternal)
}

func TestOverlayErrorsPropagateAndEnableRollsBackOnPersistFailure(t *testing.T) {
	r := newRig(t)
	r.per.fail = true
	_, err := r.call(t, "overlay.set", `{"enabled":false}`)
	wantCode(t, err, CodeSaveFailed)
	if !r.ov.State().Enabled {
		t.Error("overlay must be re-enabled after the persist failure")
	}
	r.per.fail = false

	q := &queueUI{}
	r.b.d.UI = q
	old := uiCallTimeoutForTest(20 * time.Millisecond)
	defer old()
	_, err = r.call(t, "overlay.set", `{"visible":false}`)
	wantCode(t, err, CodeBusy)
	q.runAll()
	if !r.ov.State().Visible {
		t.Error("a cancelled visible job must not run")
	}
}

// Finding 4: capture teardown races.
func TestCaptureLateTimerAndSerializedBackend(t *testing.T) {
	be := &fakeBackend{}
	em := &recEmitter{}
	m := newCaptureManager(be, em.Emit, 40*time.Millisecond)
	if err := m.Start("s", CaptureKey, "", model.Hotkey{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ { // activity keeps pushing the deadline past the original timer
		time.Sleep(20 * time.Millisecond)
		be.sink.Key("a")
	}
	if mode, _ := m.Active(); mode != CaptureKey {
		t.Fatalf("active capture must survive while events arrive, got %q", mode)
	}
	m.expire(1) // a stale timer callback for the current generation before its deadline
	if mode, _ := m.Active(); mode != CaptureKey {
		t.Fatal("early expire must be ignored")
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_ = m.Start("s", CaptureMouse, "", model.Hotkey{})
			} else {
				m.End(EndStopped)
			}
		}(i)
	}
	wg.Wait()
	m.End(EndStopped)
	be.mu.Lock()
	defer be.mu.Unlock()
	if len(be.starts) == 0 || be.stops < len(be.starts) {
		t.Errorf("every started hook must be stopped: starts=%d stops=%d", len(be.starts), be.stops)
	}
}

// Finding 6: shutdown fence.
func TestBeginShutdownRejectsMutationsAndWaitsForClosingReady(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	sid, edit := openEditor(t, r, "s1")
	thenRan := make(chan struct{})
	r.b.BeginShutdown(5*time.Second, func() { close(thenRan) })
	r.b.BeginShutdown(5*time.Second, func() { t.Error("second BeginShutdown must be a no-op") })
	ev := r.em.named(EvAppClosing)
	if len(ev) != 1 || ev[0].payload.(map[string]int64)["deadline_ms"] != 5000 {
		t.Fatalf("app.closing = %v", ev)
	}
	for _, c := range []struct{ m, p string }{
		{"scripts.setEnabled", `{"id":"s1","enabled":false}`}, {"editor.open", `{"id":null}`},
		{"settings.update", `{"patch":{"theme":"dark"}}`}, {"app.setGlobalActive", `{"active":false}`},
	} {
		_, err := r.call(t, c.m, c.p)
		wantCode(t, err, CodeBusy)
	}
	edit["name"] = "Alpha 2"
	r.mustCall(t, "editor.save", mustJSON(map[string]any{"sessionId": sid, "script": edit}))
	r.mustCall(t, "editor.close", `{"sessionId":"`+sid+`","autosave":false}`)
	r.mustCall(t, "scripts.list", "")
	select {
	case <-thenRan:
		t.Fatal("then must wait for app.closingReady")
	case <-time.After(30 * time.Millisecond):
	}
	r.mustCall(t, "app.closingReady", "")
	select {
	case <-thenRan:
	case <-time.After(2 * time.Second):
		t.Fatal("then did not run after closingReady")
	}
}

func TestBeginShutdownDeadlineRunsThen(t *testing.T) {
	r := newRig(t)
	done := make(chan struct{})
	r.b.BeginShutdown(20*time.Millisecond, func() { close(done) })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deadline must trigger then")
	}
}

func TestNoSaveIsQueuedAfterFinalFlush(t *testing.T) {
	r := newRig(t, script("s1", "A", "", false))
	if err := r.b.Shutdown(time.Second); err != nil {
		t.Fatal(err)
	}
	n := len(r.store.calls())
	for _, c := range []struct{ m, p string }{
		{"scripts.setEnabled", `{"id":"s1","enabled":true}`}, {"scripts.retrySave", `{"id":"s1"}`},
		{"scripts.clone", `{"id":"s1"}`}, {"editor.save", `{"sessionId":"x","script":{}}`},
	} {
		_, err := r.call(t, c.m, c.p)
		wantCode(t, err, CodeBusy)
	}
	if len(r.store.calls()) != n {
		t.Error("a save was queued after shutdown")
	}
}

func TestShutdownWaitsForInFlightMutation(t *testing.T) {
	r := newRig(t, script("s1", "A", "", false))
	entered := make(chan struct{})
	release := make(chan struct{})
	r.store.flushHook = func() {} // keep default
	h := r.m["scripts.setEnabled"]
	r.b.fence.RLock() // simulate a handler in flight
	go func() { close(entered); <-release; r.b.fence.RUnlock() }()
	<-entered
	done := make(chan error, 1)
	go func() { done <- r.b.Shutdown(2 * time.Second) }()
	time.Sleep(20 * time.Millisecond)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_, err := h([]byte(`{"id":"s1","enabled":true}`))
	wantCode(t, err, CodeBusy)
}

func TestResetSessionEndsCaptureEditorAndIsIdempotent(t *testing.T) {
	r := newRig(t, script("s1", "A", "", true))
	sid, _ := openEditor(t, r, "s1")
	r.mustCall(t, "capture.key.start", `{"sessionId":"`+sid+`"}`)
	r.b.ResetSession("editorClosed")
	r.b.ResetSession("bogus-reason")
	if r.ctl.Snapshot().EditDialogOpen {
		t.Error("edit-dialog-open must be cleared")
	}
	ended := r.em.named(EvCaptureEnded)
	if len(ended) != 1 || ended[0].payload.(map[string]string)["reason"] != "editorClosed" {
		t.Errorf("capture.ended = %v", ended)
	}
	_, err := r.call(t, "editor.save", `{"sessionId":"`+sid+`","script":{}}`)
	wantCode(t, err, CodeEditorClosed)
	openEditor(t, r, "s1")
}

// Finding 7 and minors.
func TestHotkeyVocabularyNormalizationAndPunctuation(t *testing.T) {
	for _, in := range []string{"Ctrl + ;", "Alt + `", "Ctrl + Shift + [", "Ctrl + \\", "LWin", "Ctrl + Space"} {
		hk, ok := ParseHotkey(in)
		if !ok {
			t.Errorf("%q should parse", in)
			continue
		}
		if back, ok := ParseHotkey(DisplayHotkey(hk)); !ok || back.Key != hk.Key {
			t.Errorf("%q does not round-trip: %v", in, back)
		}
	}
	for _, in := range []string{"Ctrl + Ctrl + A", "Ctrl + Up", "Ctrl + f13", "Ctrl + aa", "Shift +", "Foo + A"} {
		if _, ok := ParseHotkey(in); ok {
			t.Errorf("%q must be rejected", in)
		}
	}
	hk, _ := ParseHotkey("Shift + Ctrl + A")
	if DisplayHotkey(hk) != "Ctrl + Shift + A" {
		t.Errorf("modifiers must be ordered: %v", hk)
	}

	r := newRig(t, script("s1", "A", "", true))
	sid, edit := openEditor(t, r, "s1")
	edit["toggle_hotkey"] = map[string]any{"modifiers": []string{"CTRL", "ctrl", "Shift"}, "key": ";"}
	res := r.mustCall(t, "editor.save", mustJSON(map[string]any{"sessionId": sid, "script": edit}))
	got := res["script"].(map[string]any)["toggle_hotkey"].(map[string]any)
	if mods := got["modifiers"].([]any); len(mods) != 2 || mods[0] != "ctrl" || mods[1] != "shift" || got["key"] != ";" {
		t.Errorf("hotkey not normalised: %v", got)
	}
	edit["toggle_hotkey"] = map[string]any{"modifiers": []string{}, "key": "up"}
	_, err := r.call(t, "editor.save", mustJSON(map[string]any{"sessionId": sid, "script": edit}))
	wantCode(t, err, CodeBadRequest)
}

func TestStrictParamShapes(t *testing.T) {
	r := newRig(t, script("s1", "A", "", true))
	for _, c := range []struct{ m, p string }{
		{"scripts.setEnabled", `{"id":"s1"}`},
		{"scripts.setEnabled", `{"enabled":true}`},
		{"scripts.setEnabled", `{"id":"s1","enabled":null}`},
		{"scripts.setEnabled", `null`},
		{"scripts.setEnabled", `[]`},
		{"scripts.setGroupEnabled", `{"group":"g"}`},
		{"scripts.setGroupCollapsed", `{"group":"g"}`},
		{"app.setGlobalActive", `{}`},
		{"editor.open", `{}`},
		{"state.get", `{"x":1}`},
		{"scripts.list", `[1]`},
		{"settings.get", `"x"`},
	} {
		_, err := r.call(t, c.m, c.p)
		wantCode(t, err, CodeBadRequest)
	}
	for _, c := range []struct{ m, p string }{{"state.get", ``}, {"state.get", `{}`}, {"state.get", `null`}, {"editor.open", `{"id":null}`}} {
		if _, err := r.call(t, c.m, c.p); err != nil {
			t.Errorf("%s(%s) should be accepted: %v", c.m, c.p, err)
		}
	}
	r.mustCall(t, "editor.close", `{"sessionId":"`+mustSession(t, r)+`"}`) // autosave omitted = true
}

func mustSession(t *testing.T, r *rig) string {
	t.Helper()
	m, _ := r.b.session, 0
	if m == nil {
		t.Fatal("no session")
	}
	return m.id
}

func TestEditorCloseAutosaveDefaultsToTrue(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	sid, edit := openEditor(t, r, "s1")
	edit["name"] = "Renamed"
	res := r.mustCall(t, "editor.close", mustJSON(map[string]any{"sessionId": sid, "script": edit}))
	if res["saved"] != true {
		t.Errorf("omitted autosave must default to true: %v", res)
	}
	sid, edit = openEditor(t, r, "s1")
	edit["name"] = "Other"
	res = r.mustCall(t, "editor.close", mustJSON(map[string]any{"sessionId": sid, "autosave": false, "script": edit}))
	if res["saved"] != false || r.mustCall(t, "scripts.get", `{"id":"s1"}`)["name"] != "Renamed" {
		t.Errorf("explicit autosave:false must not save: %v", res)
	}
}
