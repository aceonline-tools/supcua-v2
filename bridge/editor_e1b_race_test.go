package bridge

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stages blocks the first hit of each blocked stage until released and signals every notify stage.
type stages struct {
	in, release map[string]chan struct{}
	hit         map[string]*atomic.Bool
	reached     map[string]chan struct{}
}

func hookStages(e *edRig, block []string, notify []string) *stages {
	s := &stages{in: map[string]chan struct{}{}, release: map[string]chan struct{}{}, hit: map[string]*atomic.Bool{}, reached: map[string]chan struct{}{}}
	for _, b := range block {
		s.in[b], s.release[b], s.hit[b] = make(chan struct{}), make(chan struct{}), &atomic.Bool{}
	}
	for _, n := range notify {
		s.reached[n] = make(chan struct{})
		s.hit["n:"+n] = &atomic.Bool{}
	}
	e.b.testHook = func(stage string) {
		if ch, ok := s.reached[stage]; ok && s.hit["n:"+stage].CompareAndSwap(false, true) {
			close(ch)
		}
		if h, ok := s.hit[stage]; ok && h.CompareAndSwap(false, true) {
			close(s.in[stage])
			<-s.release[stage]
		}
	}
	return s
}

func recv(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func notClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s must not have happened yet", what)
	case <-time.After(60 * time.Millisecond):
	}
}

func queuedNames(e *edRig) []string {
	var out []string
	for _, c := range e.store.calls() {
		for _, s := range c {
			out = append(out, s.ID+"="+s.Name)
		}
	}
	return out
}

func hasQueued(e *edRig, idName string) bool {
	for _, n := range queuedNames(e) {
		if n == idName {
			return true
		}
	}
	return false
}

func recoveryFiles(t *testing.T, dir string) []RecoveryEnvelope {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	var out []RecoveryEnvelope
	for _, en := range ents {
		if strings.HasPrefix(en.Name(), ".") || !strings.HasSuffix(en.Name(), ".json") {
			continue // a temp file still being written (temp + rename)
		}
		raw, err := os.ReadFile(filepath.Join(dir, en.Name()))
		if os.IsNotExist(err) {
			continue // pruned meanwhile
		}
		if err == nil && len(raw) == 0 {
			continue // a name reservation whose content is still being written
		}
		if err != nil {
			t.Fatal(err)
		}
		var env RecoveryEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		out = append(out, env)
	}
	return out
}

// Item 1 (amendment B): a recovery that has not enqueued by its deadline is abandoned and can never
// enqueue later, even when its goroutine resumes after cleanup finished.
func TestE1bItem1RecoveryNotEnqueuedByItsDeadlineNeverEnqueuesLater(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Late Draft")+`}`)
	h := hookStages(e, []string{"recover.enqueue"}, []string{"recover.persisted"})
	e.b.win.ProcessFailed(e.gen, 1)
	_, done := e.b.win.waitChans()
	recv(t, h.in["recover.enqueue"], "recovery reaching its enqueue")
	notClosed(t, done, "cleanup before the deadline")
	e.clk.Advance(recoverBound + time.Millisecond)
	recv(t, done, "cleanup at the deadline")
	close(h.release["recover.enqueue"])
	recv(t, h.reached["recover.persisted"], "the abandoned recovery goroutine returning")
	if hasQueued(e, "a=Late Draft") || e.scr("a").Name != "S a" {
		t.Fatalf("an abandoned recovery enqueued after its deadline: %v %q", queuedNames(e), e.scr("a").Name)
	}
	waitFor(t, func() bool { return len(recoveryFiles(t, e.dir)) == 1 }) // possibly landing late (logged)
	if f := recoveryFiles(t, e.dir); len(f) != 1 || f[0].Script.Name != "Late Draft" {
		t.Fatalf("the draft must survive as a file: %+v", f)
	}
}

