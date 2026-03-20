// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package tf2schema provides a comprehensive TF2 item schema manager.
package tf2schema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andygrunwald/vdf"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/mitchellh/mapstructure"

	"golang.org/x/sync/errgroup"
)

const ModuleName string = "tf2_schema"

// SchemaManagerOption allows functional configuration of SchemaManager.
type SchemaManagerOption func(*SchemaManager)

type Config struct {
	UpdateInterval time.Duration // How often to refresh the schema
	LiteMode       bool          // Prunes unnecessary items_game data to save RAM
}

// SchemaManager manages the TF2 item schema, keeping it up to date.
type SchemaManager struct {
	bus    *bus.Bus
	client service.Requester
	logger log.Logger
	config Config

	mu     sync.RWMutex
	schema *Schema
	ready  atomic.Bool

	cancel context.CancelFunc
}

// Option allows functional configuration of SchemaManager.
type Option func(*SchemaManager)

func WithLogger(logger log.Logger) Option {
	return func(sm *SchemaManager) { sm.logger = logger }
}

// New creates a manager with the given options.
func New(cfg Config, opts ...Option) *SchemaManager {
	if cfg.UpdateInterval < 1*time.Minute {
		cfg.UpdateInterval = 24 * time.Hour
	}

	sm := &SchemaManager{
		config: cfg,
		logger: log.Discard,
	}
	for _, opt := range opts {
		opt(sm)
	}

	return sm
}

func (m *SchemaManager) Name() string { return ModuleName }

func (m *SchemaManager) Init(c *steam.Client) error {
	m.bus = c.Bus()
	if m.bus == nil {
		return errors.New("nil bus")
	}
	m.client = c.Service()
	if m.client == nil {
		return errors.New("nil API client")
	}
	return nil
}

// Start triggers the initial fetch and sets up the refresh loop.
func (m *SchemaManager) Start(ctx context.Context) error {
	m.logger.Info("Initializing TF2 Schema...")

	loopCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	// Initial fetch (blocking to ensure modules depending on schema can start)
	if m.schema == nil {
		if err := m.Refresh(loopCtx); err != nil {
			return fmt.Errorf("initial schema fetch failed: %w", err)
		}
	}

	m.ready.Store(true)
	m.bus.Publish(&SchemaReadyEvent{})

	go m.refreshLoop(loopCtx)

	return nil
}

// Get returns the current active schema. Returns nil if not ready.
func (m *SchemaManager) Get() *Schema {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.schema
}

// Refresh manually triggers a full schema update from Steam and GitHub sources.
func (m *SchemaManager) Refresh(ctx context.Context) error {
	m.logger.Debug("Fetching schema components in parallel...")

	var overview map[string]any
	var items []any
	var paintkits map[string]string
	var itemsGame map[string]any

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		overview, err = m.getSchemaOverview(gCtx)
		return err
	})
	g.Go(func() error {
		var err error
		items, err = m.getSchemaItems(gCtx)
		return err
	})
	g.Go(func() error {
		var err error
		paintkits, err = m.getPaintKits(gCtx)
		return err
	})
	g.Go(func() error {
		var err error
		itemsGame, err = m.getItemsGame(gCtx)
		return err
	})

	if err := g.Wait(); err != nil {
		m.bus.Publish(&SchemaUpdateFailedEvent{Error: err})
		return fmt.Errorf("schema fetch failed: %w", err)
	}

	err := m.buildSchema(overview, items, paintkits, itemsGame)
	if err != nil {
		return err
	}

	m.logger.Info("TF2 Schema successfully updated")
	m.bus.Publish(&SchemaUpdatedEvent{Timestamp: time.Now()})

	return nil
}

// buildSchema combines all fetched parts into a unified Schema object.
func (m *SchemaManager) buildSchema(overview map[string]any, items []any, paintKits map[string]string, itemsGame map[string]any) error {
	raw := &RawSchema{
		ItemsGame: itemsGame,
	}
	raw.Schema.PaintKits = paintKits

	overviewBytes, err := json.Marshal(overview)
	if err != nil {
		return fmt.Errorf("failed to encode overview map: %w", err)
	}
	if err := json.Unmarshal(overviewBytes, &raw.Schema); err != nil {
		return fmt.Errorf("failed to decode overview into struct: %w", err)
	}

	raw.Schema.Items = make([]*ItemSchema, 0, len(items))
	for _, itemBytes := range items {
		var item ItemSchema
		if err := mapstructure.Decode(itemBytes, &item); err != nil {
			m.logger.Warn("Failed to parse single item schema, skipping", log.Err(err))
			continue
		}
		raw.Schema.Items = append(raw.Schema.Items, &item)
	}

	if m.config.LiteMode {
		m.pruneItemsGame(raw)
	}

	newSchema := NewSchema(raw)

	m.mu.Lock()
	m.schema = newSchema
	m.mu.Unlock()

	return nil
}

