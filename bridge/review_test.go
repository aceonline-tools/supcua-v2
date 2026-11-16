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
	if _, err := m.Start("s", CaptureKey, "", model.Hotkey{}); err != nil {
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
				_, _ = m.Start("s", CaptureMouse, "", model.Hotkey{})
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
	if len(ended) != 1 || ended[0].payload.(endedEvent).Reason != "editorClosed" {
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

// Round 2, finding 1: no save may be queued after the final flush, even when a handler stalls
// between its entry check and QueueSave.
func TestStalledHandlerCannotQueueAfterFinalFlush(t *testing.T) {
	r := newRig(t, script("s1", "A", "", false))
	entered := make(chan struct{})
	release := make(chan struct{})
	r.b.testHook = func(stage string) {
		if stage == "queue" {
			close(entered)
			<-release
		}
	}
	res := make(chan error, 1)
	go func() { _, err := r.m["scripts.setEnabled"]([]byte(`{"id":"s1","enabled":true}`)); res <- err }()
	<-entered
	err := r.b.Shutdown(100 * time.Millisecond)
	if err == nil {
		t.Error("an unquiesced handler must be reported as unresolved")
	}
	callsAtFlush, flushed := len(r.store.calls()), r.store.flushed
	close(release)
	wantCode(t, <-res, CodeBusy)
	if len(r.store.calls()) != callsAtFlush || flushed != 1 {
		t.Fatalf("a save was queued after the final flush: before=%d after=%d", callsAtFlush, len(r.store.calls()))
	}
}

func TestBeginShutdownWaitsForInFlightHandlerBeforeThen(t *testing.T) {
	r := newRig(t, script("s1", "A", "", false))
	entered := make(chan struct{})
	release := make(chan struct{})
	r.b.testHook = func(stage string) {
		if stage == "queue" {
			close(entered)
			<-release
		}
	}
	res := make(chan error, 1)
	go func() { _, err := r.m["scripts.setEnabled"]([]byte(`{"id":"s1","enabled":true}`)); res <- err }()
	<-entered
	var order []string
	var mu sync.Mutex
	thenDone := make(chan struct{})
	r.b.BeginShutdown(5*time.Millisecond, func() { mu.Lock(); order = append(order, "then"); mu.Unlock(); close(thenDone) })
	time.Sleep(60 * time.Millisecond)
	select {
	case <-thenDone:
		t.Fatal("then must wait for the in-flight mutation")
	default:
	}
	mu.Lock()
	order = append(order, "release")
	mu.Unlock()
	close(release)
	if err := <-res; err != nil {
		t.Fatalf("in-flight mutation must complete normally: %v", err)
	}
	select {
	case <-thenDone:
	case <-time.After(3 * time.Second):
		t.Fatal("then never ran")
	}
	if len(r.store.calls()) != 1 || order[0] != "release" {
		t.Errorf("order=%v calls=%d", order, len(r.store.calls()))
	}
}

// Round 2, finding 2: session generation. Reset between validation and commit.
func TestResetBetweenValidateAndCommitMakesSaveANoOp(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	sid, edit := openEditor(t, r, "s1")
	edit["name"] = "Changed"
	r.b.testHook = func(stage string) {
		if stage == "commit" {
			r.b.ResetSession("editorClosed")
		}
	}
	_, err := r.call(t, "editor.save", mustJSON(map[string]any{"sessionId": sid, "script": edit}))
	wantCode(t, err, CodeEditorClosed)
	if len(r.store.calls()) != 0 || r.mustCall(t, "scripts.get", `{"id":"s1"}`)["name"] != "Alpha" {
		t.Error("a stale save must neither commit nor queue")
	}
}

func TestResetAfterCommitKeepsCommittedSaveAndDropsDraft(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	sid, edit := openEditor(t, r, "s1")
	edit["name"] = "Changed"
	r.b.testHook = func(stage string) {
		if stage == "queue" {
			r.b.ResetSession("editorClosed")
		}
	}
	if _, err := r.call(t, "editor.save", mustJSON(map[string]any{"sessionId": sid, "script": edit})); err != nil {
		t.Fatalf("a committed save must finish: %v", err)
	}
	if len(r.store.calls()) != 1 || r.b.session != nil {
		t.Errorf("calls=%d session=%v", len(r.store.calls()), r.b.session)
	}
}

func TestResetDuringFailedSaveDoesNotRetainDraft(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true), script("s2", "Beta", "", true))
	sid, edit := openEditor(t, r, "s1")
	sess := r.b.session
	edit["name"] = "Beta" // NAME_TAKEN
	r.b.testHook = func(stage string) {
		if stage == "validated" {
			r.b.ResetSession("editorClosed")
		}
	}
	_, err := r.call(t, "editor.save", mustJSON(map[string]any{"sessionId": sid, "script": edit}))
	wantCode(t, err, CodeNameTaken)
	if sess.draft != nil {
		t.Error("a reset session must not retain a draft")
	}
}

