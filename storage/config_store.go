package storage

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/yanmad27/supcua-v2/model"
)

// ConfigStore handles loading and saving the app config JSON file.
type ConfigStore struct {
	filePath string
}

// NewConfigStore creates a ConfigStore for the given file path.
func NewConfigStore(filePath string) *ConfigStore {
	return &ConfigStore{filePath: filePath}
}

// Load reads the config from disk. Returns default config if file doesn't exist.
func (configStore *ConfigStore) Load() (model.AppConfig, error) {
	data, err := os.ReadFile(configStore.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.DefaultConfig(), nil
		}
		return model.AppConfig{}, err
	}

	appConfig := model.DefaultConfig()
	if err := json.Unmarshal(data, &appConfig); err != nil {
		return model.AppConfig{}, err
	}
	return appConfig, nil
}

// Save writes the config to disk as formatted JSON.
func (configStore *ConfigStore) Save(appConfig model.AppConfig) error {
	data, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configStore.filePath, data, 0644)
}
