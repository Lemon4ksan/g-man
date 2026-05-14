// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/andygrunwald/vdf"
	"github.com/mitchellh/mapstructure"
	"golang.org/x/sync/errgroup"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

// ModuleName is the name of the schema manager module.
const ModuleName string = "tf2_schema"

// Config holds the configuration for the schema manager.
type Config struct {
	// UpdateInterval is the time interval between schema updates.
	UpdateInterval time.Duration
	// LiteMode enables pruning of unnecessary items_game data to save RAM.
	LiteMode bool
	// CachePath is the path to the local schema cache file.
	CachePath string
	// SchemaMirrorURL is the URL to use for fetching schema data in case the default URL is not reachable.
	SchemaMirrorURL string
	// PaintKitMirrorURL is the URL to use for fetching paintkit data in case the default URL is not reachable.
	PaintKitMirrorURL string
	// ItemsMirrorURL is the URL to use for fetching items data in case the default URL is not reachable.
	ItemsMirrorURL string
	// ItemsGameURL is the URL to use for fetching items_game.txt data.
	ItemsGameURL string
}

// DefaultConfig returns a default configuration for the schema manager.
func DefaultConfig() Config {
	return Config{
		UpdateInterval:    24 * time.Hour,
		LiteMode:          false,
		CachePath:         "cache/tf2/schema.json",
		PaintKitMirrorURL: "https://raw.githubusercontent.com/SteamDatabase/GameTracking-TF2/master/tf/resource/tf_proto_obj_defs_english.txt",
		ItemsGameURL:      "https://raw.githubusercontent.com/SteamDatabase/GameTracking-TF2/master/tf/scripts/items/items_game.txt",
	}
}

// WithModule returns a steam.Option that registers the schema manager with the given configuration.
func WithModule(cfg Config) steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New(cfg))
	}
}

// Manager manages the TF2 item schema, keeping it up to date.
// It embeds BaseModule for standardized lifecycle and concurrency management.
type Manager struct {
	module.Base

	config     Config
	svcClient  service.Doer
	restClient rest.Requester

	mu     sync.RWMutex
	schema *schema.Schema
}

// New creates a manager with the given options.
func New(cfg Config) *Manager {
	if cfg.UpdateInterval < 1*time.Minute {
		cfg.UpdateInterval = 24 * time.Hour
	}

	return &Manager{
		Base:   module.New(ModuleName),
		config: cfg,
	}
}

// Name returns the name of the module.
func (m *Manager) Name() string { return ModuleName }

// Init initializes the manager with the given context.
func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.svcClient = init.Service()
	m.restClient = init.Rest()

	return nil
}

// StartAuthed triggers the initial fetch and sets up the refresh loop.
func (m *Manager) StartAuthed(ctx context.Context, _ module.AuthContext) error {
	m.Logger.Info("Starting TF2 Schema loading...")

	// Listen for manual update requests (e.g. from GC)
	sub := m.Bus.Subscribe(&schema.UpdateRequestedEvent{})

	m.Go(func(ctx context.Context) {
		defer sub.Unsubscribe()

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub.C():
				if !ok {
					return
				}

				req := ev.(*schema.UpdateRequestedEvent)
				m.handleUpdateRequested(req)
			}
		}
	})

	// The first run: try cache first, then refresh if needed.
	if err := m.loadFromCache(); err != nil {
		m.Logger.Info("Cache not available or invalid, performing full refresh", log.Err(err))

		if err := m.Refresh(ctx); err != nil {
			return fmt.Errorf("initial schema fetch failed: %w", err)
		}
	} else {
		m.Logger.Info("Schema loaded from cache",
			log.Time("time", m.schema.Time),
			log.Int("items", len(m.schema.Raw.ItemsGame)),
		)
	}

	m.Bus.Publish(&schema.ReadyEvent{})

	m.Go(func(moduleCtx context.Context) {
		m.refreshLoop(moduleCtx)
	})

	return nil
}

func (m *Manager) handleUpdateRequested(req *schema.UpdateRequestedEvent) {
	m.Logger.Info("Schema update requested",
		log.Uint32("version", req.Version),
		log.String("url", req.ItemsGameURL),
	)

	// Trigger a refresh in a separate goroutine to avoid blocking the bus
	m.Go(func(ctx context.Context) {
		if err := m.Refresh(ctx); err != nil {
			m.Logger.Error("Manual schema refresh failed", log.Err(err))
		}
	})
}

// Get returns the current active schema. Returns nil if not ready.
func (m *Manager) Get() *schema.Schema {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.schema
}

