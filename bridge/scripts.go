package bridge

import (
	"log"
	"sort"
	"strings"

	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
)

type idParam struct {
	ID string `json:"id"`
}

type revResult struct {
	Rev uint64 `json:"rev"`
}

func summarize(s model.Script, runningID string) ScriptSummary {
	return ScriptSummary{
		ID: s.ID, Name: s.Name, Group: s.Group, RunMode: s.RunMode, HotkeyMode: s.HotkeyMode,
		HotkeyLabel: scriptHotkeyLabel(s), Enabled: s.Enabled, Running: s.ID != "" && s.ID == runningID,
	}
}

// scriptsList sorts ungrouped first, then by group, then by name (case-insensitive).
func (b *Bridge) scriptsList() (any, error) {
	running := b.d.Controller.Snapshot().RunningScriptID
	collapsed := map[string]bool{}
	for _, g := range b.d.Config.Get().CollapsedGroups {
		collapsed[g] = true
	}
	b.mu.Lock()
	scripts := append([]model.Script(nil), b.scripts...)
	rev := b.rev
	b.mu.Unlock()

	sort.SliceStable(scripts, func(i, j int) bool {
		gi, gj := strings.ToLower(scripts[i].Group), strings.ToLower(scripts[j].Group)
		if (gi == "") != (gj == "") {
			return gi == ""
		}
		if gi != gj {
			return gi < gj
		}
		return strings.ToLower(scripts[i].Name) < strings.ToLower(scripts[j].Name)
	})
	out := ScriptList{Rev: rev, Scripts: make([]ScriptSummary, 0, len(scripts)), Groups: []GroupDTO{}}
	groupIdx := map[string]int{}
	for _, s := range scripts {
		out.Scripts = append(out.Scripts, summarize(s, running))
		if s.Group == "" {
			continue
		}
		i, ok := groupIdx[s.Group]
		if !ok {
			i = len(out.Groups)
			groupIdx[s.Group] = i
			out.Groups = append(out.Groups, GroupDTO{Name: s.Group, AllEnabled: true, Collapsed: collapsed[s.Group]})
		}
		out.Groups[i].Count++
		out.Groups[i].AllEnabled = out.Groups[i].AllEnabled && s.Enabled
	}
	return out, nil
}

func (b *Bridge) scriptsGet(p idParam) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	i := b.indexLocked(p.ID)
	if i < 0 {
		return nil, newErr(CodeNotFound)
	}
	return b.scripts[i], nil
}

// queue marks the scripts as saving and hands them to the store in ONE QueueSave call. It is the
// only path to QueueSave and is atomic with Shutdown closing the queue: it reports false (and
// queues nothing) once the final flush is due, so no save can follow the flush.
func (b *Bridge) queue(changed []model.Script) bool {
	if len(changed) == 0 {
		return true
	}
	b.at("queue")
	b.qMu.Lock()
	if b.qClosed {
		b.qMu.Unlock()
		return false
	}
	b.mu.Lock()
	for _, s := range changed {
		delete(b.failed, s.ID)
	}
	b.mu.Unlock()
	b.d.Store.QueueSave(changed...)
	b.qMu.Unlock()
	b.syncHotkeys()
	return true
}

func (b *Bridge) scriptsSetEnabled(p struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}) (any, error) {
	b.mu.Lock()
	i := b.indexLocked(p.ID)
	if i < 0 {
		b.mu.Unlock()
		return nil, newErr(CodeNotFound)
	}
	if b.scripts[i].Enabled == p.Enabled {
		rev := b.rev
		b.mu.Unlock()
		return revResult{rev}, nil
	}
	b.scripts[i].Enabled = p.Enabled
	b.rev++
	rev, changed := b.rev, b.scripts[i]
	b.mu.Unlock()
	if !b.queue([]model.Script{changed}) {
		return nil, newErr(CodeBusy)
	}
	return revResult{rev}, nil
}

func (b *Bridge) scriptsSetGroupEnabled(p struct {
	Group   string `json:"group"`
	Enabled bool   `json:"enabled"`
}) (any, error) {
	if p.Group == "" || len(p.Group) > maxNameLen {
		return nil, badRequest()
	}
	b.mu.Lock()
	var changed []model.Script
	found := false
	for i := range b.scripts {
		if b.scripts[i].Group != p.Group {
			continue
		}
		found = true
		if b.scripts[i].Enabled != p.Enabled {
			b.scripts[i].Enabled = p.Enabled
			changed = append(changed, b.scripts[i])
		}
	}
	if !found {
		b.mu.Unlock()
		return nil, newErr(CodeNotFound)
	}
	if len(changed) > 0 {
		b.rev++
	}
	rev := b.rev
	b.mu.Unlock()
	if !b.queue(changed) {
		return nil, newErr(CodeBusy)
	}
	if len(changed) > 0 {
		b.emit(EvScriptsChanged, revResult{rev})
	}
	return struct {
		Rev     uint64 `json:"rev"`
		Changed int    `json:"changed"`
	}{rev, len(changed)}, nil
}

