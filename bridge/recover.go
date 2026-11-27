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
	recoverBound     = 2 * time.Second // persistence bound after a forced close (contract §4.5)
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

// waitSaves waits at most d, on the injected clock, for the session's in-flight saves to release
// their leases. It takes no lock (the lease count and the notification are lock-free) and reports
// whether saves are still in flight when the bound expired.
func (b *Bridge) waitSaves(sess *editorSession, d time.Duration) (inFlight bool) {
	if sess.savingN.Load() == 0 {
		return false
	}
	expired := make(chan struct{})
	t := b.clock().AfterFunc(d, func() { close(expired) })
	defer t.Stop()
	for sess.savingN.Load() > 0 {
		select {
		case <-sess.notify:
		case <-expired:
			return sess.savingN.Load() > 0
		}
	}
	return false
}

// recoverDraft writes the newest unsaved draft to the recovery file (always) and then persists it
// into the script only when no save is in flight, it is newer than the last saved revision and it
// validates without conflicts. Everything is bounded by deadline: it reads the lock-free snapshot,
// runs the persistence on its own goroutine and abandons it at the deadline, and nothing commits
// or flushes after it. It bypasses the mutation fence (the session is already closing) and never
// overwrites a newer revision.
func (b *Bridge) recoverDraft(sess *editorSession, savesInFlight bool, deadline time.Time) RecoverResult {
	snap := sess.snap.Load()
	if snap == nil || !snap.ok || snap.rev <= snap.lastSaved {
		return RecoverResult{}
	}
	res := RecoverResult{Found: true, Revision: snap.rev}
	clk := b.clock()
	if b.recoveredDir != "" && clk.Now().Before(deadline) {
		env := RecoveryEnvelope{V: 1, ScriptID: sess.scriptID, Revision: snap.rev, SavedAt: time.Now().UTC().Format(time.RFC3339), Script: snap.script}
		if path, err := writeRecovery(b.recoveredDir, env); err != nil {
			res.Reason = "could not write recovery file"
		} else {
			res.Path = path
		}
	}
	if savesInFlight || !clk.Now().Before(deadline) {
		return res
	}
	done := make(chan error, 1)
	go func() {
		_, err := b.saveScript(sess, snap.script, saveOpts{rev: snap.rev, recovery: true, deadline: deadline})
		done <- err
	}()
	expired := make(chan struct{})
	t := clk.AfterFunc(deadline.Sub(clk.Now()), func() { close(expired) })
	defer t.Stop()
	select {
	case err := <-done:
		res.Persisted = err == nil
	case <-expired: // abandoned: the commit-time deadline check keeps it from persisting later
	}
	return res
}

// writeRecovery writes the envelope as compact JSON, atomically (temp + rename), 0600. The script
// is bounded by the same compact measure as every draft (maxDraftBytes); the envelope adds only its
// fixed fields, so no slack is allowed.
func writeRecovery(dir string, env RecoveryEnvelope) (string, error) {
	if draftBytes(env.Script) > maxDraftBytes {
		return "", fmt.Errorf("draft too large")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", err
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