type blockingBackend struct {
	*fakeBackend
	entered chan struct{}
	release chan struct{}
}

func (b *blockingBackend) Start(m CaptureMode, p model.Hotkey, s CaptureSink) error {
	close(b.entered)
	<-b.release
	return b.fakeBackend.Start(m, p, s)
}

func TestResetRacesCaptureStartBothInterleavings(t *testing.T) {
	// reset first: a start with the old session id is refused and no hook is installed
	r := newRig(t, script("s1", "A", "", true))
	sid, _ := openEditor(t, r, "s1")
	r.b.ResetSession("editorClosed")
	_, err := r.call(t, "capture.key.start", `{"sessionId":"`+sid+`"}`)
	wantCode(t, err, CodeEditorClosed)
	if len(r.cap.starts) != 0 {
		t.Fatal("capture must not start for a reset session")
	}

	// start first (backend still starting): reset waits for it and then tears it down
	r = newRig(t, script("s1", "A", "", true))
	sid, _ = openEditor(t, r, "s1")
	bb := &blockingBackend{fakeBackend: r.cap, entered: make(chan struct{}), release: make(chan struct{})}
	r.b.capture = newCaptureManager(bb, r.b.emit, time.Hour)
	startErr := make(chan error, 1)
	go func() { _, err := r.call(t, "capture.key.start", `{"sessionId":"`+sid+`"}`); startErr <- err }()
	<-bb.entered
	resetDone := make(chan struct{})
	go func() { r.b.ResetSession("editorClosed"); close(resetDone) }()
	select {
	case <-resetDone:
		t.Fatal("reset must wait for the in-progress capture start")
	case <-time.After(30 * time.Millisecond):
	}
	close(bb.release)
	if err := <-startErr; err != nil {
		t.Fatal(err)
	}
	<-resetDone
	if mode, _ := r.b.capture.Active(); mode != CaptureNone {
		t.Error("capture must be ended after reset")
	}
	r.cap.mu.Lock()
	defer r.cap.mu.Unlock()
	if len(r.cap.starts) != 1 || r.cap.stops != 1 {
		t.Errorf("starts=%d stops=%d", len(r.cap.starts), r.cap.stops)
	}
}

// Round 2, finding 3.
func TestNoParamMethodsAcceptAnyWhitespaceEmptyObject(t *testing.T) {
	r := newRig(t)
	for _, p := range []string{"", "{}", "{ }", "{\n}", " \t{\r\n} ", "null"} {
		if _, err := r.call(t, "state.get", p); err != nil {
			t.Errorf("state.get(%q): %v", p, err)
		}
	}
	for _, p := range []string{`{"a":1}`, `{ "a" : null }`, `[]`, `{} {}`, `1`} {
		if _, err := r.call(t, "state.get", p); err == nil {
			t.Errorf("state.get(%q) should be rejected", p)
		}
	}
}

