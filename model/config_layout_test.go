package model

import (
	"encoding/json"
	"testing"
)

func TestWindowLayoutStale(t *testing.T) {
	for _, c := range []struct {
		layout int
		stale  bool
	}{{0, true}, {WindowLayoutVersion - 1, true}, {WindowLayoutVersion, false}, {WindowLayoutVersion + 1, false}} {
		if got := (AppConfig{WindowLayout: c.layout}).WindowLayoutStale(); got != c.stale {
			t.Errorf("layout %d: stale=%v want %v", c.layout, got, c.stale)
		}
	}
	if !DefaultConfig().WindowLayoutStale() {
		t.Error("a fresh config must start stale so the first saved size is stamped")
	}
}

func TestOldConfigLoadsWithoutLayoutFields(t *testing.T) {
	var c AppConfig
	if err := json.Unmarshal([]byte(`{"main_window_pos":{"x":80,"y":60,"width":1000,"height":700}}`), &c); err != nil {
		t.Fatal(err)
	}
	if !c.WindowLayoutStale() || c.SidebarCollapsed || c.MainWindowPos.X != 80 || c.MainWindowPos.Y != 60 {
		t.Errorf("unexpected %+v", c)
	}
	b, _ := json.Marshal(AppConfig{WindowLayout: WindowLayoutVersion, SidebarCollapsed: true})
	var back AppConfig
	if err := json.Unmarshal(b, &back); err != nil || back.WindowLayout != WindowLayoutVersion || !back.SidebarCollapsed {
		t.Errorf("round trip: %+v %v", back, err)
	}
}