// Refresh manually triggers a full schema update from Steam and GitHub sources.
func (m *Manager) Refresh(ctx context.Context) error {
	m.Logger.Debug("Fetching schema components from Steam and GitHub...")

	overview, err := m.getSchemaOverview(ctx)
	if err != nil {
		return err
	}

	items, err := m.getSchemaItems(ctx)
	if err != nil {
		return err
	}

	var (
		paintkits map[string]string
		itemsGame map[string]any
	)

	g, gCtx := errgroup.WithContext(ctx)

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
		m.Bus.Publish(&schema.UpdateFailedEvent{Error: err})
		return fmt.Errorf("parallel fetch failed: %w", err)
	}

	if err := m.buildSchema(overview, items, paintkits, itemsGame); err != nil {
		return err
	}

	if err := m.saveToCache(); err != nil {
		m.Logger.Warn("Failed to save schema to cache", log.Err(err))
	}

	m.Logger.Info("TF2 Schema updated successfully", log.Int("items", len(m.schema.Raw.ItemsGame)))
	m.Bus.Publish(&schema.UpdatedEvent{Timestamp: time.Now()})

	return nil
}

func (m *Manager) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Refresh(ctx); err != nil {
				m.Logger.Error("Scheduled schema refresh failed", log.Err(err))
			}
		}
	}
}

func (m *Manager) buildSchema(
	overview map[string]any,
	items []any,
	paintKits map[string]string,
	itemsGame map[string]any,
) error {
	raw := &schema.Raw{
		ItemsGame: itemsGame,
	}
	raw.Schema.PaintKits = paintKits

	overviewBytes, _ := json.Marshal(overview)
	if err := json.Unmarshal(overviewBytes, &raw.Schema); err != nil {
		return fmt.Errorf("failed to parse schema overview: %w", err)
	}

	version := ""
	if res, ok := overview["result"].(map[string]any); ok {
		if url, ok := res["items_game_url"].(string); ok {
			version = url // Use URL as version for now as it contains the hash
		}
	}

	strPool := make(map[string]string)
	intern := func(s string) string {
		if s == "" {
			return ""
		}

		if val, ok := strPool[s]; ok {
			return val
		}

		strPool[s] = s

		return s
	}

	raw.Schema.Items = make([]*schema.Item, 0, len(items))
	for _, it := range items {
		var item schema.Item
		if err := mapstructure.Decode(it, &item); err == nil {
			item.ItemClass = intern(item.ItemClass)
			item.CraftClass = intern(item.CraftClass)
			item.ItemName = intern(item.ItemName)

			for i, class := range item.UsedByClasses {
				item.UsedByClasses[i] = intern(class)
			}

			raw.Schema.Items = append(raw.Schema.Items, &item)
		}
	}

	strPool = nil

	if m.config.LiteMode {
		m.pruneItemsGame(raw)
	}

	newSchema := schema.New(raw)
	newSchema.Version = version
	newSchema.Time = time.Now()

	m.mu.Lock()
	m.schema = newSchema
	m.mu.Unlock()

	debug.FreeOSMemory()

	return nil
}

// pruneItemsGame deletes unnecessary fields from the massive items_game map
// to save RAM. Used when LiteMode is true.
func (m *Manager) pruneItemsGame(raw *schema.Raw) {
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

	m.Logger.Debug("LiteMode: pruned items_game data to save memory")
}

func (m *Manager) getSchemaOverview(ctx context.Context) (map[string]any, error) {
	req := struct {
		Language string `url:"language"`
	}{"English"}

	resp, err := service.WebAPI[map[string]any](ctx, m.svcClient, "GET", "IEconItems_440", "GetSchemaOverview", 1, req)
	if err != nil {
		if m.isForbiddenError(err) {
			m.Logger.Warn("WebAPI returned 403. Attempting to fetch Overview from community mirror...")
			return m.fetchFromMirror(ctx, "overview")
		}

		return nil, fmt.Errorf("overview fetch failed: %w", err)
	}

	return *resp, nil
}

func (m *Manager) getSchemaItems(ctx context.Context) ([]any, error) {
	var allItems []any

	next := 0

	for {
		var resp *map[string]any

		err := m.withRetry(ctx, func() error {
			req := struct {
				Language string `url:"language"`
				Start    int    `url:"start"`
			}{"English", next}

			var err error

			resp, err = service.WebAPI[map[string]any](
				ctx,
				m.svcClient,
				"GET",
				"IEconItems_440",
				"GetSchemaItems",
				1,
				req,
			)

			return err
		})
		if err != nil {
			if m.isForbiddenError(err) {
				return m.fetchItemsFromMirror(ctx)
			}

			return nil, err
		}

		result, ok := (*resp)["result"].(map[string]any)
		if !ok {
			break
		}

		if items, ok := result["items"].([]any); ok {
			allItems = append(allItems, items...)
			m.Logger.Debug("Items progress", log.Int("count", len(allItems)))
		}

		nextVal, hasNext := result["next"].(float64)
		if !hasNext || nextVal <= 0 {
			break
		}

		next = int(nextVal)
	}

	return allItems, nil
}

