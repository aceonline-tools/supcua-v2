package storage

import (
	"fmt"
	"os"
	"path/filepath"
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
	os.WriteFile(legacy, []byte(`{"id":"legacy-1","name":"Legacy"}`), 0644)
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
