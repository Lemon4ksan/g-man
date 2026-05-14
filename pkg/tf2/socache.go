// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"context"
	"encoding/binary"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/tf2"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// EconItemFlag represents bitmask flags for Steam Econ items (GC).
type EconItemFlag uint32

const (
	// EconItemFlag_CannotTrade indicates the item cannot be traded.
	EconItemFlag_CannotTrade EconItemFlag = 1 << iota
	// EconItemFlag_CannotBeUsedInCrafting indicates the item cannot be used in crafting.
	EconItemFlag_CannotBeUsedInCrafting
	// EconItemFlag_CanBeTradedByFreeAccounts indicates the item can be traded even if the account is not premium.
	EconItemFlag_CanBeTradedByFreeAccounts
	// EconItemFlag_NonEconomy indicates the item is a non-economy item (e.g., achievement items).
	EconItemFlag_NonEconomy
	// EconItemFlag_PurchasedAfterStoreCraftabilityChanges2012 relates to store items bought after the 2012 craftability update.
	EconItemFlag_PurchasedAfterStoreCraftabilityChanges2012
	// EconItemFlag_ForceBlueTeam indicates the item is forced to blue team (client-only).
	EconItemFlag_ForceBlueTeam
	// EconItemFlag_StoreItem indicates the item was bought from the Mann Co. Store.
	EconItemFlag_StoreItem
	// EconItemFlag_Preview indicates the item is a preview item from the store.
	EconItemFlag_Preview
)

// HasFlag checks if the provided flag is set in the bitmask.
func (f EconItemFlag) HasFlag(flag EconItemFlag) bool {
	return (f & flag) != 0
}

// Attribute IDs from items_game.txt
const (
	AttrCustomName         uint32 = 111
	AttrCustomDesc         uint32 = 112
	AttrMedalNumber        uint32 = 133
	AttrUnusualEffect      uint32 = 134
	AttrPaintPrimary       uint32 = 142
	AttrCannotTrade        uint32 = 153
	AttrCannotCraft        uint32 = 186
	AttrCrateSeries        uint32 = 187
	AttrAlwaysTradable     uint32 = 195
	AttrTradableAfter      uint32 = 211
	AttrKillEater          uint32 = 214 // Strange counter presence
	AttrCraftNumber        uint32 = 229
	AttrPaintSecondary     uint32 = 261
	AttrStrangePart1       uint32 = 380
	AttrStrangePart2       uint32 = 382
	AttrStrangePart3       uint32 = 384
	AttrCannotCraftVariant uint32 = 449
	AttrEOTLEarlySupporter uint32 = 703
	AttrWear               uint32 = 725
	AttrPaintkit           uint32 = 834
	AttrSpell1             uint32 = 1004
	AttrSpell2             uint32 = 1005
	AttrSpell3             uint32 = 1006
	AttrSpell4             uint32 = 1007
	AttrSpell5             uint32 = 1008
	AttrSpell6             uint32 = 1009
	AttrTarget             uint32 = 2012 // Target DefIndex for Kits/Strangifiers
	AttrKillstreaker       uint32 = 2013 // Professional Killstreak Eye Effect
	AttrSheen              uint32 = 2014 // Specialized/Professional Killstreak Sheen
	AttrKillstreakTier     uint32 = 2025 // 1: Basic, 2: Specialized, 3: Professional
	AttrAustralium         uint32 = 2027
	AttrSeries             uint32 = 2031 // Secondary series (Duck Journal)
	AttrTauntUnusualEffect uint32 = 2041
	AttrQuestLoanerIDLow   uint32 = 2051
	AttrQuestLoanerIDHigh  uint32 = 2052
	AttrFestivized         uint32 = 2053
)

// Killstreak tier
const (
	KillstreakTierNone uint32 = iota
	KillstreakTierBasic
	KillstreakTierSpecialized
	KillstreakTierProfessional
)

