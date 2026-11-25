package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

func readFrontend(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "frontend", "src", "api", rel))
	if err != nil {
		t.Skipf("frontend file %s not available: %v", rel, err)
	}
	return data
}

// parseTypesBlock returns the names declared inside `export interface <iface> {` in a types.ts
// source; CRLF and LF checkouts parse identically.
func parseTypesBlock(src, iface string) ([]string, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	start := strings.Index(src, "export interface "+iface+" {")
	if start < 0 {
		return nil, fmt.Errorf("interface %s not found in types.ts", iface)
	}
	body := src[start:]
	end := strings.Index(body, "\n}")
	if end < 0 {
		return nil, fmt.Errorf("interface %s has no closing brace", iface)
	}
	re := regexp.MustCompile(`(?m)^  '?([A-Za-z.]+)'?:`)
	var names []string
	for _, m := range re.FindAllStringSubmatch(body[:end], -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("interface %s is empty", iface)
	}
	return names, nil
}

func typesBlock(t *testing.T, iface string) []string {
	t.Helper()
	names, err := parseTypesBlock(string(readFrontend(t, "types.ts")), iface)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestTypesParserIsLineEndingAgnostic(t *testing.T) {
	src := string(readFrontend(t, "types.ts"))
	lf, err := parseTypesBlock(src, "Methods")
	if err != nil {
		t.Fatal(err)
	}
	crlf, err := parseTypesBlock(strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\n", "\r\n"), "Methods")
	if err != nil || !reflect.DeepEqual(lf, crlf) {
		t.Fatalf("CRLF parse differs: %v / %v", err, crlf)
	}
	if _, err := parseTypesBlock("export interface X {\n a: 1\n}\n", "Missing"); err == nil {
		t.Error("missing interface must be an error, not a panic")
	}
}

func TestEveryContractMethodHasHandler(t *testing.T) {
	r := newRig(t)
	for _, name := range typesBlock(t, "Methods") {
		if _, ok := r.m[name]; !ok {
			t.Errorf("types.ts method %s has no handler", name)
		}
	}
	for _, name := range []string{"window.show", "window.reportBounds", "scripts.get", "actions.catalog", "capture.mouse.stop"} {
		if _, ok := r.m[name]; !ok {
			t.Errorf("§9.1 method %s has no handler", name)
		}
	}
}

func TestEveryContractEventHasEmitterName(t *testing.T) {
	have := map[string]bool{}
	for _, n := range []string{EvState, EvScriptsChanged, EvSettingsChanged, EvSaveFailed, EvCaptureHotkey, EvCaptureKey,
		EvCaptureMouse, EvCaptureEnded, EvEditorReset, EvUpdateAvailable, EvUpdateProgress, EvUpdateFailed, EvNavGoto, EvAppClosing} {
		have[n] = true
	}
	for _, name := range typesBlock(t, "Events") {
		if !have[name] {
			t.Errorf("types.ts event %s has no emitter constant", name)
		}
	}
}

func dropNulls(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if e == nil {
				delete(x, k)
			} else {
				x[k] = dropNulls(e)
			}
		}
	case []any:
		for i := range x {
			x[i] = dropNulls(x[i])
		}
	}
	return v
}

func roundTrip[T any](t *testing.T, name string, raw []byte) {
	t.Helper()
	var dto T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&dto); err != nil {
		t.Errorf("%s: fixture does not decode into the Go DTO: %v", name, err)
		return
	}
	out, _ := json.Marshal(dto)
	var a, b any
	_ = json.Unmarshal(raw, &a)
	_ = json.Unmarshal(out, &b)
	if !reflect.DeepEqual(dropNulls(a), dropNulls(b)) {
		t.Errorf("%s: field set differs after round-trip\nfixture: %s\ngo:      %s", name, raw, out)
	}
}

