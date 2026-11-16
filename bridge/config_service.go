package bridge

import (
	"sync"

	"github.com/yanmad27/supcua-v2/model"
)

// ConfigPersister is the on-disk config store (storage.ConfigStore satisfies it).
type ConfigPersister interface {
	Load() (model.AppConfig, error)
	Save(model.AppConfig) error
}

// ConfigService serialises every config read-modify-write behind one mutex (§9.6 rule 3).
type ConfigService struct {
	mu      sync.Mutex
	store   ConfigPersister
	current model.AppConfig
}

// NewConfigService loads the config once; later reads come from memory.
func NewConfigService(store ConfigPersister) (*ConfigService, error) {
	cfg, err := store.Load()
	if err != nil {
		return nil, err
	}
	return &ConfigService{store: store, current: cloneConfig(cfg)}, nil
}

// Get returns a copy of the current config.
func (c *ConfigService) Get() model.AppConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneConfig(c.current)
}

// Update applies fn to a copy and persists it; the in-memory config only changes if the save succeeds.
func (c *ConfigService) Update(fn func(*model.AppConfig)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := cloneConfig(c.current)
	fn(&next)
	if err := c.store.Save(next); err != nil {
		return err
	}
	c.current = next
	return nil
}

func cloneConfig(cfg model.AppConfig) model.AppConfig {
	cfg.CollapsedGroups = append([]string(nil), cfg.CollapsedGroups...)
	return cfg
}