// Item 2 (amendment A): a detached A's recovery is a compare-and-swap against the script's store
// revision captured at open, and is refused while a newer generation has the script open.
func TestE1bItem2DetachedRecoveryNeverOverwritesANewerSaveOfTheSameScript(t *testing.T) {
	for _, bSaves := range []bool{true, false} {
		name := map[bool]string{true: "B saved and closed", false: "B has it open"}[bSaves]
		t.Run(name, func(t *testing.T) {
			e := newEdRig(t, edScript("a"))
			e.openAll(t, "a")
			e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "A Draft")+`}`)
			h := hookStages(e, []string{"recover.enqueue"}, []string{"recover.persisted"})
			e.b.win.ProcessFailed(e.gen, 1)
			_, doneA := e.b.win.waitChans()
			recv(t, h.in["recover.enqueue"], "A's recovery paused before its enqueue")

			e.openAll(t, "a") // B
			want := "S a"
			if bSaves {
				e.mustCall(t, "editor.save", `{"sessionId":"`+e.sid+`","snapshotRev":1,"script":`+scriptJSON("a", "B Saved")+`}`)
				e.b.win.CloseRequested(e.gen)
				e.mustCall(t, "editor.closeDecision", `{"sessionId":"`+e.sid+`","decision":"discard"}`)
				e.waitDone(t)
				want = "B Saved"
			}
			close(h.release["recover.enqueue"])
			recv(t, h.reached["recover.persisted"], "A's recovery deciding")
			if hasQueued(e, "a=A Draft") || e.scr("a").Name != want {
				t.Fatalf("A's recovery overwrote the newer state: %v %q", queuedNames(e), e.scr("a").Name)
			}
			waitFor(t, func() bool { return len(recoveryFiles(t, e.dir)) == 1 })
			e.clk.Advance(recoverBound + time.Millisecond)
			recv(t, doneA, "A's cleanup")
			if f := recoveryFiles(t, e.dir); len(f) != 1 || f[0].Script.Name != "A Draft" {
				t.Fatalf("A's draft must be file-only: %+v", f)
			}
			if !bSaves && e.b.sessionIDForGen(e.gen) != e.sid {
				t.Fatal("B's session must be untouched")
			}
		})
	}
}

// Item 3 (amendment C): the recovery file is written on its own goroutine; a stalled write neither
// holds the gate nor delays cleanup past the deadline, and lands late (logged).
func TestE1bItem3StalledRecoveryFileWriteIsBoundedAndNeverHoldsTheGate(t *testing.T) {
	buf := &syncBuf{}
	log.SetOutput(buf)
	defer log.SetOutput(os.Stderr)
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Draft")+`}`)
	h := hookStages(e, []string{"recover.file"}, nil)
	e.b.win.ProcessFailed(e.gen, 1)
	_, done := e.b.win.waitChans()
	recv(t, h.in["recover.file"], "the file write stalling")
	waitFor(t, func() bool { return !e.ctl.Snapshot().EditDialogOpen })
	if e.scr("a").Name != "Draft" {
		t.Fatalf("persistence does not wait for the file: %q", e.scr("a").Name)
	}
	notClosed(t, done, "cleanup before its deadline while the file is pending")
	e.clk.Advance(recoverBound + time.Millisecond)
	recv(t, done, "cleanup at the deadline despite the stalled file")
	ev := e.em.named(evEditorWindowFailed)
	if len(ev) != 1 || ev[0].payload.(windowFailedEvent).RecoveredPath != "" {
		t.Fatalf("no path can be reported for a pending file: %+v", ev)
	}
	close(h.release["recover.file"])
	waitFor(t, func() bool { return len(recoveryFiles(t, e.dir)) == 1 })
	// the completion/abandonment handshake: a write that lands after cleanup gave up is always logged
	waitFor(t, func() bool { return strings.Contains(buf.String(), "late recovery file for script a") })
}