// Item Origins
const (
	OriginDrop        uint32 = 0
	OriginAchievement uint32 = 1
	OriginPurchase    uint32 = 2
	OriginStorePromo  uint32 = 5
	OriginSupport     uint32 = 7
	OriginHalloween   uint32 = 12
	OriginForeign     uint32 = 14
	OriginPreview     uint32 = 17
	OriginWorkshop    uint32 = 18
	OriginLoaner      uint32 = 24
)

// Quality constants for TF2 items
const (
	QualityNormal uint32 = iota
	QualityGenuine
	QualityVintage
	QualityUnusual
	QualityUnique
	QualityCommunity
	QualityValve
	QualitySelfMade
	QualityCustomized
	QualityStrange
	QualityCompleted
	QualityHaunted
	QualityCollectors
	QualityDecorated
)

// Item represents a parsed and normalized TF2 item with all its economic and technical metadata.
type Item struct {
	// Base Properties
	ID         uint64       // Unique Asset ID (changes when traded/marketed)
	OriginalID uint64       // The ID assigned when the item was first created
	AccountID  uint32       // Steam Account ID of the current owner
	DefIndex   uint32       // Item definition index from items_game.txt (e.g., 5021 for Key)
	Level      uint32       // Item level (0-100), mostly cosmetic
	Quality    uint32       // Quality ID (e.g., 6: Unique, 11: Strange, 5: Unusual)
	Inventory  uint32       // Raw inventory bitmask containing backpack position
	Quantity   uint32       // Stack size (usually 1)
	Origin     uint32       // Origin ID (e.g., 0: Drop, 2: Purchase, 24: Loaner)
	Flags      EconItemFlag // Bitmask for restrictions (Cannot Trade, etc.)
	Style      uint32       // Selected style index for items with multiple variants
	InUse      bool         // Whether the item is currently equipped or being used

	// Customization
	CustomName string // Text from a Name Tag
	CustomDesc string // Text from a Description Tag
	SKU        string // Canonical SKU string (e.g., "5021;6") for pricing

	// Trade/Craft Status
	IsTradable   bool // Can be traded via Steam
	IsMarketable bool // Can be listed on the Steam Community Market
	IsCraftable  bool // Can be used in crafting recipes

	// Specialized Attributes (Standard)
	Effect         uint32  // Unusual effect ID (e.g., 13: Sunbeams)
	KillstreakTier uint32  // Killstreak tier (1: Basic, 2: Specialized, 3: Professional)
	Australium     bool    // True if the item is an Australium variant
	Festivized     bool    // True if a Festivizer has been applied
	Wear           float32 // Skin wear value (0.0 to 1.0, where 0 is Factory New)
	Paintkit       uint32  // Skin pattern ID (War Paint index)
	CrateSeries    uint32  // Series number for supply crates (Attribute 187)
	Paint          uint32  // Applied paint color ID

	// Advanced & Rare Attributes
	Sheen          uint32      // Killstreak sheen effect (e.g., Team Shine)
	Killstreaker   uint32      // Professional Killstreak eye effect (e.g., Cerebral Discharge)
	CraftNumber    uint32      // Limited edition craft number (e.g., #1)
	Series         uint32      // Secondary series ID (used for Duck Journals/EOTL levels)
	MedalNumber    uint32      // Number assigned to tournament medals
	Target         uint32      // DefIndex of the target item (for Strangifiers/KS Kits)
	IsElevated     bool        // True if the item has a Strange counter but isn't Strange quality
	EarlySupporter bool        // True if the item has the "Early Supporter of EOTL" tag (Attr 703)
	QuestID        uint64      // 64-bit ID of the quest/contract linked to a loaner item
	IsBuggedLoaner bool        // True if item has Origin 24 but is actually tradable (rare glitch)
	Spells         []sku.Spell // List of applied Halloween spells (IDs 1004-1009)
	Parts          []uint32    // List of Strange Part IDs (counters applied to the item)
}

// Position returns the item's slot in the backpack.
// The lower 16 bits of the Inventory field represent the position.
func (i *Item) Position() uint32 {
	return i.Inventory & 0xFFFF
}

