package storage

import (
	"path/filepath"
	"testing"

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

	loaded, err := scriptStore.Get("test-id-1")
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

	_, err = scriptStore.Get("del-1")
	if err == nil {
		t.Error("expected error getting deleted script")
	}
}

func TestGetScriptNotFound(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	_, err := scriptStore.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing script")
	}
}

func TestScriptFilePath(t *testing.T) {
	tempDir := t.TempDir()
	scriptStore := NewScriptStore(tempDir)

	expected := filepath.Join(tempDir, "test-id.json")
	actual := scriptStore.scriptFilePath("test-id")
	if actual != expected {
		t.Errorf("expected path %s, got %s", expected, actual)
	}
}
