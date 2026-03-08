package trading

import (
	"fmt"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/modules/econ"
)

type AssetCache struct {
	mu    sync.RWMutex
	items map[string]*econ.Item // key: appid_classid_instanceid
}

func NewAssetCache() *AssetCache {
	return &AssetCache{items: make(map[string]*econ.Item)}
}

func (c *AssetCache) Get(appID uint32, classID, instanceID uint64) *econ.Item {
	key := fmt.Sprintf("%d_%d_%d", appID, classID, instanceID)
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items[key]
}

func (c *AssetCache) Add(items []*econ.Item) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, it := range items {
		key := fmt.Sprintf("%d_%d_%d", it.AppID, it.ClassID, it.InstanceID)
		c.items[key] = it
	}
}
