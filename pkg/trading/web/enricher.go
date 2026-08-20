// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"time"

	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/async/pipeline"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// Enricher handles caching and batch resolution of Steam asset class descriptions.
type Enricher struct {
	cache *generic.Cache[descKey, rawAssetClassDescription]
}

// NewEnricher constructs an Enricher instance with an empty TTL cache.
func NewEnricher() *Enricher {
	return &Enricher{
		cache: generic.NewCache[descKey, rawAssetClassDescription](),
	}
}

// EnrichOffer enriches missing asset descriptions in offer items.
func (e *Enricher) EnrichOffer(
	ctx context.Context,
	web service.Doer,
	appID uint32,
	language string,
	offer *trading.TradeOffer,
) error {
	if offer == nil {
		return nil
	}

	missingKeys := e.getMissingKeysFromOffer(offer)
	if len(missingKeys) == 0 {
		return nil
	}

	resolvedDescs, uncachedKeys := e.resolveCachedKeys(missingKeys)
	if len(uncachedKeys) > 0 {
		fetched, err := e.fetchAssetClassInfos(ctx, web, appID, language, uncachedKeys)
		if err != nil {
			return err
		}

		for k, desc := range fetched {
			e.cache.Set(k, desc, 5*time.Minute)
			resolvedDescs[k] = desc
		}
	}

	updateItems(offer.ItemsToGive, resolvedDescs)
	updateItems(offer.ItemsToReceive, resolvedDescs)

	return nil
}

// EnrichItems enriches missing asset descriptions in a slice of items.
func (e *Enricher) EnrichItems(
	ctx context.Context,
	web service.Doer,
	appID uint32,
	language string,
	items []*trading.Item,
) error {
	missingKeys := e.getMissingKeys(items)
	if len(missingKeys) == 0 {
		return nil
	}

	resolvedDescs, uncachedKeys := e.resolveCachedKeys(missingKeys)
	if len(uncachedKeys) > 0 {
		fetched, err := e.fetchAssetClassInfos(ctx, web, appID, language, uncachedKeys)
		if err != nil {
			return err
		}

		for k, desc := range fetched {
			e.cache.Set(k, desc, 5*time.Minute)
			resolvedDescs[k] = desc
		}
	}

	updateItems(items, resolvedDescs)

	return nil
}

func (e *Enricher) getMissingKeysFromOffer(offer *trading.TradeOffer) []descKey {
	var (
		seenBuf [32]descKey
		seen    = seenBuf[:0]
		missing []descKey
	)

	checkItem := func(it *trading.Item) {
		if it == nil || it.MarketHashName != "" {
			return
		}

		k := packDescKey(it.ClassID, it.InstanceID)

		for _, s := range seen {
			if s == k {
				return
			}
		}

		seen = append(seen, k)
		missing = append(missing, k)
	}

	for _, it := range offer.ItemsToGive {
		checkItem(it)
	}

	for _, it := range offer.ItemsToReceive {
		checkItem(it)
	}

	return missing
}

func (e *Enricher) getMissingKeys(items []*trading.Item) []descKey {
	if len(items) == 0 {
		return nil
	}

	var (
		seenBuf [32]descKey
		seen    = seenBuf[:0]
		missing []descKey
	)

	for _, it := range items {
		if it == nil || it.MarketHashName != "" {
			continue
		}

		k := packDescKey(it.ClassID, it.InstanceID)
		found := false

		for _, s := range seen {
			if s == k {
				found = true

				break
			}
		}

		if !found {
			seen = append(seen, k)
			missing = append(missing, k)
		}
	}

	return missing
}

func (e *Enricher) resolveCachedKeys(keys []descKey) (map[descKey]rawAssetClassDescription, []descKey) {
	resolved := make(map[descKey]rawAssetClassDescription)

	var uncached []descKey

	for _, k := range keys {
		if desc, ok := e.cache.Get(k); ok {
			resolved[k] = desc
		} else if desc, ok := e.cache.Get(packDescKey(k>>32, 0)); ok {
			resolved[k] = desc
		} else {
			uncached = append(uncached, k)
		}
	}

	return resolved, uncached
}