// Item 4 (amendment D): a save paused before its commit check past the lease bound is still the
// newest draft: its payload was published at entry, so recovery writes it.
func TestE1bItem4SaveRejectedAfterTheLeaseBoundKeepsItsDraft(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Checkpoint")+`}`)
	h := hookStages(e, []string{"validated"}, []string{"lease.wait"})
	saveErr := make(chan error, 1)
	go func() {
		_, err := e.m["editor.save"](json.RawMessage(`{"sessionId":"` + e.sid + `","snapshotRev":3,"script":` + scriptJSON("a", "Paused") + `}`))
		saveErr <- err
	}()
	recv(t, h.in["validated"], "the save pausing before commit")
	e.b.win.ProcessFailed(e.gen, 1)
	_, done := e.b.win.waitChans()
	recv(t, h.reached["lease.wait"], "the lease wait")
	e.clk.Advance(saveLeaseWaitMax + time.Millisecond)
	recv(t, done, "cleanup after the lease bound")
	f := recoveryFiles(t, e.dir)
	if len(f) != 1 || f[0].Revision != 3 || f[0].Script.Name != "Paused" {
		t.Fatalf("the paused save's payload is the newest draft: %+v", f)
	}
	close(h.release["validated"])
	wantCode(t, <-saveErr, CodeSessionClosing)
	if e.scr("a").Name != "S a" {
		t.Fatalf("a save still in flight makes recovery file-only: %q", e.scr("a").Name)
	}
}

// Item 5 (amendment F): each cleanup owns a token; a later generation failing before it has a
// session neither consumes the older token nor releases the gate it still holds.
func TestE1bItem5CleanupTokensArePerCleanup(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Draft")+`}`)
	h := hookStages(e, []string{"recover.enqueue"}, nil)
	e.b.win.ProcessFailed(e.gen, 1)
	_, doneA := e.b.win.waitChans()
	recv(t, h.in["recover.enqueue"], "A's cleanup holding its token")

	res := e.mustCall(t, "editor.openWindow", `{"id":"a"}`)
	genB := uint64(res["gen"].(float64))
	e.b.win.WindowFailed(genB, "embed", "async") // B fails before any session exists
	_, doneB := e.b.win.waitChans()
	recv(t, doneB, "B's cleanup")
	e.b.win.cleanupDone(1 << 40) // an unknown token is a no-op
	if !e.ctl.Snapshot().EditDialogOpen {
		t.Fatal("B's failure released the gate A's cleanup still owns")
	}
	close(h.release["recover.enqueue"])
	recv(t, doneA, "A's cleanup")
	waitFor(t, func() bool { return !e.ctl.Snapshot().EditDialogOpen })
}

