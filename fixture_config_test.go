package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

// The -selftest-production gate seeds this file; if it is missing (e.g. gitignored) or reverts to
// defaults the gate would silently exercise the default config.
func TestProductionFixtureConfigIsPresentAndNonDefault(t *testing.T) {
	data, err := os.ReadFile("testdata/prod-fixture/config.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg := model.DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	def := model.DefaultConfig()
	if cfg.Theme != "light" || !cfg.AutoActiveEnabled || reflect.DeepEqual(cfg.GlobalHotkey, def.GlobalHotkey) {
		t.Fatalf("fixture config is not the seeded non-default one: %+v", cfg)
	}
	if want := (model.Hotkey{Modifiers: []string{"ctrl", "alt", "shift"}, Key: "f9"}); !reflect.DeepEqual(cfg.GlobalHotkey, want) {
		t.Fatalf("global hotkey = %+v", cfg.GlobalHotkey)
	}
}