func (e *Enricher) fetchAssetClassInfos(
	ctx context.Context,
	web service.Doer,
	appID uint32,
	language string,
	uncachedKeys []descKey,
) (map[descKey]rawAssetClassDescription, error) {
	type chunkResult struct {
		descs map[descKey]rawAssetClassDescription
	}

	chunkSize := 50

	var chunks [][]descKey
	for i := 0; i < len(uncachedKeys); i += chunkSize {
		end := min(i+chunkSize, len(uncachedKeys))
		chunks = append(chunks, uncachedKeys[i:end])
	}

	cfg := pipeline.PipelineConfig{Workers: 3, RPS: 5, Burst: 2}

	results, err := pipeline.Map(ctx, cfg, chunks, func(chunkCtx context.Context, chunk []descKey) (chunkResult, error) {
		params := make(url.Values)
		params.Set("appid", strconv.FormatUint(uint64(appID), 10))
		params.Set("language", language)
		params.Set("class_count", strconv.Itoa(len(chunk)))

		for idx, k := range chunk {
			params.Set(fmt.Sprintf("classid%d", idx), strconv.FormatUint(k>>32, 10))

			if k != 0 {
				params.Set(fmt.Sprintf("instanceid%d", idx), strconv.FormatUint(k&0xFFFFFFFF, 10))
			}
		}

		apiResp, err := service.WebAPI[getAssetClassInfoResponse](
			chunkCtx, web, "GET", "ISteamEconomy", "GetAssetClassInfo", 1, params,
		)
		if err != nil {
			return chunkResult{}, err
		}

		resolvedDescs := make(map[descKey]rawAssetClassDescription)
		if apiResp != nil && apiResp.Result != nil {
			for key, rawVal := range apiResp.Result {
				if key == "success" {
					continue
				}

				var desc rawAssetClassDescription
				if err := json.Unmarshal(rawVal, &desc); err == nil {
					resolvedDescs[newDescKey(desc.ClassID, desc.InstanceID)] = desc
				}
			}
		}

		return chunkResult{descs: resolvedDescs}, nil
	})
	if err != nil {
		return nil, err
	}

	merged := make(map[descKey]rawAssetClassDescription)
	for _, r := range results {
		maps.Copy(merged, r.descs)
	}

	return merged, nil
}

func mapDescriptionsToOffer(offer *trading.TradeOffer, rawDescs []rawDescription) {
	if offer == nil || len(rawDescs) == 0 {
		return
	}

	if len(rawDescs) <= 8 {
		mapItemsLinear := func(items []*trading.Item) {
			for _, it := range items {
				if it == nil {
					continue
				}

				key := packDescKey(it.ClassID, it.InstanceID)

				for i := range rawDescs {
					d := &rawDescs[i]
					if newDescKey(d.ClassID, d.InstanceID) == key {
						it.Name = d.Name
						it.NameColor = d.NameColor
						it.Type = d.Type
						it.MarketName = d.MarketName
						it.MarketHashName = d.MarketHashName
						it.IconURL = d.IconURL
						it.Tradable = bool(d.Tradable)
						it.Marketable = bool(d.Marketable)
						it.Descriptions = d.Descriptions
						it.Tags = d.Tags
						it.Actions = d.Actions

						break
					}
				}
			}
		}

		mapItemsLinear(offer.ItemsToGive)
		mapItemsLinear(offer.ItemsToReceive)

		return
	}

	descMap := make(map[descKey]*rawDescription, len(rawDescs))
	for i := range rawDescs {
		d := &rawDescs[i]
		descMap[newDescKey(d.ClassID, d.InstanceID)] = d
	}

	mapItems := func(items []*trading.Item) {
		for _, it := range items {
			if it == nil {
				continue
			}

			key := packDescKey(it.ClassID, it.InstanceID)
			if d, ok := descMap[key]; ok {
				it.Name = d.Name
				it.NameColor = d.NameColor
				it.Type = d.Type
				it.MarketName = d.MarketName
				it.MarketHashName = d.MarketHashName
				it.IconURL = d.IconURL
				it.Tradable = bool(d.Tradable)
				it.Marketable = bool(d.Marketable)
				it.Descriptions = d.Descriptions
				it.Tags = d.Tags
				it.Actions = d.Actions
			}
		}
	}

	mapItems(offer.ItemsToGive)
	mapItems(offer.ItemsToReceive)
}

func updateItems(items []*trading.Item, descs map[descKey]rawAssetClassDescription) {
	for _, it := range items {
		if it == nil || it.MarketHashName != "" {
			continue
		}

		key := packDescKey(it.ClassID, it.InstanceID)

		desc, found := descs[key]
		if !found {
			desc, found = descs[packDescKey(it.ClassID, 0)]
		}

		if !found {
			continue
		}

		it.Name = desc.Name
		it.MarketName = desc.MarketName
		it.MarketHashName = desc.MarketHashName
		it.Type = desc.Type
		it.IconURL = desc.IconURL
		it.Descriptions = desc.Descriptions
		it.Tradable = bool(desc.Tradable)
		it.Marketable = bool(desc.Marketable)

		it.Tags = make([]trading.Tag, len(desc.Tags))
		for idx, t := range desc.Tags {
			locName := t.LocalizedTagName
			if locName == "" {
				locName = t.Name
			}

			it.Tags[idx] = trading.Tag{
				Category:      t.Category,
				InternalName:  t.InternalName,
				Localized:     t.LocalizedCategoryName,
				LocalizedName: locName,
			}
		}
	}
}