// Item 6 (amendment E): teardown detaches A synchronously, so B's editor.open during A's lease wait
// never sees A's session (no EDITOR_BUSY).
func TestE1bItem6ReopenDuringTheLeaseWaitNeverGetsEditorBusy(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	sidA := e.sid
	h := hookStages(e, []string{"validated"}, []string{"lease.wait"})
	go e.m["editor.save"](json.RawMessage(`{"sessionId":"` + sidA + `","snapshotRev":1,"script":` + scriptJSON("a", "Stuck") + `}`))
	recv(t, h.in["validated"], "A's save holding its lease")
	e.b.win.ProcessFailed(e.gen, 1)
	_, doneA := e.b.win.waitChans()
	recv(t, h.reached["lease.wait"], "A's cleanup waiting on the lease")
	if e.b.curSess() != nil || e.b.sessAtomic.Load() != nil {
		t.Fatal("A must be detached before teardown returns")
	}
	e.openAll(t, "a") // mustCall: EDITOR_BUSY would fail here
	if e.sid == sidA {
		t.Fatal("B must have its own session")
	}
	notClosed(t, doneA, "A's cleanup during its lease wait")
	e.clk.Advance(saveLeaseWaitMax + time.Millisecond)
	recv(t, doneA, "A's cleanup")
	close(h.release["validated"])
	if e.b.sessionIDForGen(e.gen) != e.sid || !e.ctl.Snapshot().EditDialogOpen {
		t.Fatal("A's cleanup touched B")
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// Item 7 (amendment H): the shutdown escape is explicit — with the destroy unconfirmed at D3+0.5s
// shutdown logs it and proceeds, and not one instant before.
func TestE1bItem7ShutdownEscapeIsExplicitAndLogged(t *testing.T) {
	buf := &syncBuf{}
	log.SetOutput(buf)
	defer log.SetOutput(os.Stderr)
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	ui := &heldUI{}
	e.b.d.UI = ui
	e.b.BeginShutdown(1500*time.Millisecond, func() {})
	e.mustCall(t, "app.closingReady", ``)
	e.clk.Advance(1500*time.Millisecond + time.Millisecond)
	waitFor(t, func() bool { return ui.n() >= 1 })
	e.clk.Advance(shutdownDestroy + shutdownDestroyGrace - 2*time.Millisecond) // D3+0.5s minus 1ms
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(buf.String(), "editor destroy not confirmed; proceeding") {
		t.Fatal("escaped before D3+0.5s")
	}
	e.clk.Advance(2 * time.Millisecond)
	waitFor(t, func() bool { return strings.Contains(buf.String(), "editor destroy not confirmed; proceeding") })
}

// Item 8 (amendment G): cleanup takes no unbounded lock and never waits on EndSession: with
// Bridge.mu, sessMu and the capture lock all held, teardown returns, main is re-enabled, the gate
// opens at the deadline, cleanup finishes, and the blocked recovery never persists afterwards.
func TestE1bItem8CleanupIsBoundedWithBridgeLocksAndCaptureEndBlocked(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Held Draft")+`}`)
	h := hookStages(e, nil, []string{"recover.enqueue", "recover.persisted"})
	e.b.sessMu.Lock()
	e.b.mu.Lock()
	e.b.capture.opMu.Lock()
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			e.b.capture.opMu.Unlock()
			e.b.mu.Unlock()
			e.b.sessMu.Unlock()
		}
	}
	defer unlock()
	td := make(chan struct{})
	go func() { e.b.win.ProcessFailed(e.gen, 1); close(td) }()
	recv(t, td, "teardown returning with the bridge locks held")
	if !e.h.mainEnabled {
		t.Fatal("main must be re-enabled")
	}
	_, done := e.b.win.waitChans()
	recv(t, h.reached["recover.enqueue"], "recovery blocked behind the locks")
	e.clk.Advance(recoverBound + time.Millisecond)
	recv(t, done, "cleanup at the deadline")
	if e.ctl.Snapshot().EditDialogOpen {
		t.Fatal("the gate must open at the deadline")
	}
	unlock()
	recv(t, h.reached["recover.persisted"], "the abandoned recovery returning")
	if hasQueued(e, "a=Held Draft") || e.scr("a").Name != "S a" {
		t.Fatalf("persisted after abandonment: %v", queuedNames(e))
	}
	waitFor(t, func() bool { return len(recoveryFiles(t, e.dir)) == 1 })
}

// Review fix 1 (amendment I): a save paused before it publishes its entry draft, resumed after
// recovery sealed the session and finished, is never dropped: it lands as a second, file-only
// recovery file with its own revision, and the save itself gets SESSION_CLOSING.
func TestE1bSealLatePublishAfterRecoveryIsWrittenAsItsOwnRecoveryFile(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "Checkpoint")+`}`)
	h := hookStages(e, []string{"publish"}, []string{"lease.wait", "recover.latefile"})
	saveErr := make(chan error, 1)
	go func() {
		_, err := e.m["editor.save"](json.RawMessage(`{"sessionId":"` + e.sid + `","snapshotRev":3,"script":` + scriptJSON("a", "Late Payload") + `}`))
		saveErr <- err
	}()
	recv(t, h.in["publish"], "the save paused before publishing")
	e.b.win.ProcessFailed(e.gen, 1)
	_, done := e.b.win.waitChans()
	recv(t, h.reached["lease.wait"], "the lease wait")
	e.clk.Advance(saveLeaseWaitMax + time.Millisecond)
	recv(t, done, "recovery sealed and finished")
	if f := recoveryFiles(t, e.dir); len(f) != 1 || f[0].Revision != 2 {
		t.Fatalf("the sealed snapshot is the checkpoint: %+v", f)
	}
	close(h.release["publish"])
	wantCode(t, <-saveErr, CodeSessionClosing)
	recv(t, h.reached["recover.latefile"], "the late payload's file")
	f := recoveryFiles(t, e.dir)
	late := false
	for _, env := range f {
		late = late || env.Revision == 3 && env.Script.Name == "Late Payload"
	}
	if len(f) != 2 || !late {
		t.Fatalf("the late payload must be a second recovery file: %+v", f)
	}
	if e.scr("a").Name != "S a" {
		t.Fatalf("file-only: %q", e.scr("a").Name)
	}
}

