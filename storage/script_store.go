package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// readFile is a seam so tests can count file reads.
var readFile = os.ReadFile

// ScriptStore handles CRUD operations for script JSON files in a directory.
type ScriptStore struct {
	scriptsDirectory string

	mu    sync.Mutex
	files map[string]string // script ID -> file path; nil until first scan

	queueMu    sync.Mutex
	pending    map[string]model.Script
	pendingOrd []string
	draining   bool
	idle       chan struct{} // closed when the worker drains
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

// indexLocked returns the ID->path index, scanning the directory once if needed. Caller holds mu.
func (scriptStore *ScriptStore) indexLocked() map[string]string {
	if scriptStore.files != nil {
		return scriptStore.files
	}
	index := map[string]string{}
	entries, _ := os.ReadDir(scriptStore.scriptsDirectory)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		filePath := filepath.Join(scriptStore.scriptsDirectory, entry.Name())
		data, err := readFile(filePath)
		if err != nil {
			continue
		}
		var existing model.Script
		if err := json.Unmarshal(data, &existing); err != nil {
			continue
		}
		index[existing.ID] = filePath
	}
	scriptStore.files = index
	return index
}

// Save writes a script using its name as the filename.
// If the script was previously saved with a different name, the old file is removed.
// Only the target file (and a renamed-away predecessor) is touched; the directory is
// scanned at most once per store, not per call.
func (scriptStore *ScriptStore) Save(script model.Script) error {
	scriptStore.mu.Lock()
	defer scriptStore.mu.Unlock()

	if err := os.MkdirAll(scriptStore.scriptsDirectory, 0755); err != nil {
		return fmt.Errorf("create scripts directory: %w", err)
	}
	index := scriptStore.indexLocked()

	data, err := json.MarshalIndent(script, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal script: %w", err)
	}
	newPath := scriptStore.scriptFilePath(script.Name)
	tmpPath := newPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, newPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if oldPath, ok := index[script.ID]; ok && oldPath != newPath {
		os.Remove(oldPath)
	}
	index[script.ID] = newPath
	return nil
}

// QueueSave schedules scripts to be saved by a single background worker, in order.
// A newer queued state for the same script ID replaces an older pending one, so the
// last state wins and writes never overlap.
func (scriptStore *ScriptStore) QueueSave(scripts ...model.Script) {
	scriptStore.queueMu.Lock()
	if scriptStore.pending == nil {
		scriptStore.pending = map[string]model.Script{}
	}
	for _, script := range scripts {
		if _, ok := scriptStore.pending[script.ID]; !ok {
			scriptStore.pendingOrd = append(scriptStore.pendingOrd, script.ID)
		}
		scriptStore.pending[script.ID] = script
	}
	start := !scriptStore.draining && len(scriptStore.pendingOrd) > 0
	if start {
		scriptStore.draining = true
	}
	scriptStore.queueMu.Unlock()
	if start {
		go scriptStore.drainQueue()
	}
}

// FlushSaves blocks until all queued saves have been written, or the timeout elapses.
// It returns true if the queue drained. A timeout <= 0 waits indefinitely.
func (scriptStore *ScriptStore) FlushSaves(timeout time.Duration) bool {
	scriptStore.queueMu.Lock()
	if !scriptStore.draining {
		scriptStore.queueMu.Unlock()
		return true
	}
	if scriptStore.idle == nil {
		scriptStore.idle = make(chan struct{})
	}
	idle := scriptStore.idle
	scriptStore.queueMu.Unlock()
	if timeout <= 0 {
		<-idle
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-idle:
		return true
	case <-timer.C:
		return false
	}
}

func (scriptStore *ScriptStore) drainQueue() {
	for {
		scriptStore.queueMu.Lock()
		if len(scriptStore.pendingOrd) == 0 {
			scriptStore.draining = false
			if scriptStore.idle != nil {
				close(scriptStore.idle)
				scriptStore.idle = nil
			}
			scriptStore.queueMu.Unlock()
			return
		}
		id := scriptStore.pendingOrd[0]
		scriptStore.pendingOrd = scriptStore.pendingOrd[1:]
		script := scriptStore.pending[id]
		delete(scriptStore.pending, id)
		scriptStore.queueMu.Unlock()
		scriptStore.Save(script)
	}
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
		data, err := readFile(filePath)
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
	scriptStore.mu.Lock()
	defer scriptStore.mu.Unlock()
	index := scriptStore.indexLocked()
	filePath, ok := index[scriptID]
	if !ok {
		scriptStore.files = nil
		filePath, ok = scriptStore.indexLocked()[scriptID]
		index = scriptStore.files
	}
	scriptStore.queueMu.Lock()
	delete(scriptStore.pending, scriptID)
	scriptStore.queueMu.Unlock()
	if !ok {
		return fmt.Errorf("script %s not found", scriptID)
	}
	delete(index, scriptID)
	return os.Remove(filePath)
}
