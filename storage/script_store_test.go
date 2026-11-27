package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

func TestCreateAndGetScript(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	script := model.Script{
		ID:      "test-id-1",
		Name:    "Test Script",
		Enabled: true,
		RunMode: model.RunModeOnce,
		Actions: []model.Action{
			{Type: model.ActionDelay, DelayMs: 100},
		},
	}

	err := scriptStore.Save(script)
	if err != nil {
		t.Fatalf("failed to save script: %v", err)
	}

	loaded, err := getByID(scriptStore, "test-id-1")
	if err != nil {
		t.Fatalf("failed to get script: %v", err)
	}
	if loaded.Name != "Test Script" {
		t.Errorf("expected name 'Test Script', got '%s'", loaded.Name)
	}
	if len(loaded.Actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(loaded.Actions))
	}
}

func TestListScripts(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	for i, name := range []string{"Alpha", "Beta"} {
		script := model.Script{
			ID:   "id-" + string(rune('a'+i)),
			Name: name,
		}
		if err := scriptStore.Save(script); err != nil {
			t.Fatalf("failed to save script %s: %v", name, err)
		}
	}

	allScripts, err := scriptStore.List()
	if err != nil {
		t.Fatalf("failed to list scripts: %v", err)
	}
	if len(allScripts) != 2 {
		t.Errorf("expected 2 scripts, got %d", len(allScripts))
	}
}

func TestDeleteScript(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	script := model.Script{ID: "del-1", Name: "Delete Me"}
	if err := scriptStore.Save(script); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	err := scriptStore.Delete("del-1")
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	_, err = getByID(scriptStore, "del-1")
	if err == nil {
		t.Error("expected error getting deleted script")
	}
}

func TestGetScriptNotFound(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	_, err := getByID(scriptStore, "nonexistent")
	if err == nil {
		t.Error("expected error for missing script")
	}
}

func TestScriptFilePath(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	expected := filepath.Join(tempDir, "test_id.json")
	actual := scriptStore.scriptFilePath("test-id")
	if actual != expected {
		t.Errorf("expected path %s, got %s", expected, actual)
	}
}

func getByID(scriptStore *ScriptStore, id string) (model.Script, error) {
	all, err := scriptStore.List()
	if err != nil {
		return model.Script{}, err
	}
	for _, s := range all {
		if s.ID == id {
			return s, nil
		}
	}
	return model.Script{}, fmt.Errorf("script %s not found", id)
}

