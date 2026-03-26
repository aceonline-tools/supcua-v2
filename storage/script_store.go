package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yanmad27/supcua-v2/model"
)

// ScriptStore handles CRUD operations for script JSON files in a directory.
type ScriptStore struct {
	scriptsDirectory string
}

// NewScriptStore creates a ScriptStore for the given directory.
func NewScriptStore(scriptsDirectory string) *ScriptStore {
	return &ScriptStore{scriptsDirectory: scriptsDirectory}
}

func (scriptStore *ScriptStore) scriptFilePath(scriptID string) string {
	return filepath.Join(scriptStore.scriptsDirectory, scriptID+".json")
}

// Save writes a script to disk as a JSON file named by its ID.
func (scriptStore *ScriptStore) Save(script model.Script) error {
	if err := os.MkdirAll(scriptStore.scriptsDirectory, 0755); err != nil {
		return fmt.Errorf("create scripts directory: %w", err)
	}

	data, err := json.MarshalIndent(script, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal script: %w", err)
	}

	return os.WriteFile(scriptStore.scriptFilePath(script.ID), data, 0644)
}

// Get reads a single script by ID.
func (scriptStore *ScriptStore) Get(scriptID string) (model.Script, error) {
	data, err := os.ReadFile(scriptStore.scriptFilePath(scriptID))
	if err != nil {
		return model.Script{}, fmt.Errorf("read script %s: %w", scriptID, err)
	}

	var script model.Script
	if err := json.Unmarshal(data, &script); err != nil {
		return model.Script{}, fmt.Errorf("unmarshal script %s: %w", scriptID, err)
	}
	return script, nil
}

// List reads all scripts from the directory.
func (scriptStore *ScriptStore) List() ([]model.Script, error) {
	entries, err := os.ReadDir(scriptStore.scriptsDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read scripts directory: %w", err)
	}

	var allScripts []model.Script
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		scriptID := strings.TrimSuffix(entry.Name(), ".json")
		script, err := scriptStore.Get(scriptID)
		if err != nil {
			continue
		}
		allScripts = append(allScripts, script)
	}
	return allScripts, nil
}

// Delete removes a script file by ID.
func (scriptStore *ScriptStore) Delete(scriptID string) error {
	filePath := scriptStore.scriptFilePath(scriptID)
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("delete script %s: %w", scriptID, err)
	}
	return nil
}