// GetSchema returns data about an item from the provided schema.
func (i *Item) GetSchema(s *schema.Schema) *schema.Item {
	return s.GetItemByDef(int(i.DefIndex))
}

// IsWeapon checks if an item is a weapon using the schema.
func (i *Item) IsWeapon(s *schema.Schema) bool {
	sch := i.GetSchema(s)
	return sch != nil && sch.CraftClass == "weapon"
}

// ToEconItem converts the item to an EconItem.
func (i Item) ToEconItem() *trading.Item {
	return &trading.Item{
		AppID:          AppID,
		ContextID:      2, // For TF2 it's always 2
		AssetID:        i.ID,
		Amount:         int64(i.Quantity),
		Name:           i.CustomName,
		MarketName:     i.CustomName,
		MarketHashName: i.CustomName,
		Tradable:       i.IsTradable,
		Marketable:     i.IsMarketable,
	}
}

// ToSKUObject converts the item to a SKU object.
func (i Item) ToSKUObject() *sku.Item {
	quality := int(i.Quality)
	quality2 := 0

	if i.IsElevated {
		quality2 = 11
	}

	// For Unusual + Strange cosmetics, quality is Unusual (5) and quality2 is Strange (11)
	if i.Effect != 0 && i.Quality == 11 && i.Paintkit == 0 {
		quality = 5
		quality2 = 11
	}

	return &sku.Item{
		Defindex:    int(i.DefIndex),
		Quality:     quality,
		Quality2:    quality2,
		Tradable:    i.IsTradable,
		Craftable:   i.IsCraftable,
		Killstreak:  int(i.KillstreakTier),
		Australium:  i.Australium,
		Effect:      int(i.Effect),
		Festivized:  i.Festivized,
		Paintkit:    int(i.Paintkit),
		Wear:        int(i.Wear * 100), // Convert to percentage for SKU
		Craftnumber: int(i.CraftNumber),
		Crateseries: int(i.CrateSeries),
		Target:      int(i.Target),
		Paint:       int(i.Paint),
		Spells:      i.Spells,
		Parts: func() []int {
			p := make([]int, len(i.Parts))
			for idx, v := range i.Parts {
				p[idx] = int(v)
			}

			return p
		}(),
	}
}

// GetSKU returns the SKU string for the item using the provided schema.
func (i *Item) GetSKU(s *schema.Schema) string {
	if i.SKU != "" {
		return i.SKU
	}

	return s.GetSKUFromObject(i.ToSKUObject())
}

// Fix applies schema-based fixes and normalizations to the item.
// This is equivalent to TF2Autobot's fixItem logic.
func (i *Item) Fix(s *schema.Schema) {
	sch := i.GetSchema(s)
	if sch == nil {
		return
	}

	// 1. Standardize DefIndexes for specialized items
	// Killstreak Kits
	if (i.DefIndex >= 5726 && i.DefIndex <= 5733) ||
		(i.DefIndex >= 5743 && i.DefIndex <= 5751) ||
		(i.DefIndex >= 5793 && i.DefIndex <= 5801) {
		i.DefIndex = 6527
	}

	// Strangifiers
	strangifiers := []uint32{
		5661, 5721, 5722, 5723, 5724, 5725, 5753, 5754, 5755, 5756, 5757, 5758, 5759, 5783, 5784, 5804,
	}
	if slices.Contains(strangifiers, i.DefIndex) {
		i.DefIndex = 6522
	}

	// Chemistry Sets
	if i.DefIndex >= 20001 && i.DefIndex <= 20009 {
		i.DefIndex = 20000
	}

	// 2. Fallback to schema for Crate Series
	if sch.ItemClass == "supply_crate" && i.CrateSeries == 0 {
		// Try to find series in schema attributes
		for _, attr := range sch.Attributes {
			if attr.Name == "set supply crate series" {
				i.CrateSeries = uint32(attr.Value)
				break
			}
		}
	}

	// 3. Normalization for SKU (Unusual Strange Cosmetics)
	// This logic is mostly for SKU generation, so we keep it in ToSKUObject.
}

