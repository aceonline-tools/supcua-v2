package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// sanitizeName converts a script name to a safe filename (lowercase, underscores).
func sanitizeName(name string) string {
	result := strings.ToLower(name)
	result = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, result)
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}
	return strings.Trim(result, "_")
}

func (scriptStore *ScriptStore) scriptFilePath(name string) string {
	return filepath.Join(scriptStore.scriptsDirectory, sanitizeName(name)+".json")
}

// NameExists returns true if a script with the given name exists, excluding the script with excludeID.
func (scriptStore *ScriptStore) NameExists(name string, excludeID string) bool {
	scripts, err := scriptStore.List()
	if err != nil {
		return false
	}
	for _, s := range scripts {
		if strings.EqualFold(s.Name, name) && s.ID != excludeID {
			return true
		}
	}
	return false
}

// NameOrFileTaken returns true if the name is used by an existing script or if saving
// under this name would write to a file that already exists (e.g. "A (copy)" vs "A copy").
func (scriptStore *ScriptStore) NameOrFileTaken(name string) bool {
	if scriptStore.NameExists(name, "") {
		return true
	}
	_, err := os.Stat(scriptStore.scriptFilePath(name))
	return err == nil
}

// Save writes a script using its name as the filename.
// If the script was previously saved with a different name, the old file is removed.
func (scriptStore *ScriptStore) Save(script model.Script) error {
	if err := os.MkdirAll(scriptStore.scriptsDirectory, 0755); err != nil {
		return fmt.Errorf("create scripts directory: %w", err)
	}

	// Remove old file if script was renamed or previously had UUID-based filename
	entries, _ := os.ReadDir(scriptStore.scriptsDirectory)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		filePath := filepath.Join(scriptStore.scriptsDirectory, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		var existing model.Script
		if err := json.Unmarshal(data, &existing); err != nil {
			continue
		}
		if existing.ID == script.ID {
			newPath := scriptStore.scriptFilePath(script.Name)
			if filePath != newPath {
				os.Remove(filePath)
			}
			break
		}
	}

	data, err := json.MarshalIndent(script, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal script: %w", err)
	}
	return os.WriteFile(scriptStore.scriptFilePath(script.Name), data, 0644)
}

// List reads all scripts from the directory, sorted alphabetically by name.
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
		filePath := filepath.Join(scriptStore.scriptsDirectory, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		var script model.Script
		if err := json.Unmarshal(data, &script); err != nil {
			continue
		}
		allScripts = append(allScripts, script)
	}

	sort.Slice(allScripts, func(i, j int) bool {
		return strings.ToLower(allScripts[i].Name) < strings.ToLower(allScripts[j].Name)
	})
	return allScripts, nil
}

// Delete removes a script file by ID.
func (scriptStore *ScriptStore) Delete(scriptID string) error {
	entries, _ := os.ReadDir(scriptStore.scriptsDirectory)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		filePath := filepath.Join(scriptStore.scriptsDirectory, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		var script model.Script
		if err := json.Unmarshal(data, &script); err != nil {
			continue
		}
		if script.ID == scriptID {
			return os.Remove(filePath)
		}
	}
	return fmt.Errorf("script %s not found", scriptID)
}
