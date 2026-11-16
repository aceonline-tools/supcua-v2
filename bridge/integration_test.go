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