func TestFixturesRoundTripThroughGoDTOs(t *testing.T) {
	cases := map[string]func(*testing.T, string, []byte){
		"state": roundTrip[StateDTO], "script_summary": roundTrip[ScriptSummary], "group": roundTrip[GroupDTO],
		"settings": roundTrip[SettingsDTO], "scripts_list": roundTrip[ScriptList], "boot_state": roundTrip[BootState],
		"overlay": roundTrip[OverlayDTO], "script": roundTrip[model.Script], "action_spec": roundTrip[ActionSpec],
		"editor_session": roundTrip[EditorSession], "hotkey_conflict": roundTrip[HotkeyConflict],
	}
	for name, fn := range cases {
		fn(t, name, readFrontend(t, "fixtures/"+name+".json"))
	}

	var events map[string]json.RawMessage
	if err := json.Unmarshal(readFrontend(t, "fixtures/events.json"), &events); err != nil {
		t.Fatal(err)
	}
	typed := map[string]func(*testing.T, string, []byte){
		"state": roundTrip[StateDTO], "scripts.changed": roundTrip[scriptsChangedEvent],
		"settings.changed": roundTrip[settingsChangedEvent], "save.failed": roundTrip[saveFailedEvent],
		"nav.goto": roundTrip[navGotoEvent], "app.closing": roundTrip[appClosingEvent],
		"capture.hotkey": roundTrip[hotkeyEvent], "capture.key": roundTrip[keyEvent],
		"capture.mouse": roundTrip[mouseEvent], "capture.ended": roundTrip[endedEvent],
		"update.available": roundTrip[updateAvailableEvent], "update.progress": roundTrip[updateProgressEvent],
		"update.failed": roundTrip[updateFailedEvent], "editor.reset": roundTrip[editorResetEvent],
	}
	for name, raw := range events {
		fn, ok := typed[name]
		if !ok {
			t.Errorf("fixture has event %s unknown to the bridge", name)
			continue
		}
		fn(t, "event:"+name, raw)
	}
	for name := range typed {
		if _, ok := events[name]; !ok {
			t.Logf("fixture events.json has no %s sample", name)
		}
	}
}