// Review fix 2 (amendment J): B's openWindow interleaving exactly between recovery's admission
// check and its enqueue waits on the admission lock, so recovery enqueues first and B's session
// opens on the recovered script and the store revision it produced.
func TestE1bAdmissionOpenBetweenRecoveryCheckAndEnqueueSeesTheRecoveredRevision(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	e.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e.sid+`","revision":2,"script":`+scriptJSON("a", "A Draft")+`}`)
	h := hookStages(e, []string{"recover.admit"}, []string{"recover.persisted"})
	e.b.win.ProcessFailed(e.gen, 1)
	_, doneA := e.b.win.waitChans()
	recv(t, h.in["recover.admit"], "recovery inside its admission")

	opened := make(chan uint64, 1)
	go func() {
		v, err := e.m["editor.openWindow"](json.RawMessage(`{"id":"a"}`))
		if err != nil {
			opened <- 0
			return
		}
		raw, _ := json.Marshal(v)
		var r struct{ Gen uint64 }
		_ = json.Unmarshal(raw, &r)
		opened <- r.Gen
	}()
	select {
	case <-opened:
		t.Fatal("B registered its script while recovery held the admission")
	case <-time.After(60 * time.Millisecond):
	}
	close(h.release["recover.admit"])
	recv(t, h.reached["recover.persisted"], "recovery deciding")
	recv(t, doneA, "A's cleanup")
	var genB uint64
	select {
	case genB = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("B's open never completed")
	}
	if genB == 0 || !hasQueued(e, "a=A Draft") {
		t.Fatalf("recovery enqueued first: gen=%d %v", genB, queuedNames(e))
	}
	e.gen = genB
	e.b.win.WindowReady(genB)
	e.mustCall(t, "editor.windowReady", ``)
	s := e.mustCall(t, "editor.open", `{"id":"a"}`)
	if s["script"].(map[string]any)["name"] != "A Draft" {
		t.Fatalf("B must open on the recovered script: %v", s["script"])
	}
	if sess := e.b.curSess(); sess == nil || sess.etag != e.store.Revision("a") {
		t.Fatal("B's etag is the revision recovery produced")
	}
}

// Two recovery files of one script written back to back (same millisecond) never replace each other.
func TestE1bRecoveryFilesInTheSameMillisecondDoNotOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "recovered")
	env := RecoveryEnvelope{V: 1, ScriptID: "a", Revision: 7, Script: edScript("a")}
	p1, err1 := writeRecovery(dir, env)
	p2, err2 := writeRecovery(dir, env)
	if err1 != nil || err2 != nil || p1 == p2 || len(recoveryFiles(t, dir)) != 2 {
		t.Fatalf("%q %q %v %v", p1, p2, err1, err2)
	}
}

// A page flooding saves after recovery sealed its session gets at most one late-payload writer:
// files stay within the per-script cap and the newest flooded revision is on disk.
func TestE1bSealFloodOfLateSavesUsesOneCoalescingWriter(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	sid := e.sid
	e.b.win.ProcessFailed(e.gen, 1)
	e.waitDone(t)
	base := runtime.NumGoroutine()
	peak := 0
	for i := 1; i <= 2000; i++ {
		e.m["editor.save"](json.RawMessage(`{"sessionId":"` + sid + `","snapshotRev":` + itoa(i) + `,"script":` + scriptJSON("a", "F"+itoa(i)) + `}`))
		if n := runtime.NumGoroutine() - base; n > peak {
			peak = n
		}
	}
	if peak > 5 {
		t.Fatalf("late payload writers are not coalesced: +%d goroutines", peak)
	}
	waitFor(t, func() bool {
		for _, f := range recoveryFiles(t, e.dir) {
			if f.Revision == 2000 {
				return true
			}
		}
		return false
	})
	sess := e.b.lastClosed.Load()
	waitFor(t, func() bool { sess.lateMu.Lock(); defer sess.lateMu.Unlock(); return !sess.lateRunning })
	if n := len(recoveryFiles(t, e.dir)); n > recoveredKeep { // each write prunes right after its rename
		t.Fatalf("%d files", n)
	}
}

