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

type queuedSave struct {
	script model.Script
	token  uint64
}

// beforeQueuedWrite is a test seam called by the worker after it pops a save, before it locks the store.
var beforeQueuedWrite func(id string)

// ScriptStore handles CRUD operations for script JSON files in a directory.
type ScriptStore struct {
	scriptsDirectory string

	mu    sync.Mutex
	files map[string]string // script ID -> file path; nil until first scan

	queueMu    sync.Mutex
	pending    map[string]queuedSave
	inflight   map[string]queuedSave // popped by the worker, not yet written
	gen        map[string]uint64     // latest write token per ID; absent after Delete
	genCounter uint64
	pendingOrd []string
	draining   bool
	idle       chan struct{} // closed when the worker drains
	onSaveErr  func(script model.Script, err error)
}

// NewScriptStore creates a ScriptStore for the given directory.
func NewScriptStore(scriptsDirectory string) *ScriptStore {
	return &ScriptStore{scriptsDirectory: scriptsDirectory}
}

// SetOnSaveError registers a hook called from the save worker when a queued write fails.
// The hook runs without any store lock held and must not block.
func (scriptStore *ScriptStore) SetOnSaveError(hook func(script model.Script, err error)) {
	scriptStore.queueMu.Lock()
	scriptStore.onSaveErr = hook
	scriptStore.queueMu.Unlock()
}

// SanitizeName returns the file-name stem a script name maps to.
func SanitizeName(name string) string { return sanitizeName(name) }

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

// NameOrFileTaken returns true if the name is used by an existing, queued or in-flight script,
// or if saving under this name would write to a file that already exists or is about to be
// written by the save worker (e.g. "A (copy)" vs "A copy").
func (scriptStore *ScriptStore) NameOrFileTaken(name string) bool {
	if scriptStore.NameExists(name, "") {
		return true
	}
	target := sanitizeName(name)
	scriptStore.queueMu.Lock()
	taken := false
	for _, item := range scriptStore.pending {
		if strings.EqualFold(item.script.Name, name) || sanitizeName(item.script.Name) == target {
			taken = true
			break
		}
	}
	if !taken {
		for _, item := range scriptStore.inflight {
			if strings.EqualFold(item.script.Name, name) || sanitizeName(item.script.Name) == target {
				taken = true
				break
			}
		}
	}
	scriptStore.queueMu.Unlock()
	if taken {
		return true
	}
	_, err := os.Stat(scriptStore.scriptFilePath(name))
	return err == nil
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
	scriptStore.queueMu.Lock()
	token := scriptStore.supersedeLocked(script.ID)
	scriptStore.queueMu.Unlock()

	scriptStore.mu.Lock()
	defer scriptStore.mu.Unlock()
	scriptStore.queueMu.Lock()
	current := scriptStore.gen[script.ID] == token
	scriptStore.queueMu.Unlock()
	if !current {
		return nil
	}
	return scriptStore.writeLocked(script)
}

// supersedeLocked drops any pending snapshot of id and issues a new write token,
// invalidating in-flight older snapshots. Caller holds queueMu.
func (scriptStore *ScriptStore) supersedeLocked(id string) uint64 {
	if _, ok := scriptStore.pending[id]; ok {
		delete(scriptStore.pending, id)
		for i, pid := range scriptStore.pendingOrd {
			if pid == id {
				scriptStore.pendingOrd = append(scriptStore.pendingOrd[:i:i], scriptStore.pendingOrd[i+1:]...)
				break
			}
		}
	}
	if scriptStore.gen == nil {
		scriptStore.gen = map[string]uint64{}
	}
	scriptStore.genCounter++
	scriptStore.gen[id] = scriptStore.genCounter
	return scriptStore.genCounter
}

// writeLocked writes one script file. Caller holds mu.
func (scriptStore *ScriptStore) writeLocked(script model.Script) error {

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
		scriptStore.pending = map[string]queuedSave{}
	}
	for _, script := range scripts {
		token := scriptStore.supersedeLocked(script.ID)
		scriptStore.pendingOrd = append(scriptStore.pendingOrd, script.ID)
		scriptStore.pending[script.ID] = queuedSave{script: script, token: token}
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

// writeQueued writes a queued snapshot only if it is still the latest for its ID.
func (scriptStore *ScriptStore) writeQueued(item queuedSave) {
	err := scriptStore.writeQueuedLocked(item)
	if err == nil {
		return
	}
	scriptStore.queueMu.Lock()
	hook := scriptStore.onSaveErr
	scriptStore.queueMu.Unlock()
	if hook != nil {
		hook(item.script, err)
	}
}

func (scriptStore *ScriptStore) writeQueuedLocked(item queuedSave) error {
	scriptStore.mu.Lock()
	defer scriptStore.mu.Unlock()
	scriptStore.queueMu.Lock()
	current := scriptStore.gen[item.script.ID] == item.token
	scriptStore.queueMu.Unlock()
	if !current {
		return nil
	}
	return scriptStore.writeLocked(item.script)
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
		item := scriptStore.pending[id]
		delete(scriptStore.pending, id)
		if scriptStore.inflight == nil {
			scriptStore.inflight = map[string]queuedSave{}
		}
		scriptStore.inflight[id] = item
		scriptStore.queueMu.Unlock()
		if beforeQueuedWrite != nil {
			beforeQueuedWrite(id)
		}
		scriptStore.writeQueued(item)
		scriptStore.queueMu.Lock()
		delete(scriptStore.inflight, id)
		scriptStore.queueMu.Unlock()
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
	scriptStore.supersedeLocked(scriptID)
	delete(scriptStore.gen, scriptID)
	scriptStore.queueMu.Unlock()
	if !ok {
		return fmt.Errorf("script %s not found", scriptID)
	}
	delete(index, scriptID)
	return os.Remove(filePath)
}