// Contract addition: capture identity, editor.reset, ignore_conflicts.
func TestCaptureIDsAreMonotonicCarriedByEventsAndGateStop(t *testing.T) {
	r := newRig(t, script("s1", "A", "", true))
	sid, _ := openEditor(t, r, "s1")
	first := r.mustCall(t, "capture.key.start", `{"sessionId":"`+sid+`"}`)["capture_id"].(float64)
	r.cap.sink.Key("a")
	if ev := r.em.named(EvCaptureKey); len(ev) != 1 || ev[0].payload.(keyEvent).CaptureID != uint64(first) {
		t.Fatalf("capture.key must carry capture_id %v: %v", first, ev)
	}
	second := r.mustCall(t, "capture.mouse.start", `{"sessionId":"`+sid+`"}`)["capture_id"].(float64)
	if second <= first {
		t.Fatalf("capture_id must increase: %v then %v", first, second)
	}
	ended := r.em.named(EvCaptureEnded)
	if len(ended) != 1 || ended[0].payload.(endedEvent).CaptureID != uint64(first) || ended[0].payload.(endedEvent).Reason != EndStopped {
		t.Fatalf("replacing must end the first capture by id: %v", ended)
	}
	r.cap.sink.Mouse(1, 2)
	if ev := r.em.named(EvCaptureMouse); len(ev) != 1 || ev[0].payload.(mouseEvent).CaptureID != uint64(second) {
		t.Errorf("capture.mouse id: %v", ev)
	}
	r.mustCall(t, "capture.stop", `{"sessionId":"`+sid+`","capture_id":`+mustJSON(first)+`}`) // stale: no-op
	if mode, _ := r.b.capture.Active(); mode != CaptureMouse {
		t.Fatal("a stale capture_id must not stop the current capture")
	}
	r.mustCall(t, "capture.stop", `{"sessionId":"`+sid+`","capture_id":`+mustJSON(second)+`}`)
	if mode, _ := r.b.capture.Active(); mode != CaptureNone {
		t.Fatal("the current capture_id must stop it")
	}
	if got := r.em.named(EvCaptureEnded); len(got) != 2 || got[1].payload.(endedEvent).CaptureID != uint64(second) {
		t.Errorf("ended = %v", got)
	}
	third := r.mustCall(t, "capture.hotkey.start", `{"sessionId":"`+sid+`","field":"start"}`)["capture_id"].(float64)
	r.cap.sink.Hotkey(model.Hotkey{Key: "f5"})
	if ev := r.em.named(EvCaptureHotkey); len(ev) != 1 || ev[0].payload.(hotkeyEvent).CaptureID != uint64(third) {
		t.Errorf("capture.hotkey id: %v", ev)
	}
	r.mustCall(t, "capture.stop", `{"sessionId":"`+sid+`"}`) // no id: stops the current one
	if mode, _ := r.b.capture.Active(); mode != CaptureNone {
		t.Error("stop without capture_id must stop")
	}
}

func TestResetSessionAlwaysEmitsEditorResetAfterClearing(t *testing.T) {
	r := newRig(t, script("s1", "A", "", true))
	var sessionAtEmit = "unset"
	r.em.onEmit = func(name string) {
		if name == EvEditorReset {
			r.b.mu.Lock()
			if r.b.session == nil {
				sessionAtEmit = "cleared"
			} else {
				sessionAtEmit = "open"
			}
			r.b.mu.Unlock()
		}
	}
	r.b.ResetSession("navigation") // nothing open, nothing capturing
	ev := r.em.named(EvEditorReset)
	if len(ev) != 1 || ev[0].payload.(map[string]string)["reason"] != "navigation" || ev[0].payload.(map[string]string)["session_id"] != "" {
		t.Fatalf("editor.reset without session: %v", ev)
	}
	if len(r.em.named(EvCaptureEnded)) != 0 {
		t.Error("no capture.ended without a capture")
	}

	sid, _ := openEditor(t, r, "s1")
	r.mustCall(t, "capture.key.start", `{"sessionId":"`+sid+`"}`)
	r.b.ResetSession("processFailed")
	ev = r.em.named(EvEditorReset)
	if len(ev) != 2 || ev[1].payload.(map[string]string)["reason"] != "processFailed" || ev[1].payload.(map[string]string)["session_id"] != sid {
		t.Fatalf("editor.reset with session: %v", ev)
	}
	if got := r.em.named(EvCaptureEnded); len(got) != 1 || got[0].payload.(endedEvent).Reason != EndEditorClosed {
		t.Errorf("capture.ended keeps its real reason: %v", got)
	}
	if sessionAtEmit != "cleared" {
		t.Errorf("editor.reset must be emitted after the session is cleared, saw %s", sessionAtEmit)
	}
}

func TestSaveIgnoreConflictsKeepsDuplicateHotkeysAndTouchesNoOtherScript(t *testing.T) {
	s1 := script("s1", "Alpha", "", true)
	s1.ToggleHotkey = &model.Hotkey{Modifiers: []string{"ctrl"}, Key: "f5"}
	r := newRig(t, s1, script("s2", "Beta", "", true))
	sid, edit := openEditor(t, r, "s2")
	edit["toggle_hotkey"] = map[string]any{"modifiers": []string{"ctrl"}, "key": "f5"}
	body := func(extra string) string {
		b, _ := json.Marshal(map[string]any{"sessionId": sid, "script": edit})
		return string(b[:len(b)-1]) + extra + "}"
	}
	_, err := r.call(t, "editor.save", body(""))
	wantCode(t, err, CodeHotkeyConflict)
	r.mustCall(t, "editor.save", body(`,"ignore_conflicts":true`))
	calls := r.store.calls()
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0].ID != "s2" || calls[0][0].ToggleHotkey == nil {
		t.Fatalf("only the draft may be saved, hotkey kept: %+v", calls)
	}
	if got := r.mustCall(t, "scripts.get", `{"id":"s1"}`); got["toggle_hotkey"] == nil {
		t.Error("the other script's hotkey must not be cleared")
	}
}
