package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

const (
	recoveredKeep    = 5 // files kept per script
	saveLeaseWaitMax = 500 * time.Millisecond
)

// RecoveryEnvelope is the on-disk form of a recovered draft (model.Script has no version field).
type RecoveryEnvelope struct {
	V        int          `json:"v"`
	ScriptID string       `json:"scriptId"`
	Revision uint64       `json:"revision"`
	SavedAt  string       `json:"savedAt"`
	Script   model.Script `json:"script"`
}

// RecoverResult is what a recovery did.
type RecoverResult struct {
	Found     bool   // an unsaved draft existed
	Persisted bool   // it was also saved into the script
	Path      string // the recovery file ("" when none could be written)
	Revision  uint64
	Reason    string
}

// closeSessionState marks the session as closing (atomically, before anything else) and waits at
// most saveLeaseWaitMax for in-flight saves to finish (contract §6.5). It never blocks longer and
// never takes part in native teardown. Reports whether saves are still in flight.
func (b *Bridge) markClosing(sess *editorSession) (savesInFlight bool) {
	sess.closingFlag.Store(true) // lock-free: visible even if b.mu is held by a stuck handler
	deadline := time.Now().Add(saveLeaseWaitMax)
	for {
		b.mu.Lock()
		sess.closing = true
		n := sess.saving
		b.mu.Unlock()
		if n == 0 {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// recoverDraft snapshots the newest unsaved draft and ALWAYS writes the recovery file; it also
// persists it into the script only when no save is in flight, the snapshot is newer than the last
// saved revision and it validates without conflicts. It bypasses the mutation fence (the session
// is already closing) and never overwrites a newer revision. Safe from any goroutine.
func (b *Bridge) recoverDraft(sess *editorSession, savesInFlight bool) RecoverResult {
	b.mu.Lock()
	script, rev, ok := sess.newestDraftLocked()
	b.mu.Unlock()
	if !ok {
		return RecoverResult{}
	}
	res := RecoverResult{Found: true, Revision: rev}
	if b.recoveredDir != "" {
		if path, err := writeRecovery(b.recoveredDir, RecoveryEnvelope{V: 1, ScriptID: sess.scriptID, Revision: rev, SavedAt: time.Now().UTC().Format(time.RFC3339), Script: script}); err != nil {
			res.Reason = "could not write recovery file"
		} else {
			res.Path = path
		}
	}
	if savesInFlight {
		return res
	}
	if _, err := b.saveScript(sess, script, saveOpts{rev: rev, recovery: true}); err == nil {
		res.Persisted = true
	}
	return res
}

func writeRecovery(dir string, env RecoveryEnvelope) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", err
	}
	if len(raw) > maxDraftBytes+4096 {
		return "", fmt.Errorf("draft too large")
	}
	name := fmt.Sprintf("%s-%d.json", safeName(env.ScriptID), time.Now().UnixMilli())
	final := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, ".recover-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("write recovery file")
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	pruneRecovery(dir, safeName(env.ScriptID))
	return final, nil
}

func safeName(id string) string {
	var sb strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			sb.WriteRune(r)
		}
	}
	if sb.Len() == 0 {
		return "script"
	}
	return sb.String()
}

func pruneRecovery(dir, prefix string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var mine []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), prefix+"-") && strings.HasSuffix(e.Name(), ".json") {
			mine = append(mine, e.Name())
		}
	}
	sort.Strings(mine) // unix-ms suffix: lexical order is age order for same-width stamps
	for len(mine) > recoveredKeep {
		_ = os.Remove(filepath.Join(dir, mine[0]))
		mine = mine[1:]
	}
}
