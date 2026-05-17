// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
)

// ItemConfig holds configuration for buying/selling a specific item SKU.
type ItemConfig struct {
	SKU          string            `json:"sku"`
	Name         string            `json:"name,omitempty"`
	MaxStock     int               `json:"max_stock"`
	MinStock     int               `json:"min_stock"`
	EnableBuy    bool              `json:"enable_buy"`
	EnableSell   bool              `json:"enable_sell"`
	MinBuyPrice  currency.Currency `json:"min_buy_price"`
	MaxBuyPrice  currency.Currency `json:"max_buy_price"`
	MinSellPrice currency.Currency `json:"min_sell_price"`
	MaxSellPrice currency.Currency `json:"max_sell_price"`
}

// Config is the top-level configuration loaded from a JSON file.
type Config struct {
	GlobalMaxStock  int                   `json:"global_max_stock"`
	DefaultMaxStock int                   `json:"default_max_stock"`
	Items           map[string]ItemConfig `json:"items"`
}

// ConfigManager handles thread-safe loading and querying of the trading configuration.
type ConfigManager struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// NewConfigManager loads a config manager from the specified path.
// If the file doesn't exist, it creates a default skeleton file.
func NewConfigManager(path string) (*ConfigManager, error) {
	cm := &ConfigManager{path: path}
	if err := cm.Load(); err != nil {
		return nil, err
	}

	return cm, nil
}

// Load reads and parses the JSON configuration.
func (cm *ConfigManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// If file doesn't exist, initialize and write a default template.
	if _, err := os.Stat(cm.path); os.IsNotExist(err) {
		cm.cfg = Config{
			GlobalMaxStock:  3000,
			DefaultMaxStock: 5,
			Items:           make(map[string]ItemConfig),
		}

		if err := os.MkdirAll(filepath.Dir(cm.path), 0o755); err != nil {
			return err
		}

		data, err := json.MarshalIndent(cm.cfg, "", "  ")
		if err != nil {
			return err
		}

		if err := os.WriteFile(cm.path, data, 0o644); err != nil {
			return err
		}

		return nil
	}

	data, err := os.ReadFile(cm.path)
	if err != nil {
		return err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	if cfg.Items == nil {
		cfg.Items = make(map[string]ItemConfig)
	}

	cm.cfg = cfg

	return nil
}

// GetConfig returns the full thread-safe copy of the trading configuration.
func (cm *ConfigManager) GetConfig() Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.cfg
}

// GetItemConfig returns configuration for a specific SKU.
func (cm *ConfigManager) GetItemConfig(sku string) (ItemConfig, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	item, ok := cm.cfg.Items[sku]

	return item, ok
}