// SO Type IDs specific to Team Fortress 2.
const (
	SOTypeEconItem              int32 = 1
	SOTypeEconGameAccountClient int32 = 7
	SOTypeTFRatingData          int32 = 2007
)

// Option defines a functional configuration for the SOCache.
type Option = bus.Option[*SOCache]

// WithLogger sets a custom logger for the module.
func WithLogger(l log.Logger) Option {
	return func(s *SOCache) {
		s.logger = l.With(log.Component("so_cache"))
	}
}

// WithBus sets a custom event bus for emitting events.
func WithBus(b *bus.Bus) Option {
	return func(s *SOCache) {
		s.bus = b
	}
}

// WithSchema allows filling out the item SKU's during processing.
func WithSchema(schema *schema.Schema) Option {
	return func(s *SOCache) {
		s.schema = schema
	}
}

// SOCache (Shared Object Cache) is the single source of truth for the TF2 inventory.
// It maintains a real-time mirror of the Game Coordinator's item state and provides
// thread-safe access to items and their properties.
//
// The cache is automatically updated via GCPackets (SOCacheSubscribed, SOUpdate, SORemove).
// It also provides high-performance methods for generating SKUs directly from internal
// item objects, bypassing expensive string parsing.
type SOCache struct {
	mu sync.RWMutex

	bus    *bus.Bus
	schema *schema.Schema
	logger log.Logger

	items     map[uint64]*Item
	slots     uint32
	isPremium bool

	// Account Metadata
	tradeBanExpiration uint32
	compAccess         bool
	phoneVerified      bool
	ratings            map[int32]uint32 // rating_type -> rating_primary (MMR)

	// Synchronization tracking
	version atomic.Uint64
	ownerID atomic.Uint64

	coord CoordinatorProvider
}

// NewSOCache creates a new empty Shared Object Cache.
func NewSOCache(coord CoordinatorProvider, opts ...Option) *SOCache {
	s := &SOCache{
		items:   make(map[uint64]*Item),
		ratings: make(map[int32]uint32),
		coord:   coord,
		logger:  log.Discard,
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.bus == nil {
		s.bus = bus.New()
	}

	return s
}

// GetMaxSlots returns the maximum number of item slots in the backpack.
func (c *SOCache) GetMaxSlots() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return int(c.slots)
}

// IsPremium returns true if the account is a premium TF2 account.
func (c *SOCache) IsPremium() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isPremium
}

// GetMMR returns the rating for a specific matchmaking group.
// Common types: 1 = Casual, 2 = 6v6 Competitive.
func (c *SOCache) GetMMR(ratingType int32) uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ratings[ratingType]
}

// GetTradeBanExpiration returns the unix timestamp when the account's trade ban expires.
// Returns 0 if not banned.
func (c *SOCache) GetTradeBanExpiration() uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tradeBanExpiration
}

// HasCompetitiveAccess returns true if the account can join competitive matches.
func (c *SOCache) HasCompetitiveAccess() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.compAccess
}

// GetItems returns a snapshot of the current inventory.
func (c *SOCache) GetItems() []*Item {
	c.mu.RLock()
	defer c.mu.RUnlock()

	list := make([]*Item, 0, len(c.items))
	for _, item := range c.items {
		list = append(list, item)
	}

	return list
}

// GetItem returns a specific item by its AssetID, or nil if not found.
func (c *SOCache) GetItem(id uint64) (*Item, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, ok := c.items[id]

	return item, ok
}

// GetMetal returns the list of metal item ids.
func (c *SOCache) GetMetal(defIndex uint32, count int) []uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var ids []uint64

	for _, item := range c.items {
		if item.DefIndex == defIndex && item.IsTradable {
			ids = append(ids, item.ID)
			if len(ids) == count {
				return ids
			}
		}
	}

	return ids
}

