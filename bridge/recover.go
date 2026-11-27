package bridge

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
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
	b.at("lease.wait")
	for sess.savingN.Load() > 0 {
		select {
		case <-sess.notify:
		case <-expired:
			return sess.savingN.Load() > 0
		}
	}
	return false
}

// Recovery persistence states (editor-window §4.5 amendment B). The enqueue goroutine moves
// pending→enqueued under the store's queue lock; the deadline moves pending→abandoned. Exactly one
// wins, so nothing is enqueued after the deadline and an abandoned recovery can never enqueue later.
const (
	recPending int32 = iota
	recEnqueued
	recAbandoned
)

// recoverDraft recovers the newest unsaved draft of a detached session (amendments A–C). The
// recovery file is written on its own goroutine; persistence into the script is a compare-and-swap
// enqueue on another. It returns at the deadline at the latest: persistence that did not enqueue by
// then is abandoned (file only); a file write still running is logged when it lands. released is
// called as soon as persistence is settled, before any wait on file I/O (the gate never waits on it).
func (b *Bridge) recoverDraft(sess *editorSession, savesInFlight bool, deadline time.Time, released func()) RecoverResult {
	snap := sess.snap.Load()
	if snap == nil || !snap.ok || snap.rev <= snap.lastSaved {
		released()
		return RecoverResult{}
	}
	res := RecoverResult{Found: true, Revision: snap.rev}
	clk := b.clock()
	expired, stop := b.afterChan(deadline.Sub(clk.Now()))
	defer stop()

	type fileRes struct {
		path string
		err  error
	}
	var fileCh chan fileRes
	var late atomic.Bool
	if b.recoveredDir != "" && clk.Now().Before(deadline) {
		fileCh = make(chan fileRes, 1)
		env := RecoveryEnvelope{V: 1, ScriptID: sess.scriptID, Revision: snap.rev, SavedAt: time.Now().UTC().Format(time.RFC3339), Script: snap.script}
		dir := b.recoveredDir
		go func() {
			b.at("recover.file")
			path, err := writeRecovery(dir, env)
			if late.Load() {
				log.Printf("bridge: late recovery file for script %s: path=%q err=%v", sess.scriptID, path, err)
			}
			fileCh <- fileRes{path, err}
		}()
	}

	var st atomic.Int32
	if !savesInFlight && clk.Now().Before(deadline) {
		persisted := make(chan bool, 1)
		go func() {
			ok := b.persistRecovery(sess, snap, &st, deadline)
			persisted <- ok
			b.at("recover.persisted")
		}()
		select {
		case res.Persisted = <-persisted:
		case <-expired:
			if !st.CompareAndSwap(recPending, recAbandoned) {
				res.Persisted = st.Load() == recEnqueued
			}
		}
	}
	released()

	if fileCh != nil {
		var r fileRes
		got := false
		select { // a finished write wins over an expired deadline
		case r, got = <-fileCh:
		default:
			select {
			case r, got = <-fileCh:
			case <-expired:
				late.Store(true)
				select { // the write may have landed while the flag was being set
				case r, got = <-fileCh:
				default:
				}
			}
		}
		switch {
		case !got:
			res.Reason = "recovery file still being written"
		case r.err != nil:
			res.Reason = "could not write recovery file"
		default:
			res.Path = r.path
		}
	}
	return res
}

// persistRecovery enqueues the recovered draft into the script (amendment A) only when, atomically
// under the store's queue lock: the store revision of the script still equals the session's etag
// (nobody else saved it since), no newer editor generation has the script open, the deadline has
// not passed and the deadline did not abandon it. Only then is the in-memory list updated (under
// Bridge.mu, held across the enqueue). It never waits for the write: anything enqueued is flushed
// by the store and the shutdown quiesce. Any blocking here only delays a goroutine that can no
// longer enqueue once abandoned.
func (b *Bridge) persistRecovery(sess *editorSession, snap *recSnap, st *atomic.Int32, deadline time.Time) bool {
	rs, ok := b.d.Store.(RevStore)
	if !ok {
		return false
	}
	script := snap.script
	script.ID = sess.scriptID
	if validateScript(&script, b.known) != nil || b.d.Store.NameOrFileTakenExcept(script.Name, script.ID) {
		return false
	}
	b.at("recover.enqueue")
	clk := b.clock()
	w := b.winCtl()
	b.qMu.Lock()
	if b.qClosed {
		b.qMu.Unlock()
		return false
	}
	b.mu.Lock()
	others := make([]model.Script, 0, len(b.scripts))
	for _, s := range b.scripts {
		if s.ID != script.ID {
			others = append(others, s)
		}
	}
	if b.nameTakenLocked(script.Name, script.ID) || snap.rev <= sess.lastSavedRev || len(b.findConflicts(script, others)) > 0 {
		b.mu.Unlock()
		b.qMu.Unlock()
		return false
	}
	cur := b.liveLocked()
	_, queued := rs.QueueSaveIf(script.ID, sess.etag, false, func() bool {
		if cur != nil && cur.scriptID == script.ID {
			return false
		}
		if w != nil {
			if p := w.activeScript.Load(); p != nil && *p == script.ID {
				return false
			}
		}
		return clk.Now().Before(deadline) && st.CompareAndSwap(recPending, recEnqueued)
	}, script)
	if !queued {
		b.mu.Unlock()
		b.qMu.Unlock()
		return false
	}
	delete(b.failed, script.ID)
	if i := b.indexLocked(script.ID); i >= 0 {
		b.scripts[i] = script
	} else {
		b.scripts = append(b.scripts, script)
	}
	b.rev++
	rev := b.rev
	if snap.rev > sess.lastSavedRev {
		sess.lastSavedRev = snap.rev
	}
	sess.publishLocked()
	b.mu.Unlock()
	b.qMu.Unlock()
	b.syncHotkeys()
	b.emit(EvScriptsChanged, revResult{rev})
	return true
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