func TestEmittedPayloadsMatchEventFixtureKeys(t *testing.T) {
	var events map[string]map[string]any
	if err := json.Unmarshal(readFrontend(t, "fixtures/events.json"), &events); err != nil {
		t.Fatal(err)
	}
	keys := func(v any) []string {
		data, _ := json.Marshal(v)
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	fixKeys := func(name string) []string {
		var ks []string
		for k := range events[name] {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	r := newRig(t, script("s1", "A", "g", false))
	r.cap.err = nil
	r.store.failIDs["s1"] = true
	r.mustCall(t, "scripts.setEnabled", `{"id":"s1","enabled":true}`)
	if ev := r.em.named(EvSaveFailed); len(ev) != 1 || !reflect.DeepEqual(keys(ev[0].payload), fixKeys("save.failed")) {
		t.Errorf("save.failed payload keys mismatch: %v", ev)
	}
	r.mustCall(t, "scripts.setGroupEnabled", `{"group":"g","enabled":false}`)
	if ev := r.em.named(EvScriptsChanged); len(ev) != 1 || !reflect.DeepEqual(keys(ev[0].payload), fixKeys("scripts.changed")) {
		t.Errorf("scripts.changed payload keys mismatch: %v", ev)
	}
	r.b.NavGoto("settings")
	if ev := r.em.named(EvNavGoto); len(ev) != 1 || !reflect.DeepEqual(keys(ev[0].payload), fixKeys("nav.goto")) {
		t.Errorf("nav.goto payload keys mismatch")
	}
}

func TestStateDTOCarriesLocksAndRunningName(t *testing.T) {
	r := newRig(t)
	st := r.mustCall(t, "state.get", "")
	if st["mouse_locked"] != true || st["keyboard_locked"] != false {
		t.Errorf("lock state not reported: %v", st)
	}
	if st["theme"] != "system" || st["global_active"] != true {
		t.Errorf("unexpected defaults: %v", st)
	}
	st = r.mustCall(t, "app.setGlobalActive", `{"active":false}`)
	if st["global_active"] != false {
		t.Errorf("global active not applied: %v", st)
	}
	if ev := r.em.named(EvState); len(ev) != 1 || ev[0].payload.(StateDTO).GlobalActive {
		t.Errorf("hub snapshot must reach the page as a state event, got %v", ev)
	}
}

func TestGroupToggleSavesOnlyChangedScriptsInOneQueueSave(t *testing.T) {
	r := newRig(t,
		script("a", "A", "farm", true), script("b", "B", "farm", false), script("c", "C", "farm", false),
		script("d", "D", "other", false), script("e", "E", "", false))
	res := r.mustCall(t, "scripts.setGroupEnabled", `{"group":"farm","enabled":true}`)
	if res["changed"] != float64(2) {
		t.Fatalf("changed = %v, want 2", res["changed"])
	}
	calls := r.store.calls()
	if len(calls) != 1 {
		t.Fatalf("want exactly one QueueSave call, got %d", len(calls))
	}
	var ids []string
	for _, s := range calls[0] {
		ids = append(ids, s.ID)
		if !s.Enabled {
			t.Errorf("script %s queued but not enabled", s.ID)
		}
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"b", "c"}) {
		t.Errorf("queued ids = %v, want [b c]", ids)
	}
	r.mustCall(t, "scripts.setGroupEnabled", `{"group":"farm","enabled":true}`)
	if len(r.store.calls()) != 1 {
		t.Errorf("a no-op group toggle must not queue a save")
	}
	_, err := r.call(t, "scripts.setGroupEnabled", `{"group":"nope","enabled":true}`)
	wantCode(t, err, CodeNotFound)
	_, err = r.call(t, "scripts.setGroupEnabled", `{"group":"","enabled":true}`)
	wantCode(t, err, CodeBadRequest)
}

func TestSaveFailureEmitsSaveFailedAndRetryRequeues(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", false))
	r.store.failIDs["s1"] = true
	r.mustCall(t, "scripts.setEnabled", `{"id":"s1","enabled":true}`)
	ev := r.em.named(EvSaveFailed)
	if len(ev) != 1 {
		t.Fatalf("want one save.failed, got %d", len(ev))
	}
	p := ev[0].payload.(saveFailedEvent)
	if p != (saveFailedEvent{ScriptID: "s1", Name: "Alpha", Message: "could not write script file"}) {
		t.Errorf("payload = %v", p)
	}
	delete(r.store.failIDs, "s1")
	r.mustCall(t, "scripts.retrySave", `{"id":"s1"}`)
	if len(r.store.calls()) != 2 {
		t.Errorf("retrySave must re-queue the script")
	}
	_, err := r.call(t, "scripts.retrySave", `{"id":"zzz"}`)
	wantCode(t, err, CodeNotFound)
}

func openEditor(t *testing.T, r *rig, id string) (string, map[string]any) {
	t.Helper()
	params := `{"id":null}`
	if id != "" {
		params = `{"id":"` + id + `"}`
	}
	res := r.mustCall(t, "editor.open", params)
	return res["sessionId"].(string), res["script"].(map[string]any)
}

func TestEditorOpenCloseTogglesSuppressionAndStopsCapture(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	sid, _ := openEditor(t, r, "s1")
	if !r.ctl.Snapshot().EditDialogOpen {
		t.Fatal("editor.open must set edit-dialog-open")
	}
	_, err := r.call(t, "editor.open", `{"id":"s1"}`)
	wantCode(t, err, CodeEditorBusy)

	r.mustCall(t, "capture.hotkey.start", `{"sessionId":"`+sid+`","field":"toggle"}`)
	res := r.mustCall(t, "editor.close", `{"sessionId":"`+sid+`","autosave":false}`)
	if res["saved"] != false {
		t.Errorf("unexpected saved: %v", res)
	}
	if r.ctl.Snapshot().EditDialogOpen {
		t.Error("editor.close must clear edit-dialog-open")
	}
	ended := r.em.named(EvCaptureEnded)
	if len(ended) != 1 || ended[0].payload.(endedEvent).Reason != EndEditorClosed {
		t.Errorf("capture.ended = %v", ended)
	}
	r.cap.mu.Lock()
	stops := r.cap.stops
	r.cap.mu.Unlock()
	if stops == 0 {
		t.Error("capture backend hook was not removed")
	}
	_, err = r.call(t, "editor.close", `{"sessionId":"`+sid+`","autosave":false}`)
	wantCode(t, err, CodeEditorClosed)
	openEditor(t, r, "s1")
}

func TestShutdownClearsEditorOpenAndRunsOwnedSteps(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	sid, _ := openEditor(t, r, "s1")
	r.mustCall(t, "capture.key.start", `{"sessionId":"`+sid+`"}`)
	r.ov.mu.Lock()
	r.ov.state.X, r.ov.state.Y = 77, 88
	r.ov.mu.Unlock()
	if err := r.b.Shutdown(time.Second); err != nil {
		t.Fatal(err)
	}
	if r.ctl.Snapshot().EditDialogOpen {
		t.Error("Shutdown must force edit-dialog-open false")
	}
	if len(r.em.named(EvAppClosing)) != 1 || len(r.em.named(EvCaptureEnded)) != 1 {
		t.Errorf("expected app.closing and capture.ended")
	}
	if got := r.cfg.Get().OverlayPosition; got.X != 77 || got.Y != 88 {
		t.Errorf("overlay position not persisted: %+v", got)
	}
	r.auto.mu.Lock()
	log := r.auto.log
	r.auto.mu.Unlock()
	if !reflect.DeepEqual(log, []string{"stop"}) || r.store.flushed != 1 {
		t.Errorf("auto-active stop=%v flushed=%d", log, r.store.flushed)
	}
	_, err := r.call(t, "editor.open", `{"id":null}`)
	wantCode(t, err, CodeBusy)
	r.store.flushOK = false
	r.b.shutdownOnce = sync_Once()
	if r.b.Shutdown(time.Millisecond) == nil {
		t.Error("a flush timeout must be reported")
	}
}

func TestEditorSaveValidationAndConflicts(t *testing.T) {
	s1 := script("s1", "Alpha", "", true)
	s1.ToggleHotkey = &model.Hotkey{Modifiers: []string{"ctrl"}, Key: "f5"}
	s2 := script("s2", "Beta", "", true)
	r := newRig(t, s1, s2)
	sid, edit := openEditor(t, r, "s2")
	edit["toggle_hotkey"] = map[string]any{"modifiers": []string{"CTRL"}, "key": "F5"}
	save := func(name string, override bool) (map[string]any, error) {
		edit["name"] = name
		body, _ := json.Marshal(map[string]any{"sessionId": sid, "script": edit, "overrideConflicts": override})
		return r.call(t, "editor.save", string(body))
	}

	_, err := save("   ", false)
	wantCode(t, err, CodeNameEmpty)
	_, err = save("!!!", false)
	wantCode(t, err, CodeNameEmpty)
	_, err = save("alpha", false)
	wantCode(t, err, CodeNameTaken)
	_, err = save("Alpha", true)
	wantCode(t, err, CodeNameTaken)
	e := func() *Error { _, err := save("Beta", false); return wantCode(t, err, CodeHotkeyConflict) }()
	if len(e.Conflicts) != 1 || e.Conflicts[0].ScriptID != "s1" || e.Conflicts[0].Field != "toggle_hotkey" {
		t.Errorf("conflicts = %+v", e.Conflicts)
	}
	if len(r.store.calls()) != 0 {
		t.Fatal("nothing must be saved before conflicts are resolved")
	}
	res, err := save("Beta", true)
	if err != nil {
		t.Fatal(err)
	}
	if res["rev"] == nil {
		t.Errorf("missing rev: %v", res)
	}
	calls := r.store.calls()
	if len(calls) != 1 || len(calls[0]) != 2 {
		t.Fatalf("override must save the edited and the cleared script in ONE QueueSave, got %v", calls)
	}
	for _, s := range calls[0] {
		if s.ID == "s1" && s.ToggleHotkey != nil {
			t.Error("conflicting hotkey was not cleared on the other script")
		}
	}
	if len(r.hk.lists) == 0 {
		t.Error("hotkey set must be refreshed after a mutation")
	}
}

func TestEditorSaveFailureReportsSaveFailedAndCloseReportsReason(t *testing.T) {
	r := newRig(t)
	sid, edit := openEditor(t, r, "")
	r.store.failIDs[edit["id"].(string)] = true
	body, _ := json.Marshal(map[string]any{"sessionId": sid, "script": edit})
	_, err := r.call(t, "editor.save", string(body))
	wantCode(t, err, CodeSaveFailed)

	res := r.mustCall(t, "editor.close", `{"sessionId":"`+sid+`","autosave":true}`)
	if res["saved"] != false || res["reason"] != "could not write script file" {
		t.Errorf("close should report Not saved with a reason: %v", res)
	}
	if r.ctl.Snapshot().EditDialogOpen {
		t.Error("close must clear edit-dialog-open even when autosave fails")
	}
}

func TestEditorCloseAutosaveSavesValidDraft(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	sid, edit := openEditor(t, r, "s1")
	edit["name"] = "Alpha 2"
	body, _ := json.Marshal(map[string]any{"sessionId": sid, "autosave": true, "script": edit})
	res := r.mustCall(t, "editor.close", string(body))
	if res["saved"] != true {
		t.Fatalf("autosave should save: %v", res)
	}
	got := r.mustCall(t, "scripts.get", `{"id":"s1"}`)
	if got["name"] != "Alpha 2" {
		t.Errorf("script not updated: %v", got)
	}
}

func TestNewScriptGetsUniqueNameAndIsNotPersistedUntilSaved(t *testing.T) {
	r := newRig(t, script("s1", "New Script", "", true))
	sid, edit := openEditor(t, r, "")
	if edit["name"] != "New Script 2" {
		t.Errorf("name = %v", edit["name"])
	}
	r.mustCall(t, "editor.close", `{"sessionId":"`+sid+`","autosave":false}`)
	if len(r.store.calls()) != 0 || len(r.mustCall(t, "scripts.list", "")["scripts"].([]any)) != 1 {
		t.Error("an unsaved new script must not be persisted")
	}
}

func TestInvalidParamsAreRejected(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	bad := []struct{ method, params string }{
		{"scripts.setEnabled", ``},
		{"scripts.setEnabled", `{"id":"s1"`},
		{"scripts.setEnabled", `{"id":"s1","enabled":"yes"}`},
		{"scripts.setEnabled", `{"id":"s1","enabled":true,"extra":1}`},
		{"app.setGlobalActive", `[]`},
		{"capture.hotkey.start", `{"sessionId":"x","field":"toggle"}`},
		{"settings.update", `{"patch":{"collapsed_groups":["a"]}}`},
		{"clipboard.write", `{"text":123}`},
		{"window.reportBounds", `{"x":0,"y":0,"w":-1,"h":5}`},
	}
	for _, c := range bad {
		if _, err := r.call(t, c.method, c.params); err == nil {
			t.Errorf("%s(%s) should fail", c.method, c.params)
		}
	}
	_, err := r.call(t, "scripts.setEnabled", `{"id":"missing","enabled":true}`)
	wantCode(t, err, CodeNotFound)
	_, err = r.call(t, "scripts.delete", `{"id":"missing"}`)
	wantCode(t, err, CodeNotFound)
	_, err = r.call(t, "scripts.clone", `{"id":"missing"}`)
	wantCode(t, err, CodeNotFound)
	_, err = r.call(t, "editor.open", `{"id":"missing"}`)
	wantCode(t, err, CodeNotFound)

	sid, edit := openEditor(t, r, "s1")
	for _, mutate := range []func(){
		func() { edit["run_mode"] = "forever" },
		func() { edit["run_mode"] = "once"; edit["hotkey_mode"] = "x" },
		func() { edit["hotkey_mode"] = "toggle"; edit["actions"] = []any{map[string]any{"type": "format_disk"}} },
		func() { edit["actions"] = []any{map[string]any{"type": "delay", "ms": -5}} },
		func() {
			edit["actions"] = []any{}
			edit["toggle_hotkey"] = map[string]any{"modifiers": []string{"super"}, "key": "a"}
		},
	} {
		mutate()
		body, _ := json.Marshal(map[string]any{"sessionId": sid, "script": edit})
		_, err := r.call(t, "editor.save", string(body))
		wantCode(t, err, CodeBadRequest)
	}
	_, err = r.call(t, "editor.save", `{"sessionId":"nope","script":{}}`)
	wantCode(t, err, CodeEditorClosed)
	_, err = r.call(t, "capture.stop", `{"sessionId":"nope"}`)
	wantCode(t, err, CodeEditorClosed)
	_, err = r.call(t, "editor.fromJSON", `{"text":"{not json"}`)
	wantCode(t, err, CodeBadRequest)
	_, err = r.call(t, "capture.hotkey.start", `{"sessionId":"`+sid+`","field":"bogus"}`)
	wantCode(t, err, CodeBadRequest)
}

func TestSettingsRestartOnlyAndImmediateClassification(t *testing.T) {
	r := newRig(t)
	dir := filepath.Join(t.TempDir(), "new-scripts")
	body, _ := json.Marshal(map[string]any{"patch": map[string]any{
		"key_press_duration_ms": 25, "scripts_dir": dir, "theme": "dark", "global_hotkey": "Ctrl + G",
	}})
	res := r.mustCall(t, "settings.update", string(body))
	restart := toStrings(res["restartRequired"])
	applied := toStrings(res["applied"])
	sort.Strings(restart)
	sort.Strings(applied)
	if !reflect.DeepEqual(restart, []string{"key_press_duration_ms", "scripts_dir"}) {
		t.Errorf("restartRequired = %v", restart)
	}
	if !reflect.DeepEqual(applied, []string{"global_hotkey", "theme"}) {
		t.Errorf("applied = %v", applied)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("creatable scripts_dir should have been created: %v", err)
	}
	cfg := r.cfg.Get()
	if cfg.KeyPressDurationMs != 25 || cfg.Theme != "dark" || cfg.GlobalHotkey.Key != "g" {
		t.Errorf("config not persisted: %+v", cfg)
	}
	if res["state"].(map[string]any)["theme"] != "dark" {
		t.Errorf("state must reflect new theme")
	}
	if len(r.em.named(EvSettingsChanged)) != 1 {
		t.Errorf("settings.changed not emitted")
	}
	res = r.mustCall(t, "settings.update", `{"patch":{"theme":"dark"}}`)
	if len(toStrings(res["applied"]))+len(toStrings(res["restartRequired"])) != 0 {
		t.Errorf("no-op patch should report nothing: %v", res)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, e := range v.([]any) {
		out = append(out, e.(string))
	}
	return out
}

func TestSettingsValidationAndRollback(t *testing.T) {
	r := newRig(t)
	file := filepath.Join(t.TempDir(), "afile")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	for field, patch := range map[string]string{
		"theme":                    `{"theme":"neon"}`,
		"global_hotkey":            `{"global_hotkey":"Ctrl + + X"}`,
		"overlay_hotkey":           `{"overlay_hotkey":"Ctrl + O", "capture_hotkey":"Ctrl + O"}`,
		"key_press_duration_ms":    `{"key_press_duration_ms":0}`,
		"scripts_dir":              `{"scripts_dir":"` + strings.ReplaceAll(file, `\`, `\\`) + `"}`,
		"auto_active_process_name": `{"auto_active_process_name":"..\\evil.exe"}`,
	} {
		_, err := r.call(t, "settings.update", `{"patch":`+patch+`}`)
		e := wantCode(t, err, CodeInvalidSetting)
		if field == "overlay_hotkey" {
			field = "capture_hotkey"
		}
		if e.Field != field {
			t.Errorf("field = %q, want %q", e.Field, field)
		}
	}
	_, err := r.call(t, "settings.update", `{"patch":{"scripts_dir":""}}`)
	wantCode(t, err, CodeInvalidSetting)

	r.host.failKind = HotkeyOverlay
	before := r.cfg.Get()
	_, err = r.call(t, "settings.update", `{"patch":{"global_hotkey":"Ctrl + G","overlay_hotkey":"Ctrl + H"}}`)
	wantCode(t, err, CodeHotkeyRegisterFail)
	if !reflect.DeepEqual(r.cfg.Get(), before) {
		t.Error("failed hotkey registration must not persist anything")
	}
	r.host.mu.Lock()
	rb := strings.Join(r.host.rebinds, ",")
	r.host.mu.Unlock()
	if !strings.Contains(rb, "global=Ctrl + G") || !strings.HasSuffix(rb, "global=RCtrl") {
		t.Errorf("applied hotkey must be rolled back, rebinds = %s", rb)
	}

	r.host.failKind = ""
	r.per.fail = true
	_, err = r.call(t, "settings.update", `{"patch":{"auto_active_enabled":true,"auto_active_process_name":"game.exe"}}`)
	wantCode(t, err, CodeSaveFailed)
	r.auto.mu.Lock()
	log := append([]string(nil), r.auto.log...)
	r.auto.mu.Unlock()
	if !reflect.DeepEqual(log, []string{"start", "stop"}) {
		t.Errorf("auto-active must be rolled back on persist failure, got %v", log)
	}
}

func TestOverlayAutoActiveAndCollapsedGroups(t *testing.T) {
	r := newRig(t, script("s1", "A", "farm", true))
	res := r.mustCall(t, "overlay.set", `{"visible":false,"draggable":true}`)
	if res["visible"] != false || res["draggable"] != true {
		t.Errorf("overlay = %v", res)
	}
	r.mustCall(t, "overlay.set", `{"enabled":false}`)
	if r.cfg.Get().OverlayEnabled {
		t.Error("overlay_enabled not persisted")
	}
	r.ov.err = os_ErrInvalid()
	_, err := r.call(t, "overlay.set", `{"enabled":true}`)
	wantCode(t, err, CodeInternal)

	ad := r.mustCall(t, "autoactive.set", `{"enabled":true,"processName":" game.exe "}`)
	if ad["enabled"] != true || ad["processName"] != "game.exe" {
		t.Errorf("autoactive = %v", ad)
	}
	r.auto.mu.Lock()
	if !reflect.DeepEqual(r.auto.log, []string{"start"}) || !reflect.DeepEqual(r.auto.names, []string{"game.exe"}) {
		t.Errorf("watcher calls = %v %v", r.auto.log, r.auto.names)
	}
	r.auto.mu.Unlock()
	_, err = r.call(t, "autoactive.set", `{"processName":"a/b.exe"}`)
	wantCode(t, err, CodeInvalidSetting)

	r.mustCall(t, "scripts.setGroupCollapsed", `{"group":"farm","collapsed":true}`)
	list := r.mustCall(t, "scripts.list", "")
	if list["groups"].([]any)[0].(map[string]any)["collapsed"] != true {
		t.Errorf("collapsed state not reflected in list: %v", list)
	}
	r.mustCall(t, "scripts.setGroupCollapsed", `{"group":"farm","collapsed":false}`)
	if len(r.cfg.Get().CollapsedGroups) != 0 {
		t.Error("collapsed group not removed")
	}
}

func TestListSortCloneDeleteAndLabels(t *testing.T) {
	s1 := script("s1", "zeta", "", true)
	s2 := script("s2", "Alpha", "Beta", true)
	s3 := script("s3", "alpha", "", false)
	s3.HotkeyMode = "separate"
	s3.StartHotkey = &model.Hotkey{Modifiers: []string{"ctrl"}, Key: "f1"}
	s3.StopHotkey = &model.Hotkey{Modifiers: []string{}, Key: "f2"}
	r := newRig(t, s1, s2, s3)
	list := r.mustCall(t, "scripts.list", "")
	var names []string
	for _, s := range list["scripts"].([]any) {
		names = append(names, s.(map[string]any)["name"].(string))
	}
	if !reflect.DeepEqual(names, []string{"alpha", "zeta", "Alpha"}) {
		t.Errorf("order = %v", names)
	}
	if lbl := list["scripts"].([]any)[0].(map[string]any)["hotkey_label"]; lbl != "Ctrl + F1 / F2" {
		t.Errorf("label = %v", lbl)
	}

	r.store.taken["zeta (copy)"] = true
	cl := r.mustCall(t, "scripts.clone", `{"id":"s1"}`)
	if cl["name"] != "zeta (copy 2)" || cl["enabled"] != false {
		t.Errorf("clone = %v", cl)
	}
	if len(r.store.calls()) != 1 {
		t.Error("clone must be saved with one QueueSave")
	}
	res := r.mustCall(t, "scripts.delete", `{"id":"s1"}`)
	if res["rev"] == nil || len(r.store.deleted) != 1 {
		t.Errorf("delete = %v %v", res, r.store.deleted)
	}
	_, err := r.call(t, "scripts.get", `{"id":"s1"}`)
	wantCode(t, err, CodeNotFound)
}

func TestMiscHandlers(t *testing.T) {
	r := newRig(t)
	if got := r.mustCall(t, "app.getBootState", ""); got["mode"] != "main" || got["macs"] == nil {
		t.Errorf("boot = %v", got)
	}
	r.mustCall(t, "app.ready", "")
	r.mustCall(t, "window.hide", "")
	r.mustCall(t, "app.exit", "")
	r.mustCall(t, "clipboard.write", `{"text":"hello"}`)
	r.mustCall(t, "window.reportBounds", `{"x":5,"y":6,"w":700,"h":500}`)
	if r.host.hidden != 1 || r.host.exited != 1 || r.host.clip != "hello" {
		t.Errorf("host calls: %+v", r.host)
	}
	if got := r.cfg.Get().MainWindowPos; got.X != 5 || got.Width != 700 {
		t.Errorf("bounds = %+v", got)
	}
	r.per.mu.Lock()
	n := r.per.n
	r.per.mu.Unlock()
	r.mustCall(t, "window.reportBounds", `{"x":5,"y":6,"w":700,"h":500}`)
	r.per.mu.Lock()
	same := r.per.n == n
	r.per.mu.Unlock()
	if !same {
		t.Error("unchanged bounds must not rewrite the config")
	}
	cat := r.mustCall(t, "actions.catalog", "")
	if cat["_raw"] == nil || !strings.Contains(cat["_raw"].(string), `"wait_for_image"`) {
		t.Errorf("catalog = %v", cat)
	}
	d := r.mustCall(t, "editor.describeActions", `{"actions":[{"type":"mouse_move","x":1,"y":2},{"type":"loop","count":0}]}`)
	var labels []string
	_ = json.Unmarshal([]byte(d["_raw"].(string)), &labels)
	if !reflect.DeepEqual(labels, []string{"Mouse Move -> (1, 2)", "Loop (infinite)"}) {
		t.Errorf("describe = %v", labels)
	}
	txt := r.mustCall(t, "editor.toJSON", `{"script":{"id":"x","name":"N","enabled":true,"run_mode":"once","hotkey_mode":"toggle","actions":[]}}`)
	back := r.mustCall(t, "editor.fromJSON", mustJSON(map[string]any{"text": txt["text"]}))
	if back["script"].(map[string]any)["name"] != "N" {
		t.Errorf("fromJSON = %v", back)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestUpdateApplyRunsOnceAndReportsProgressAndFailure(t *testing.T) {
	r := newRig(t)
	r.host.release = make(chan struct{})
	r.host.updateErr = os_ErrInvalid()
	r.mustCall(t, "update.apply", "")
	_, err := r.call(t, "update.apply", "")
	wantCode(t, err, CodeBusy)
	close(r.host.release)
	deadline := time.Now().Add(2 * time.Second)
	for len(r.em.named(EvUpdateFailed)) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(r.em.named(EvUpdateFailed)) != 1 || len(r.em.named(EvUpdateProgress)) != 1 {
		t.Errorf("events: failed=%d progress=%d", len(r.em.named(EvUpdateFailed)), len(r.em.named(EvUpdateProgress)))
	}
	if p := r.em.named(EvUpdateFailed)[0].payload.(updateFailedEvent).Message; p != "update failed" {
		t.Errorf("update.failed must carry a fixed message, got %q", p)
	}
	r.mustCall(t, "update.skip", "")
	r.b.NotifyUpdateAvailable("v2")
	if len(r.em.named(EvUpdateAvailable)) != 1 {
		t.Error("update.available not emitted")
	}
}

func noisyPNGBase64(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	rng := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Intn(256))
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestEditorSaveAcceptsRealSizeImageTemplateAndRejectsAbsurd(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	sid, edit := openEditor(t, r, "s1")
	save := func(img string) error {
		edit["actions"] = []map[string]any{{"type": "wait_for_image", "image": img}}
		body, _ := json.Marshal(map[string]any{"sessionId": sid, "script": edit})
		_, err := r.call(t, "editor.save", string(body))
		return err
	}
	real := noisyPNGBase64(t)
	if len(real) <= 1024 {
		t.Fatalf("test image too small: %d", len(real))
	}
	if err := save(real); err != nil {
		t.Fatalf("real-size template rejected: %v", err)
	}
	wantCode(t, save(strings.Repeat("A", maxImageBytes+1)), CodeBadRequest)
}