// FindCraftableItems searches for items by DefIndex that are safe to use in crafting.
// Returns an array of item IDs. If count > 0, returns the maximum count of items.
func (c *SOCache) FindCraftableItems(defIndex uint32, count int) []uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var ids []uint64

	for id, item := range c.items {
		// CRITICALLY IMPORTANT: We only accept tradable items!
		// Otherwise, the crafted metal will become non-tradable.
		if item.DefIndex == defIndex && item.IsTradable {
			ids = append(ids, id)
			if count > 0 && len(ids) == count {
				return ids
			}
		}
	}

	return ids
}

// FindWeaponsByClass searches for tradable weapons of a specific class.
func (c *SOCache) FindWeaponsByClass(class string) []*Item {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*Item

	for _, item := range c.items {
		sch := item.GetSchema(c.schema)
		if sch != nil && sch.CraftClass == "weapon" && item.IsTradable {
			if slices.Contains(sch.UsedByClasses, class) {
				result = append(result, item)
			}
		}
	}

	return result
}

// GetMetalCount returns the amount of available metal of a given type.
func (c *SOCache) GetMetalCount(defIndex uint32) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0

	for _, item := range c.items {
		if item.DefIndex == defIndex && item.IsTradable {
			count++
		}
	}

	return count
}

// GetAssetIDsBySKU returns a list of AssetIDs for a given item.
// If limit > 0, returns up to limit items.
func (c *SOCache) GetAssetIDsBySKU(targetSKU string, limit int) []uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []uint64

	for _, item := range c.items {
		if item.SKU == targetSKU && item.IsTradable {
			result = append(result, item.ID)
		}
	}

	return result
}

// handleSubscribed processes the initial full synchronization of the cache.
func (c *SOCache) handleSubscribed(pkt *protocol.GCPacket) {
	msg := &pb.CMsgSOCacheSubscribed{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		c.logger.Error("Failed to unmarshal SOCacheSubscribed", log.Err(err))
		return
	}

	c.version.Store(msg.GetVersion())
	c.ownerID.Store(msg.GetOwner())

	c.mu.Lock()
	clear(c.items)

	for _, subType := range msg.GetObjects() {
		typeID := subType.GetTypeId()
		for _, objData := range subType.GetObjectData() {
			c.processObject(typeID, objData, true, nil)
		}
	}

	count := len(c.items)
	c.mu.Unlock()

	c.logger.Info("TF2 SOCache loaded/resynced",
		log.Int("items", count),
		log.Uint64("version", msg.GetVersion()),
	)

	c.bus.Publish(&BackpackLoadedEvent{
		Count: count,
	})
}

// handleSOUpdate routes incremental events (Create, Update, Destroy, Multiple).
func (c *SOCache) handleSOUpdate(pkt *protocol.GCPacket) {
	msgType := pb.ESOMsg(pkt.MsgType &^ protocol.ProtoMask)

	var (
		newVersion uint64
		events     []bus.Event
	)

	c.mu.Lock()
	switch msgType {
	case pb.ESOMsg_k_ESOMsg_Create, pb.ESOMsg_k_ESOMsg_Update:
		msg := &pb.CMsgSOSingleObject{}
		if proto.Unmarshal(pkt.Payload, msg) == nil {
			newVersion = msg.GetVersion()
			c.processObject(msg.GetTypeId(), msg.GetObjectData(), false, &events)
		}

	case pb.ESOMsg_k_ESOMsg_Destroy:
		msg := &pb.CMsgSOSingleObject{}
		if proto.Unmarshal(pkt.Payload, msg) == nil {
			newVersion = msg.GetVersion()
			c.processDestroy(msg.GetTypeId(), msg.GetObjectData(), &events)
		}

	case pb.ESOMsg_k_ESOMsg_UpdateMultiple:
		msg := &pb.CMsgSOMultipleObjects{}
		if err := proto.Unmarshal(pkt.Payload, msg); err == nil {
			newVersion = msg.GetVersion()

			for _, obj := range msg.GetObjects() {
				c.processObject(obj.GetTypeId(), obj.GetObjectData(), false, &events)
			}
		} else {
			c.logger.Error("Failed to unmarshal SOMultipleObjects", log.Err(err))
		}
	}

	if newVersion > 0 {
		c.version.Store(newVersion)
	}

	c.mu.Unlock()

	for _, ev := range events {
		c.bus.Publish(ev)
	}
}

