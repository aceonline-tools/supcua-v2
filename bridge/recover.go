package bridge

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	snap := sess.seal()
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
	var fileSt atomic.Int32 // the completion/abandonment handshake: exactly one side wins
	if b.recoveredDir != "" && clk.Now().Before(deadline) {
		fileCh = make(chan fileRes, 1)
		env := RecoveryEnvelope{V: 1, ScriptID: sess.scriptID, Revision: snap.rev, SavedAt: time.Now().UTC().Format(time.RFC3339), Script: snap.script}
		dir := b.recoveredDir
		go func() {
			b.at("recover.file")
			path, err := writeRecovery(dir, env)
			fileCh <- fileRes{path, err}
			if !fileSt.CompareAndSwap(recPending, recEnqueued) { // cleanup gave up on it first
				log.Printf("bridge: late recovery file for script %s: path=%q err=%v", sess.scriptID, path, err)
			}
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
		select {
		case r, got = <-fileCh:
			fileSt.Store(recEnqueued)
		case <-expired:
			if !fileSt.CompareAndSwap(recPending, recAbandoned) {
				r, got = <-fileCh // it completed first: its result is already sent
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
	b.admitMu.Lock() // a newer generation registers its script either before this check or after the enqueue
	b.at("recover.admit")
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
	b.admitMu.Unlock()
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

// lateRecoveryFile writes a payload published after its session was sealed as an extra file-only
// recovery file (amendment I) and logs it. One writer goroutine per session drains the newest
// pending payload; a payload not newer than one already covered (the sealed snapshot, or a newer
// late payload) is superseded by it, never lost.
func (b *Bridge) lateRecoveryFile(sess *editorSession, script model.Script, rev uint64) {
	dir := b.recoveredDir
	if dir == "" {
		log.Printf("bridge: save payload rev %d of script %s arrived after recovery sealed the session; no recovery dir", rev, sess.scriptID)
		return
	}
	sess.lateMu.Lock()
	if rev <= sess.lateMax {
		sess.lateMu.Unlock()
		return // superseded by a newer covered revision
	}
	sess.lateMax = rev
	sess.lateNext = &RecoveryEnvelope{V: 1, ScriptID: sess.scriptID, Revision: rev, SavedAt: time.Now().UTC().Format(time.RFC3339), Script: script}
	start := !sess.lateRunning
	sess.lateRunning = true
	sess.lateMu.Unlock()
	// logged after unlocking: a stalled log sink never holds lateMu (seal takes it)
	log.Printf("bridge: save payload rev %d of script %s arrived after recovery sealed the session; writing it file-only", rev, sess.scriptID)
	if !start {
		return
	}
	go func() {
		for {
			sess.lateMu.Lock()
			env := sess.lateNext
			sess.lateNext = nil
			if env == nil {
				sess.lateRunning = false
				sess.lateMu.Unlock()
				return
			}
			sess.lateMu.Unlock()
			path, err := writeRecovery(dir, *env)
			log.Printf("bridge: late-payload recovery file for script %s rev %d: path=%q err=%v", env.ScriptID, env.Revision, path, err)
			b.at("recover.latefile")
		}
	}()
}

// recoveryNow is the wall clock used for recovery file names (a test seam).
var recoveryNow = time.Now

// writeRecovery writes the envelope as compact JSON, 0600, under a name it reserved exclusively:
// <id>-<ms:13>-<rev:10>[-n].json. The script is bounded by the same compact measure as every draft.
//
// Publication is reserve-then-rename: the final name is first created with O_CREATE|O_EXCL (so no
// two writers can ever own it, on any filesystem — NTFS, FAT/exFAT or POSIX), the content is
// written to a temp file, then the temp file is renamed over the reservation, which only this
// writer owns, so the replace can never hit another recovery file. Crash consistency: a crash
// leaves either the empty reservation (readers skip empty files; pruning removes it in time) or
// the complete file — never a partially written one. Writing into the reservation directly would
// save the rename but could leave a truncated JSON file after a crash.
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
	base := fmt.Sprintf("%s-%013d-%010d", safeName(env.ScriptID), recoveryNow().UnixMilli(), env.Revision)
	final, err := reserveRecoveryName(dir, base)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".recover-*")
	if err != nil {
		_ = os.Remove(final)
		return "", err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(raw)
	serr := tmp.Sync()
	cerr := tmp.Close()
	if werr != nil || serr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		_ = os.Remove(final)
		return "", fmt.Errorf("write recovery file")
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		_ = os.Remove(final)
		return "", err
	}
	if err := os.Rename(tmpName, final); err != nil { // replaces only our own reservation
		_ = os.Remove(tmpName)
		_ = os.Remove(final)
		return "", err
	}
	pruneRecovery(dir, safeName(env.ScriptID))
	return final, nil
}

// reserveRecoveryName creates base.json (or base-n.json) exclusively and returns its path.
func reserveRecoveryName(dir, base string) (string, error) {
	for n := 0; n < 1000; n++ {
		name := base + ".json"
		if n > 0 {
			name = fmt.Sprintf("%s-%d.json", base, n)
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return p, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("no free recovery file name")
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

type recName struct {
	name       string
	ms, rev, n uint64
}

// parseRecoveryName parses <prefix>-<ms>-<rev>[-n].json; ok is false for any other name (another
// script whose id starts with prefix+"-", or a foreign file), which pruning then leaves alone.
func parseRecoveryName(name, prefix string) (recName, bool) {
	rest, ok := strings.CutPrefix(name, prefix+"-")
	if !ok {
		return recName{}, false
	}
	rest, ok = strings.CutSuffix(rest, ".json")
	if !ok {
		return recName{}, false
	}
	parts := strings.Split(rest, "-")
	if len(parts) < 2 || len(parts) > 3 || len(parts[0]) != 13 || len(parts[1]) != 10 {
		return recName{}, false
	}
	var v [3]uint64
	for i, p := range parts {
		x, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return recName{}, false
		}
		v[i] = x
	}
	return recName{name, v[0], v[1], v[2]}, true
}

// pruneRecovery keeps the newest recoveredKeep files of a script, ordered numerically by
// (ms, revision, n) — never lexically.
func pruneRecovery(dir, prefix string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var mine []recName
	for _, e := range ents {
		if r, ok := parseRecoveryName(e.Name(), prefix); ok {
			mine = append(mine, r)
		}
	}
	sort.Slice(mine, func(i, j int) bool {
		a, b := mine[i], mine[j]
		if a.ms != b.ms {
			return a.ms < b.ms
		}
		if a.rev != b.rev {
			return a.rev < b.rev
		}
		return a.n < b.n
	})
	for len(mine) > recoveredKeep {
		_ = os.Remove(filepath.Join(dir, mine[0].name))
		mine = mine[1:]
	}
}
