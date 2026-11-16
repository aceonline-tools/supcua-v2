package bridge

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/autoactive"
	"github.com/yanmad27/supcua-v2/storage"
)

var (
	_ Store           = (*storage.ScriptStore)(nil)
	_ Controller      = (*app.Controller)(nil)
	_ ConfigPersister = (*storage.ConfigStore)(nil)
	_ AutoActive      = (*autoactive.Watcher)(nil)
	_ Config          = (*ConfigService)(nil)
)

func TestRealStoreRoundTripAndShutdownFlush(t *testing.T) {
	dir := t.TempDir()
	store := storage.NewScriptStore(filepath.Join(dir, "scripts"))
	if err := store.Save(script("a", "Alpha", "g", false)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(script("b", "Beta", "g", false)); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewConfigService(storage.NewConfigStore(filepath.Join(dir, "config.json")))
	if err != nil {
		t.Fatal(err)
	}
	b := New(Deps{Controller: app.NewController(nil, nil, nil, nil), Store: store, Config: cfg}, &recEmitter{})
	m := b.Methods()
	if _, err := m["scripts.setGroupEnabled"]([]byte(`{"group":"g","enabled":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := b.Shutdown(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	list, _ := store.List()
	if len(list) != 2 || !list[0].Enabled || !list[1].Enabled {
		t.Errorf("flush on shutdown must persist both scripts: %+v", list)
	}
	if _, err := m["scripts.delete"]([]byte(`{"id":"a"}`)); err != nil {
		t.Fatal(err)
	}
	if list, _ = store.List(); len(list) != 1 {
		t.Errorf("delete did not remove the file: %+v", list)
	}
}

func TestRefreshOverlayRereadsAndEmitsState(t *testing.T) {
	r := newRig(t)
	before := len(r.em.named(EvState))
	r.ov.mu.Lock()
	r.ov.state = OverlayDTO{Enabled: true, Visible: false, Draggable: true, X: 123, Y: 456}
	r.ov.mu.Unlock()
	r.b.RefreshOverlay()
	ev := r.em.named(EvState)
	if len(ev) != before+1 {
		t.Fatalf("want one new state event, got %d", len(ev)-before)
	}
	if got := ev[len(ev)-1].payload.(StateDTO).Overlay; got != r.ov.State() {
		t.Errorf("overlay in state = %+v", got)
	}
}

func TestEditorSaveNameTakenForNewButNotOwnID(t *testing.T) {
	r := newRig(t, script("s1", "Alpha", "", true))
	r.store.taken["Alpha"] = true // as when s1's own write is in flight
	sid, edit := openEditor(t, r, "s1")
	body := mustJSON(map[string]any{"sessionId": sid, "script": edit})
	if _, err := r.call(t, "editor.save", body); err != nil {
		t.Fatalf("own name must not count as taken: %v", err)
	}
	r.mustCall(t, "editor.close", `{"sessionId":"`+sid+`","autosave":false}`)

	sid, edit = openEditor(t, r, "")
	edit["name"] = "Alpha"
	_, err := r.call(t, "editor.save", mustJSON(map[string]any{"sessionId": sid, "script": edit}))
	wantCode(t, err, CodeNameTaken)
	r.mustCall(t, "editor.close", `{"sessionId":"`+sid+`","autosave":false}`)

	r.mustCall(t, "scripts.retrySave", `{"id":"s1"}`)
}