// handleSOCacheCheck processes k_ESOMsg_CacheSubscriptionCheck (27).
// GC asks if we are still in sync.
func (c *SOCache) handleSOCacheCheck(ctx context.Context, pkt *protocol.GCPacket) {
	msg := &pb.CMsgSOCacheSubscriptionCheck{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		c.logger.Error("Failed to unmarshal CacheSubscriptionCheck", log.Err(err))
		return
	}

	gcVersion := msg.GetVersion()
	ourVersion := c.version.Load()
	owner := msg.GetOwner()

	c.logger.Debug("Received SOCache Check",
		log.Uint64("gc_version", gcVersion),
		log.Uint64("our_version", ourVersion),
	)

	if gcVersion != ourVersion {
		c.logger.Warn("SOCache desync detected. Requesting refresh...",
			log.Uint64("expected", gcVersion),
			log.Uint64("actual", ourVersion),
		)
		c.requestRefresh(ctx, owner, c.logger)
	}
}

// handleUpToDate processes k_ESOMsg_CacheSubscribedUpToDate (29).
// Sent by GC if we requested a Refresh but we already had the latest data.
func (c *SOCache) handleUpToDate(pkt *protocol.GCPacket) {
	msg := &pb.CMsgSOCacheSubscribedUpToDate{}
	if err := proto.Unmarshal(pkt.Payload, msg); err == nil {
		c.version.Store(msg.GetVersion())
		c.logger.Debug("SOCache is up-to-date", log.Uint64("version", msg.GetVersion()))
	}
}

// requestRefresh sends k_ESOMsg_CacheSubscriptionRefresh (28) to the GC.
func (c *SOCache) requestRefresh(ctx context.Context, owner uint64, logger log.Logger) {
	req := &pb.CMsgSOCacheSubscriptionRefresh{
		Owner: proto.Uint64(owner),
	}

	err := c.coord.Send(ctx, AppID, uint32(pb.ESOMsg_k_ESOMsg_CacheSubscriptionRefresh), req)
	if err != nil {
		logger.Error("Failed to send CacheSubscriptionRefresh", log.Err(err))
	}
}

// processObject parses the raw bytes and updates the internal maps.
// Caller MUST hold the mutex.
func (c *SOCache) processObject(typeID int32, data []byte, isBulk bool, events *[]bus.Event) {
	switch typeID {
	case SOTypeEconItem: // Type 1: TF2 Item
		econItem := &pb.CSOEconItem{}
		if err := proto.Unmarshal(data, econItem); err != nil {
			c.logger.Error("Failed to unmarshal CSOEconItem", log.Err(err))
			return
		}

		item := c.protoToItem(econItem)
		if c.schema != nil {
			item.SKU = item.GetSKU(c.schema)
		}

		// Check if it's an update or a new item
		_, exists := c.items[item.ID]
		c.items[item.ID] = item

		// Fire events only if we are not in the middle of initial bulk loading
		if !isBulk && events != nil {
			if exists {
				*events = append(*events, &ItemUpdatedEvent{Item: item})
				c.logger.Debug("Item updated in GC", log.Uint64("id", item.ID))
			} else {
				*events = append(*events, &ItemAcquiredEvent{Item: item})
				c.logger.Debug("New item acquired from GC", log.Uint64("id", item.ID))
			}
		}

	case SOTypeEconGameAccountClient: // Type 7: Account Settings
		acc := &pb.CSOEconGameAccountClient{}
		if err := proto.Unmarshal(data, acc); err == nil {
			// TF2 gives you 50 slots by default. Premium gives +250 (300 total).
			baseSlots := uint32(50)

			if acc.GetTrialAccount() {
				c.isPremium = false
			} else {
				c.isPremium = true
				baseSlots = 300
			}

			c.slots = baseSlots + acc.GetAdditionalBackpackSlots()
			c.tradeBanExpiration = acc.GetTradeBanExpiration()
			c.compAccess = acc.GetCompetitiveAccess()
			c.phoneVerified = acc.GetPhoneVerified()

			c.logger.Debug("Account metadata updated",
				log.Bool("premium", c.isPremium),
				log.Uint32("slots", c.slots),
				log.Bool("comp_access", c.compAccess),
			)
		}

	case SOTypeTFRatingData:
		rating := &pb.CSOTFRatingData{}
		if err := proto.Unmarshal(data, rating); err == nil {
			c.ratings[rating.GetRatingType()] = rating.GetRatingPrimary()
			c.logger.Debug("MMR updated",
				log.Int32("type", rating.GetRatingType()),
				log.Uint32("mmr", rating.GetRatingPrimary()),
			)
		}
	}
}