func (m *Manager) getPaintKits(ctx context.Context) (map[string]string, error) {
	url := m.config.PaintKitMirrorURL

	resp, err := m.restClient.Request(ctx, "GET", url, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch paint kits: %w", err)
	}

	if resp == nil {
		return nil, errors.New("received nil response while fetching paint kits")
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github returned status: %d", resp.StatusCode)
	}

	parser := vdf.NewParser(resp.Body)

	parsed, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse VDF: %w", err)
	}

	lang, ok := parsed["lang"].(map[string]any)
	if !ok {
		return nil, errors.New("invalid VDF structure: missing 'lang'")
	}

	tokens, ok := lang["Tokens"].(map[string]any)
	if !ok {
		return nil, errors.New("invalid VDF structure: missing 'Tokens'")
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

func (m *Manager) getItemsGame(ctx context.Context) (map[string]any, error) {
	url := m.config.ItemsGameURL

	resp, err := m.restClient.Request(ctx, "GET", url, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch items_game.txt: %w", err)
	}

	if resp == nil {
		return nil, errors.New("received nil response while fetching items_game.txt")
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github returned status: %d", resp.StatusCode)
	}

	parser := vdf.NewParser(resp.Body)

	parsed, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse VDF: %w", err)
	}

	itemsGame, ok := parsed["items_game"].(map[string]any)
	if !ok {
		return nil, errors.New("missing 'items_game' in VDF")
	}

	return itemsGame, nil
}

func (m *Manager) isForbiddenError(err error) bool {
	var apiErr *api.SteamAPIError
	if errors.As(err, &apiErr) {
		return true
	}

	restErr := &rest.APIError{}
	if errors.As(err, &restErr) {
		return true
	}

	return strings.Contains(err.Error(), "403")
}

func (m *Manager) fetchFromMirror(ctx context.Context, component string) (map[string]any, error) {
	var url string

	switch component {
	case "overview":
		url = m.config.SchemaMirrorURL
	default:
		return nil, fmt.Errorf("unknown mirror component: %s", component)
	}

	if url == "" {
		return nil, fmt.Errorf("mirror URL for %s not configured", component)
	}

	res, err := rest.GetJSON[map[string]any](ctx, m.restClient, url, nil)
	if err != nil {
		return nil, fmt.Errorf("mirror fetch failed: %w", err)
	}

	return *res, nil
}

func (m *Manager) fetchItemsFromMirror(ctx context.Context) ([]any, error) {
	url := m.config.ItemsMirrorURL

	res, err := rest.GetJSON[[]any](ctx, m.restClient, url, nil)
	if err != nil {
		return nil, fmt.Errorf("mirror items fetch failed: %w", err)
	}

	return *res, nil
}

func (m *Manager) withRetry(ctx context.Context, operation func() error) error {
	const maxRetries = 3

	backoff := 2 * time.Second

	var lastErr error

	for i := range maxRetries {
		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err
		if !m.isRetryable(err) {
			return err
		}

		m.Logger.Warn("Operation failed, retrying...",
			log.Err(err),
			log.Int("attempt", i+1),
			log.Duration("backoff", backoff),
		)

		select {
		case <-time.After(backoff):
			backoff *= 2
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return fmt.Errorf("after %d attempts: %w", maxRetries, lastErr)
}

func (m *Manager) isRetryable(err error) bool {
	if strings.Contains(err.Error(), "invalid character '<'") {
		return true
	}

	if strings.Contains(err.Error(), "429") {
		return true
	}

	if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "connection refused") {
		return true
	}

	return false
}

func (m *Manager) saveToCache() error {
	if m.config.CachePath == "" {
		return nil
	}

	m.mu.RLock()
	s := m.schema
	m.mu.RUnlock()

	if s == nil {
		return nil
	}

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	return writeFile(m.config.CachePath, data)
}

func (m *Manager) loadFromCache() error {
	if m.config.CachePath == "" {
		return errors.New("cache path not configured")
	}

	data, err := readFile(m.config.CachePath)
	if err != nil {
		return err
	}

	var s schema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Simple validation
	if s.Raw == nil || s.Raw.ItemsGame == nil {
		return errors.New("cached schema is incomplete")
	}

	m.mu.Lock()
	m.schema = &s
	m.mu.Unlock()

	return nil
}

func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
