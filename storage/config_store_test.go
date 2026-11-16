package storage

import (
	"path/filepath"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

func TestSaveAndLoadConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	configStore := NewConfigStore(configPath)

	originalConfig := model.DefaultConfig()
	originalConfig.OverlayEnabled = false

	err := configStore.Save(originalConfig)
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loadedConfig, err := configStore.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loadedConfig.OverlayEnabled != false {
		t.Errorf("expected overlay_enabled=false, got %v", loadedConfig.OverlayEnabled)
	}
	if loadedConfig.GlobalHotkey.Key != "rctrl" {
		t.Errorf("expected global hotkey key=rctrl, got %s", loadedConfig.GlobalHotkey.Key)
	}
}

func TestLoadConfigFileNotExist(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "missing.json")
	configStore := NewConfigStore(configPath)

	config, err := configStore.Load()
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}

	defaultConfig := model.DefaultConfig()
	if config.GlobalHotkey.Key != defaultConfig.GlobalHotkey.Key {
		t.Errorf("expected default config, got: %+v", config)
	}
}