func TestSaveDoesNotRescanDirectory(t *testing.T) {
	dir := t.TempDir()
	store := NewScriptStore(dir)
	for i := 0; i < 20; i++ {
		if err := store.Save(model.Script{ID: fmt.Sprint("id", i), Name: fmt.Sprint("s", i)}); err != nil {
			t.Fatal(err)
		}
	}
	var reads int32
	orig := readFile
	readFile = func(p string) ([]byte, error) { atomic.AddInt32(&reads, 1); return orig(p) }
	defer func() { readFile = orig }()
	for i := 0; i < 20; i++ {
		if err := store.Save(model.Script{ID: fmt.Sprint("id", i), Name: fmt.Sprint("s", i), Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 0 {
		t.Errorf("expected 0 file reads during Save, got %d", reads)
	}
}

func TestSaveRenameRemovesOldFile(t *testing.T) {
	dir := t.TempDir()
	store := NewScriptStore(dir)
	store.Save(model.Script{ID: "r1", Name: "Old Name"})
	store.Save(model.Script{ID: "r1", Name: "New Name"})
	if _, err := os.Stat(filepath.Join(dir, "old_name.json")); !os.IsNotExist(err) {
		t.Error("old file should be removed after rename")
	}
	if _, err := os.Stat(filepath.Join(dir, "new_name.json")); err != nil {
		t.Error("new file missing")
	}
}

func TestSaveRemovesLegacyUUIDFile(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "123e4567-e89b-12d3-a456-426614174000.json")
	os.WriteFile(legacy, []byte(`{"id":"legacy-1","name":"legacy"}`), 0644)
	store := NewScriptStore(dir)
	if err := store.Save(model.Script{ID: "legacy-1", Name: "Legacy", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("legacy file should be removed")
	}
	got, err := getByID(store, "legacy-1")
	if err != nil || !got.Enabled {
		t.Errorf("unexpected: %v %+v", err, got)
	}
}

func TestQueueSaveLastStateWins(t *testing.T) {
	dir := t.TempDir()
	store := NewScriptStore(dir)
	const n = 50
	for round := 0; round < 10; round++ {
		batch := make([]model.Script, n)
		for i := range batch {
			batch[i] = model.Script{ID: fmt.Sprint("id", i), Name: fmt.Sprint("s", i), Enabled: round%2 == 0}
		}
		store.QueueSave(batch...)
	}
	if !store.FlushSaves(10 * time.Second) {
		t.Fatal("flush timed out")
	}
	all, err := store.List()
	if err != nil || len(all) != n {
		t.Fatalf("got %d scripts, err %v", len(all), err)
	}
	for _, s := range all {
		if s.Enabled {
			t.Fatalf("%s: stale final state on disk", s.ID)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != n {
		t.Errorf("expected %d files, got %d", n, len(entries))
	}
}

func TestConcurrentSaves(t *testing.T) {
	store := NewScriptStore(t.TempDir())
	done := make(chan struct{})
	for i := 0; i < 30; i++ {
		go func(i int) {
			store.Save(model.Script{ID: fmt.Sprint("id", i), Name: fmt.Sprint("s", i)})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 30; i++ {
		<-done
	}
	if all, _ := store.List(); len(all) != 30 {
		t.Errorf("expected 30, got %d", len(all))
	}
}

// stallWorker makes the queue worker block after popping the given ID until release is called.
func stallWorker(t *testing.T, id string) (popped <-chan struct{}, release func()) {
	t.Helper()
	poppedCh := make(chan struct{})
	gate := make(chan struct{})
	var once sync.Once
	beforeQueuedWrite = func(got string) {
		if got == id {
			once.Do(func() { close(poppedCh) })
			<-gate
		}
	}
	var relOnce sync.Once
	rel := func() { relOnce.Do(func() { close(gate) }) }
	t.Cleanup(func() { rel(); beforeQueuedWrite = nil })
	return poppedCh, rel
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func TestDeleteCancelsPendingQueuedSave(t *testing.T) {
	dir := t.TempDir()
	store := NewScriptStore(dir)
	store.Save(model.Script{ID: "a", Name: "A"})
	store.Save(model.Script{ID: "b", Name: "B"})
	popped, release := stallWorker(t, "a")
	store.QueueSave(model.Script{ID: "a", Name: "A", Enabled: true}, model.Script{ID: "b", Name: "B", Enabled: true})
	<-popped
	if err := store.Delete("b"); err != nil {
		t.Fatal(err)
	}
	release()
	if !store.FlushSaves(10 * time.Second) {
		t.Fatal("flush timed out")
	}
	if fileExists(dir, "b.json") || fileExists(dir, ".json") {
		t.Error("deleted pending script was written")
	}
	all, _ := store.List()
	if len(all) != 1 || all[0].ID != "a" || !all[0].Enabled {
		t.Errorf("unexpected scripts: %+v", all)
	}
}

func TestDeleteCancelsInFlightQueuedSave(t *testing.T) {
	dir := t.TempDir()
	store := NewScriptStore(dir)
	store.Save(model.Script{ID: "a", Name: "A"})
	popped, release := stallWorker(t, "a")
	store.QueueSave(model.Script{ID: "a", Name: "A", Enabled: true})
	<-popped
	if err := store.Delete("a"); err != nil {
		t.Fatal(err)
	}
	release()
	if !store.FlushSaves(10 * time.Second) {
		t.Fatal("flush timed out")
	}
	if all, _ := store.List(); len(all) != 0 {
		t.Errorf("deleted script resurrected: %+v", all)
	}
}

func TestSaveSupersedesInFlightQueuedSave(t *testing.T) {
	dir := t.TempDir()
	store := NewScriptStore(dir)
	store.Save(model.Script{ID: "a", Name: "Old"})
	popped, release := stallWorker(t, "a")
	store.QueueSave(model.Script{ID: "a", Name: "Old", Enabled: true})
	<-popped
	store.Save(model.Script{ID: "a", Name: "Newer"})
	release()
	if !store.FlushSaves(10 * time.Second) {
		t.Fatal("flush timed out")
	}
	all, _ := store.List()
	if len(all) != 1 || all[0].Name != "Newer" || all[0].Enabled || !fileExists(dir, "newer.json") || fileExists(dir, "old.json") {
		t.Errorf("stale queued snapshot won: %+v", all)
	}
}

func TestSaveSupersedesPendingQueuedSave(t *testing.T) {
	dir := t.TempDir()
	store := NewScriptStore(dir)
	store.Save(model.Script{ID: "a", Name: "A"})
	store.Save(model.Script{ID: "b", Name: "Old"})
	popped, release := stallWorker(t, "a")
	store.QueueSave(model.Script{ID: "a", Name: "A", Enabled: true}, model.Script{ID: "b", Name: "Old", Enabled: true})
	<-popped
	store.Save(model.Script{ID: "b", Name: "Newer"})
	release()
	if !store.FlushSaves(10 * time.Second) {
		t.Fatal("flush timed out")
	}
	got, err := getByID(store, "b")
	if err != nil || got.Name != "Newer" || got.Enabled || fileExists(dir, "old.json") {
		t.Errorf("stale pending snapshot won: %+v %v", got, err)
	}
}

func TestLegacyKeyPressFileSurvivesSaveReload(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"id":"leg","name":"legacy","enabled":true,"run_mode":"once","hotkey_mode":"toggle","actions":[{"type":"loop","count":2,"actions":[{"type":"key_press","key":"2"}]},{"type":"key_press","key":"f5"}]}`
	if err := os.WriteFile(filepath.Join(dir, "legacy.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	st := NewScriptStore(dir)
	list, err := st.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("load: %v %d", err, len(list))
	}
	if seq := list[0].Actions[1].Sequence(); len(seq) != 1 || seq[0].Key != "f5" {
		t.Fatalf("legacy not a sequence of one: %+v", seq)
	}
	if err := st.Save(list[0]); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "legacy.json"))
	if strings.Contains(string(raw), `"keys"`) || !strings.Contains(string(raw), `"key": "2"`) {
		t.Fatalf("single plain key must stay legacy-form:\n%s", raw)
	}
	again, err := NewScriptStore(dir).List()
	if err != nil || !reflect.DeepEqual(list, again) {
		t.Fatalf("reload differs: %v", err)
	}

	multi := list[0]
	multi.Actions = []model.Action{{Type: model.ActionKeyPress, Keys: []model.KeyStroke{{Key: "a", Mods: []string{"ctrl"}}, {Key: "b"}}}}
	if err := st.Save(multi); err != nil {
		t.Fatal(err)
	}
	again, _ = NewScriptStore(dir).List()
	if !reflect.DeepEqual(again[0].Actions, multi.Actions) {
		t.Fatalf("multi-key lost: %+v", again[0].Actions)
	}
}

// oldAction is the Action struct on origin/main d3e5cf5 (model/action.go), before key sequences.
type oldAction struct {
	Type             string      `json:"type"`
	X                int         `json:"x,omitempty"`
	Y                int         `json:"y,omitempty"`
	Source           string      `json:"source,omitempty"`
	OffsetX          int         `json:"offset_x,omitempty"`
	OffsetY          int         `json:"offset_y,omitempty"`
	Key              string      `json:"key,omitempty"`
	DelayMs          int         `json:"ms,omitempty"`
	LoopCount        int         `json:"count,omitempty"`
	Actions          []oldAction `json:"actions,omitempty"`
	Image            string      `json:"image,omitempty"`
	TimeoutMs        int         `json:"timeout_ms,omitempty"`
	UseFoundPosition bool        `json:"use_found_position,omitempty"`
	ThenActions      []oldAction `json:"then,omitempty"`
	ElseActions      []oldAction `json:"else,omitempty"`
}

// Documents the downgrade behaviour the key-sequence format must live with: the
// old reader accepts any action type on load and silently drops unknown fields.
func TestOldReaderDropsUnknownFieldsKeepsUnknownType(t *testing.T) {
	for _, in := range []string{
		`{"type":"key_press","keys":[{"key":"a"},{"key":"b"}],"key_delay_ms":80}`,
		`{"type":"key_sequence","keys":[{"key":"a"},{"key":"b"}],"key_delay_ms":80}`,
	} {
		var a oldAction
		if err := json.Unmarshal([]byte(in), &a); err != nil {
			t.Fatalf("old reader must load it: %v", err)
		}
		out, _ := json.Marshal(a)
		if !strings.Contains(string(out), `"type":"`+a.Type+`"`) || a.Type == "" {
			t.Fatalf("unknown type must survive the old reader, got %s", out)
		}
		if strings.Contains(string(out), "keys") || strings.Contains(string(out), "key_delay_ms") {
			t.Fatalf("expected old reader to drop new fields, got %s", out)
		}
	}
}

func TestQueueSaveIfIsACompareAndSwapOnTheScriptRevision(t *testing.T) {
	store := NewScriptStore(t.TempDir())
	s := model.Script{ID: "a", Name: "A", Actions: []model.Action{}}
	if store.Revision("a") != 0 {
		t.Fatal("never written: 0")
	}
	r1, ok := store.QueueSaveIf("a", 0, false, nil, s)
	if !ok || r1 == 0 || store.Revision("a") != r1 {
		t.Fatalf("cas on 0: %d %v", r1, ok)
	}
	if _, ok := store.QueueSaveIf("a", 0, false, nil, s); ok {
		t.Fatal("stale etag must be refused")
	}
	if _, ok := store.QueueSaveIf("a", r1, false, func() bool { return false }, s); ok {
		t.Fatal("admit=false must be refused")
	}
	if !store.FlushSaves(2 * time.Second) {
		t.Fatal("flush")
	}
	if err := store.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if rv := store.Revision("a"); rv == 0 || rv == r1 {
		t.Fatalf("delete leaves a tombstone revision: %d", rv)
	}
	if _, ok := store.QueueSaveIf("a", r1, false, nil, s); ok {
		t.Fatal("a deleted script is not resurrected by a stale etag")
	}
}
