package model

import (
	"encoding/json"
	"testing"
)

func TestOldConfigLoadsWithAutoActiveOff(t *testing.T) {
	loadedConfig := DefaultConfig()
	if err := json.Unmarshal([]byte(`{"scripts_dir":"./s","key_press_duration_ms":5}`), &loadedConfig); err != nil {
		t.Fatal(err)
	}
	if loadedConfig.AutoActiveEnabled || loadedConfig.AutoActiveProcessName != "" {
		t.Fatalf("auto active should be off and unnamed, got %+v", loadedConfig)
	}
	if loadedConfig.KeyPressDurationMs != 5 {
		t.Fatal("existing keys must still load")
	}
}

func TestAutoActiveConfigRoundTrip(t *testing.T) {
	original := DefaultConfig()
	original.AutoActiveEnabled = true
	original.AutoActiveProcessName = "game.exe"
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded := DefaultConfig()
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.AutoActiveEnabled || decoded.AutoActiveProcessName != "game.exe" {
		t.Fatalf("round trip lost fields: %+v", decoded)
	}
}