// pruneItemsGame deletes unnecessary fields from the massive items_game map
// to save RAM. Used when LiteMode is true.
func (m *SchemaManager) pruneItemsGame(raw *RawSchema) {
	if raw.ItemsGame == nil {
		return
	}

	keysToRemove := []string{
		"game_info", "colors", "equip_regions_list", "equip_conflicts",
		"quest_objective_conditions", "item_series_types", "item_collections",
		"operations", "prefabs", "item_criteria_templates", "random_attribute_templates",
		"lootlist_job_template_definitions", "item_sets", "client_loot_lists",
		"revolving_loot_lists", "recipes", "achievement_rewards",
		"attribute_controlled_attached_particles", "armory_data", "item_levels",
		"kill_eater_score_types", "mvm_maps", "mvm_tours", "matchmaking_categories",
		"maps", "master_maps_list", "steam_packages", "community_market_item_remaps",
		"war_definitions",
	}

	for _, key := range keysToRemove {
		delete(raw.ItemsGame, key)
	}

	m.logger.Debug("LiteMode: pruned items_game data to save memory")
}

func (m *SchemaManager) getSchemaOverview(ctx context.Context) (map[string]any, error) {
	req := struct {
		Language string `url:"language"`
	}{"en"}
	resp, err := service.WebAPI[map[string]any](ctx, m.client, "GET", "IEconItems_440", "GetSchemaOverview", 1, req)
	if err != nil {
		return nil, fmt.Errorf("transport error: %w", err)
	}
	return *resp, nil
}

func (m *SchemaManager) getSchemaItems(ctx context.Context) ([]any, error) {
	var allItems []any
	next := 0

	for {
		req := struct {
			Language string `url:"language"`
			Start    int    `url:"start"`
		}{"en", next}
		resp, err := service.WebAPI[map[string]any](ctx, m.client, "GET", "IEconItems_440", "GetSchemaItems", 1, req)
		if err != nil {
			return nil, err
		}

		result, ok := (*resp)["result"].(map[string]any)
		if !ok {
			break
		}

		if items, ok := result["items"].([]any); ok {
			allItems = append(allItems, items...)
		}

		if nextVal, ok := result["next"].(float64); ok && nextVal > 0 {
			next = int(nextVal)
		} else {
			break
		}
	}

	return allItems, nil
}

func (m *SchemaManager) getPaintKits(ctx context.Context) (map[string]string, error) {
	url := "https://raw.githubusercontent.com/SteamDatabase/GameTracking-TF2/master/tf/resource/tf_proto_obj_defs_english.txt"
	req := api.NewHttpRequest("GET", url, nil)
	resp, err := m.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	parser := vdf.NewParser(strings.NewReader(string(resp.Body)))
	parsed, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse VDF: %w", err)
	}

	lang, ok := parsed["lang"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid VDF structure: missing 'lang'")
	}

	tokens, ok := lang["Tokens"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid VDF structure: missing 'Tokens'")
	}

	paintKits := make(map[string]string)
	seen := make(map[string]bool)

	for key, val := range tokens {
		parts := strings.SplitN(key, " ", 2)
		if len(parts) != 2 {
			continue
		}
		subparts := strings.Split(parts[0], "_")
		if len(subparts) != 3 || subparts[0] != "9" {
			continue
		}
		def := subparts[1]
		name, ok := val.(string)
		if !ok {
			continue
		}
		if strings.HasPrefix(name, def+":") {
			continue
		}
		if !seen[name] {
			paintKits[def] = name
			seen[name] = true
		}
	}

	return paintKits, nil
}

func (m *SchemaManager) getItemsGame(ctx context.Context) (map[string]any, error) {
	url := "https://raw.githubusercontent.com/SteamDatabase/GameTracking-TF2/master/tf/scripts/items/items_game.txt"
	req := api.NewHttpRequest("GET", url, nil)
	resp, err := m.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	parser := vdf.NewParser(strings.NewReader(string(resp.Body)))
	parsed, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse VDF: %w", err)
	}

	itemsGame, ok := parsed["items_game"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing 'items_game' in VDF")
	}

	return itemsGame, nil
}

func (m *SchemaManager) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Refresh(ctx); err != nil {
				m.logger.Error("Scheduled schema refresh failed", log.Err(err))
			}
		}
	}
}
