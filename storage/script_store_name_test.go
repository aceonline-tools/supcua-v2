package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

func TestNameOrFileTakenDetectsSanitizedCollision(t *testing.T) {
	scriptStore := NewScriptStore(t.TempDir())
	if err := scriptStore.Save(model.Script{ID: "a", Name: "Farm copy"}); err != nil {
		t.Fatal(err)
	}

	if scriptStore.NameExists("Farm (copy)", "") {
		t.Fatal("precondition: NameExists should not see the sanitized collision")
	}
	if !scriptStore.NameOrFileTaken("Farm (copy)") {
		t.Error("expected Farm (copy) to be taken via shared file farm_copy.json")
	}
	if !scriptStore.NameOrFileTaken("farm copy") {
		t.Error("expected case-insensitive name match")
	}
	if scriptStore.NameOrFileTaken("Farm (copy 2)") {
		t.Error("Farm (copy 2) should be free")
	}
}

func TestNameOrFileTakenDetectsOrphanFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "orphan.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if !NewScriptStore(directory).NameOrFileTaken("Orphan") {
		t.Error("expected existing unrelated file to count as taken")
	}
}

func TestCloneDoesNotOverwriteSanitizedCollision(t *testing.T) {
	scriptStore := NewScriptStore(t.TempDir())
	scriptStore.Save(model.Script{ID: "a", Name: "Farm"})
	scriptStore.Save(model.Script{ID: "b", Name: "Farm copy"})

	clone := model.CloneScript(model.Script{ID: "a", Name: "Farm"}, scriptStore.NameOrFileTaken)
	if clone.Name != "Farm (copy 2)" {
		t.Fatalf("name = %q, want Farm (copy 2)", clone.Name)
	}
	if err := scriptStore.Save(clone); err != nil {
		t.Fatal(err)
	}
	scripts, _ := scriptStore.List()
	if len(scripts) != 3 {
		t.Errorf("got %d scripts, want 3", len(scripts))
	}
}

func TestNameOrFileTakenSeesPendingQueue(t *testing.T) {
	scriptStore := NewScriptStore(t.TempDir())
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	beforeQueuedWrite = func(string) {
		once.Do(func() { close(started) })
		<-release
	}
	defer func() { beforeQueuedWrite = nil }()

	scriptStore.QueueSave(model.Script{ID: "a", Name: "Blocker"})
	<-started
	scriptStore.QueueSave(model.Script{ID: "b", Name: "Farm (copy)"})

	if scriptStore.NameExists("Farm (copy)", "") {
		t.Fatal("precondition: pending script must not be on disk yet")
	}
	if !scriptStore.NameOrFileTaken("farm (COPY)") {
		t.Error("expected case-insensitive match against the pending queue")
	}
	if !scriptStore.NameOrFileTaken("Farm copy") {
		t.Error("expected sanitized-file collision against the pending queue")
	}
	if scriptStore.NameOrFileTaken("Farm (copy 2)") {
		t.Error("unrelated name must be free")
	}
	close(release)
	scriptStore.FlushSaves(2 * time.Second)
}

func TestOnSaveErrorHookReportsFailedQueuedWrite(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "scripts")
	if err := os.WriteFile(blocked, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	scriptStore := NewScriptStore(blocked)
	got := make(chan string, 1)
	scriptStore.SetOnSaveError(func(s model.Script, err error) {
		if err != nil {
			got <- s.ID
		}
	})
	scriptStore.QueueSave(model.Script{ID: "x1", Name: "X"})
	scriptStore.FlushSaves(2 * time.Second)
	select {
	case id := <-got:
		if id != "x1" {
			t.Errorf("hook got id %q", id)
		}
	default:
		t.Fatal("expected OnSaveError to fire")
	}
}

func TestNameOrFileTakenCountsQueuedSave(t *testing.T) {
	scriptStore := NewScriptStore(t.TempDir())
	popped, release := stallWorker(t, "q0")
	scriptStore.QueueSave(model.Script{ID: "q0", Name: "Stall"})
	<-popped
	scriptStore.QueueSave(model.Script{ID: "a", Name: "Farm copy"})

	if scriptStore.NameExists("Farm (copy)", "") {
		t.Fatal("precondition: NameExists must not see the queued script")
	}
	if !scriptStore.NameOrFileTaken("Farm (copy)") {
		t.Error("expected pending save to make Farm (copy) taken")
	}
	clone := model.CloneScript(model.Script{ID: "a", Name: "Farm"}, scriptStore.NameOrFileTaken)
	if clone.Name != "Farm (copy 2)" {
		t.Fatalf("name = %q, want Farm (copy 2)", clone.Name)
	}
	release()
	if !scriptStore.FlushSaves(5 * time.Second) {
		t.Fatal("flush timed out")
	}
}

func TestNameOrFileTakenCountsInFlightSave(t *testing.T) {
	scriptStore := NewScriptStore(t.TempDir())
	popped, release := stallWorker(t, "a")
	scriptStore.QueueSave(model.Script{ID: "a", Name: "Farm copy"})
	<-popped
	if !scriptStore.NameOrFileTaken("Farm (copy)") {
		t.Error("expected in-flight save to make Farm (copy) taken")
	}
	release()
	if !scriptStore.FlushSaves(5 * time.Second) {
		t.Fatal("flush timed out")
	}
	if !scriptStore.NameOrFileTaken("Farm (copy)") {
		t.Error("expected written file to keep name taken")
	}
}