// Review fix (reservation): two writers racing for the same base name (same ms, same revision) each
// reserve a distinct name with O_EXCL and both files survive; there is no hard-link path at all,
// so a filesystem without hard links (FAT/exFAT) behaves the same.
func TestE1bRecoveryReservationRacingWritersForTheSameBaseNameBothSurvive(t *testing.T) {
	fixed := time.UnixMilli(1_795_000_000_000)
	recoveryNow = func() time.Time { return fixed }
	defer func() { recoveryNow = time.Now }()
	dir := filepath.Join(t.TempDir(), "recovered")
	env := RecoveryEnvelope{V: 1, ScriptID: "a", Revision: 9, Script: edScript("a")}
	var wg sync.WaitGroup
	paths := make([]string, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			paths[i], errs[i] = writeRecovery(dir, env)
		}(i)
	}
	close(start)
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || paths[0] == paths[1] {
		t.Fatalf("%v %v %q %q", errs[0], errs[1], paths[0], paths[1])
	}
	if f := recoveryFiles(t, dir); len(f) != 2 {
		t.Fatalf("both files must survive: %+v", f)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 2 {
		t.Fatalf("no reservation or temp file may be left: %v", ents)
	}
}

// Review fix (prune order): six files in the same millisecond with revisions 95–100 — the prune
// orders numerically, so it keeps 96–100 (100 included) and drops 95.
func TestE1bPruneKeepsTheNewestByNumericMsRevisionOrder(t *testing.T) {
	fixed := time.UnixMilli(1_795_000_000_000)
	recoveryNow = func() time.Time { return fixed }
	defer func() { recoveryNow = time.Now }()
	dir := filepath.Join(t.TempDir(), "recovered")
	for rev := uint64(95); rev <= 100; rev++ {
		if _, err := writeRecovery(dir, RecoveryEnvelope{V: 1, ScriptID: "a", Revision: rev, Script: edScript("a")}); err != nil {
			t.Fatal(err)
		}
	}
	got := map[uint64]bool{}
	for _, f := range recoveryFiles(t, dir) {
		got[f.Revision] = true
	}
	if len(got) != 5 || got[95] || !got[100] || !got[96] {
		t.Fatalf("kept %v", got)
	}
	if _, ok := parseRecoveryName("a-b-1795000000000-0000000001.json", "a"); ok {
		t.Fatal("another script's file (id a-b) must not parse as script a's")
	}
}

type blockingLog struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (w *blockingLog) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

// Review fix (no logging under lateMu): with a log sink that blocks, a late publish stalls only its
// own caller after releasing lateMu, so a seal and a whole cleanup still finish by the deadline.
func TestE1bBlockingLogSinkNeverStallsSealOrCleanup(t *testing.T) {
	e := newEdRig(t, edScript("a"))
	e.openAll(t, "a")
	sess := e.b.curSess()
	sess.seal()
	w := &blockingLog{release: make(chan struct{}), entered: make(chan struct{})}
	log.SetOutput(w)
	defer log.SetOutput(os.Stderr)
	go e.b.lateRecoveryFile(sess, edScript("a"), 5)
	recv(t, w.entered, "the late publish blocking in the log sink")
	locked := make(chan struct{})
	go func() { sess.seal(); close(locked) }()
	recv(t, locked, "a seal while the log sink is blocked")

	e2 := newEdRig(t, edScript("b"))
	e2.openAll(t, "b")
	e2.mustCall(t, "editor.checkpoint", `{"sessionId":"`+e2.sid+`","revision":2,"script":`+scriptJSON("b", "Draft")+`}`)
	h2 := hookStages(e2, nil, []string{"recover.file"})
	e2.b.win.ProcessFailed(e2.gen, 1)
	_, done := e2.b.win.waitChans()
	recv(t, h2.reached["recover.file"], "e2's recovery armed")
	e2.clk.Advance(recoverBound + time.Millisecond)
	recv(t, done, "cleanup by its deadline with the log sink blocked")
	close(w.release)
	waitFor(t, func() bool { sess.lateMu.Lock(); defer sess.lateMu.Unlock(); return !sess.lateRunning })
	waitFor(t, func() bool { return len(recoveryFiles(t, e.dir)) == 1 && len(recoveryFiles(t, e2.dir)) == 1 })
}
