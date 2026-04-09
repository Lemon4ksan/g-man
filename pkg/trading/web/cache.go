// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"fmt"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

type AssetCache struct {
	mu    sync.RWMutex
	items map[string]*trading.Item // key: appid_classid_instanceid
}

func NewAssetCache() *AssetCache {
	return &AssetCache{items: make(map[string]*trading.Item)}
}

func (c *AssetCache) Get(appID uint32, classID, instanceID uint64) *trading.Item {
	key := fmt.Sprintf("%d_%d_%d", appID, classID, instanceID)
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items[key]
}

func (c *AssetCache) Add(items []*trading.Item) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, it := range items {
		key := fmt.Sprintf("%d_%d_%d", it.AppID, it.ClassID, it.InstanceID)
		c.items[key] = it
	}
}