func (b *Bridge) scriptsSetGroupCollapsed(p struct {
	Group     string `json:"group"`
	Collapsed bool   `json:"collapsed"`
}) (any, error) {
	if p.Group == "" || len(p.Group) > maxNameLen {
		return nil, badRequest()
	}
	cfg := b.d.Config.Get()
	has := false
	for _, g := range cfg.CollapsedGroups {
		has = has || g == p.Group
	}
	if has == p.Collapsed {
		return empty{}, nil
	}
	if p.Collapsed && len(cfg.CollapsedGroups) >= 500 {
		return nil, badRequest()
	}
	err := b.d.Config.Update(func(c *model.AppConfig) {
		kept := c.CollapsedGroups[:0]
		for _, g := range c.CollapsedGroups {
			if g != p.Group {
				kept = append(kept, g)
			}
		}
		c.CollapsedGroups = kept
		if p.Collapsed {
			c.CollapsedGroups = append(c.CollapsedGroups, p.Group)
		}
	})
	if err != nil {
		log.Printf("bridge: persist collapsed groups failed: %v", err)
		return nil, newErr(CodeSaveFailed)
	}
	b.emit(EvSettingsChanged, settingsChangedEvent{Fields: []string{"collapsed_groups"}})
	return empty{}, nil
}

// nameTakenLocked reports whether name collides (case-insensitively or by file stem) with a script other than excludeID.
func (b *Bridge) nameTakenLocked(name, excludeID string) bool {
	stem := storage.SanitizeName(name)
	for _, s := range b.scripts {
		if s.ID != excludeID && (strings.EqualFold(s.Name, name) || storage.SanitizeName(s.Name) == stem) {
			return true
		}
	}
	return false
}

func (b *Bridge) scriptsClone(p idParam) (any, error) {
	b.mu.Lock()
	i := b.indexLocked(p.ID)
	if i < 0 {
		b.mu.Unlock()
		return nil, newErr(CodeNotFound)
	}
	source := b.scripts[i]
	b.mu.Unlock()

	clone := model.CloneScript(source, func(name string) bool {
		b.mu.Lock()
		taken := b.nameTakenLocked(name, "")
		b.mu.Unlock()
		return taken || b.d.Store.NameOrFileTaken(name)
	})
	b.mu.Lock()
	b.scripts = append(b.scripts, clone)
	b.rev++
	rev := b.rev
	b.mu.Unlock()
	if !b.queue([]model.Script{clone}) {
		return nil, newErr(CodeBusy)
	}
	b.emit(EvScriptsChanged, revResult{rev})
	return summarize(clone, ""), nil
}

func (b *Bridge) scriptsDelete(p idParam) (any, error) {
	b.mu.Lock()
	i := b.indexLocked(p.ID)
	b.mu.Unlock()
	if i < 0 {
		return nil, newErr(CodeNotFound)
	}
	if err := b.d.Store.Delete(p.ID); err != nil && b.persisted(p.ID) {
		log.Printf("bridge: delete %s failed: %v", p.ID, err)
		return nil, newErr(CodeSaveFailed)
	}
	b.mu.Lock()
	if i = b.indexLocked(p.ID); i >= 0 {
		b.scripts = append(b.scripts[:i:i], b.scripts[i+1:]...)
	}
	delete(b.failed, p.ID)
	b.rev++
	rev := b.rev
	b.mu.Unlock()
	b.syncHotkeys()
	b.emit(EvScriptsChanged, revResult{rev})
	return revResult{rev}, nil
}

// persisted reports whether the store still lists id (a never-saved script is not a delete failure).
func (b *Bridge) persisted(id string) bool {
	list, err := b.d.Store.List()
	if err != nil {
		return true
	}
	for _, s := range list {
		if s.ID == id {
			return true
		}
	}
	return false
}

func (b *Bridge) scriptsRetrySave(p idParam) (any, error) {
	b.mu.Lock()
	i := b.indexLocked(p.ID)
	if i < 0 {
		b.mu.Unlock()
		return nil, newErr(CodeNotFound)
	}
	s := b.scripts[i]
	b.mu.Unlock()
	if !b.queue([]model.Script{s}) {
		return nil, newErr(CodeBusy)
	}
	return empty{}, nil
}