// processDestroy handles the removal of items from the cache.
// Caller MUST hold the mutex.
func (c *SOCache) processDestroy(typeID int32, data []byte, events *[]bus.Event) {
	if typeID != SOTypeEconItem {
		return
	}

	econItem := &pb.CSOEconItem{}
	if err := proto.Unmarshal(data, econItem); err != nil {
		c.logger.Error("Failed to unmarshal CSOEconItem for destroy", log.Err(err))
		return
	}

	itemID := econItem.GetId()
	delete(c.items, itemID)

	if events != nil {
		*events = append(*events, &ItemRemovedEvent{ItemID: itemID})
	}

	c.logger.Debug("Item removed from GC", log.Uint64("id", itemID))
}

// protoToItem converts the raw Protobuf object into our internal struct.
func (c *SOCache) protoToItem(p *pb.CSOEconItem) *Item {
	item := &Item{
		ID:         p.GetId(),
		OriginalID: p.GetOriginalId(),
		DefIndex:   p.GetDefIndex(),
		Level:      p.GetLevel(),
		Quality:    p.GetQuality(),
		Inventory:  p.GetInventory(),
		Quantity:   p.GetQuantity(),
		Origin:     p.GetOrigin(),
		Flags:      EconItemFlag(p.GetFlags()),
		Style:      p.GetStyle(),
		InUse:      p.GetInUse(),
		AccountID:  p.GetAccountId(),

		CustomName: p.GetCustomName(),
		CustomDesc: p.GetCustomDesc(),

		// Flags based on GC bitmask
		IsTradable:   !EconItemFlag(p.GetFlags()).HasFlag(EconItemFlag_CannotTrade),
		IsMarketable: !EconItemFlag(p.GetFlags()).HasFlag(EconItemFlag_NonEconomy),

		// By default, assume craftable unless a specific attribute/flag is set
		IsCraftable: true,
	}

	// Helper to get float value from attribute bytes
	getFloat := func(b []byte) float32 {
		if len(b) < 4 {
			return 0
		}

		return math.Float32frombits(binary.LittleEndian.Uint32(b))
	}

	// Helper to get uint32 value from attribute bytes
	getUint := func(b []byte) uint32 {
		if len(b) < 4 {
			return 0
		}

		return binary.LittleEndian.Uint32(b)
	}

	// Extract attributes
	for _, attr := range p.GetAttribute() {
		def := attr.GetDefIndex()
		val := attr.GetValueBytes()

		switch def {
		case AttrCustomName:
			item.CustomName = string(val)
		case AttrCustomDesc:
			item.CustomDesc = string(val)
		case AttrMedalNumber:
			item.MedalNumber = getUint(val)
		case AttrUnusualEffect:
			item.Effect = uint32(getFloat(val))
		case AttrPaintPrimary, AttrPaintSecondary:
			item.Paint = uint32(getFloat(val))
		case AttrCannotTrade:
			item.IsTradable = false
		case AttrCannotCraft:
			item.IsCraftable = false
		case AttrCrateSeries:
			item.CrateSeries = uint32(getFloat(val))
		case AttrAlwaysTradable:
			item.IsTradable = true
		case AttrTradableAfter:
			if getUint(val) > uint32(time.Now().Unix()) {
				item.IsTradable = false
			}

		case AttrKillEater:
			item.IsElevated = true
		case AttrCraftNumber:
			item.CraftNumber = getUint(val)
		case AttrStrangePart1, AttrStrangePart2, AttrStrangePart3:
			item.Parts = append(item.Parts, uint32(getFloat(val)))
		case AttrCannotCraftVariant:
			item.IsCraftable = false
		case AttrEOTLEarlySupporter:
			item.EarlySupporter = getFloat(val) != 0
		case AttrQuestLoanerIDLow:
			item.QuestID = (item.QuestID & 0xFFFFFFFF00000000) | uint64(getUint(val))
		case AttrQuestLoanerIDHigh:
			item.QuestID = (item.QuestID & 0x00000000FFFFFFFF) | (uint64(getUint(val)) << 32)
		case AttrWear:
			item.Wear = getFloat(val)
		case AttrPaintkit:
			item.Paintkit = getUint(val)
		case AttrSpell1, AttrSpell2, AttrSpell3, AttrSpell4, AttrSpell5, AttrSpell6:
			item.Spells = append(item.Spells, sku.Spell{
				Attribute: int(def),
				Value:     int(getFloat(val)),
			})
		case AttrTarget:
			item.Target = uint32(getFloat(val))
		case AttrKillstreaker:
			item.Killstreaker = uint32(getFloat(val))
		case AttrSheen:
			item.Sheen = uint32(getFloat(val))
		case AttrKillstreakTier:
			item.KillstreakTier = uint32(getFloat(val))
		case AttrSeries:
			item.Series = uint32(getFloat(val))
		case AttrTauntUnusualEffect:
			item.Effect = uint32(getFloat(val))
		case AttrAustralium:
			item.Australium = getFloat(val) != 0
		case AttrFestivized:
			item.Festivized = getFloat(val) != 0
		}
	}

	// Origin-based restrictions
	if slices.Contains(
		[]uint32{
			OriginAchievement,
			OriginSupport,
			OriginHalloween,
			OriginForeign,
			OriginPreview,
			OriginWorkshop,
			OriginLoaner,
		},
		item.Origin,
	) {
		// Special case: Bugged Loaners (Origin 24) that are actually tradable
		if item.Origin == OriginLoaner && item.IsTradable {
			item.IsBuggedLoaner = true
		} else {
			item.IsTradable = false
			item.IsMarketable = false
		}
	}

	// Crafting restrictions
	if slices.Contains(
		[]uint32{
			OriginStorePromo,
			OriginSupport,
			OriginHalloween,
			OriginForeign,
			OriginPreview,
			OriginWorkshop,
			OriginLoaner,
		},
		item.Origin,
	) {
		item.IsCraftable = false
	}

	// Quality-based restrictions
	if slices.Contains([]uint32{QualitySelfMade, QualityValve, QualityCommunity}, item.Quality) {
		item.IsTradable = false
		item.IsCraftable = false
	}

	// Always Tradable attribute (195) overrides most restrictions
	for _, attr := range p.GetAttribute() {
		if attr.GetDefIndex() == AttrAlwaysTradable {
			item.IsTradable = true
			break
		}
	}

	// Special case: Purchased items (Origin 2)
	if item.Origin == OriginPurchase {
		if !item.Flags.HasFlag(EconItemFlag_PurchasedAfterStoreCraftabilityChanges2012) {
			item.IsCraftable = false
		}
	}

	// Preview items are never tradable/craftable
	if item.Flags.HasFlag(EconItemFlag_Preview) {
		item.IsTradable = false
		item.IsCraftable = false
	}

	return item
}
