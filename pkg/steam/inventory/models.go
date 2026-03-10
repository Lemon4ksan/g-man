package inventory

import (
	"fmt"
)

// Asset represents a specific object in the inventory.
type Asset struct {
	AssetID    string `json:"assetid"`
	ClassID    string `json:"classid"`
	InstanceID string `json:"instanceid"`
	Amount     string `json:"amount"`
	CurrencyID string `json:"currencyid,omitempty"`
}

// Description contains the item's metadata (name, properties, whether it can be transferred).
type Description struct {
	ClassID    string `json:"classid"`
	InstanceID string `json:"instanceid"`
	Tradable   int    `json:"tradable"`
	Name       string `json:"name"`
}

// EconItem is a combined item (Asset + Description).
type EconItem struct {
	Asset       Asset
	Description Description
	Pos         int
	ContextID   uint64
}

// Response describes the raw response from the Inventory API (Steam or Proxy).
type Response struct {
	Success             int           `json:"success"`
	Error               string        `json:"error,omitempty"`
	ErrorCapital        string        `json:"Error,omitempty"`
	Assets              []Asset       `json:"assets"`
	Descriptions        []Description `json:"descriptions"`
	MoreItems           int           `json:"more_items"`
	LastAssetID         string        `json:"last_assetid"`
	TotalInventoryCount int           `json:"total_inventory_count"`
	FakeRedirect        *int          `json:"fake_redirect,omitempty"`
}

// NormalizeInstanceID forces an empty instanceid to "0", as Steam expects.
func NormalizeInstanceID(id string) string {
	if id == "" {
		return "0"
	}
	return id
}

// GetDescriptionKey creates a unique key to link an Asset and a Description.
func GetDescriptionKey(classID, instanceID string) string {
	return fmt.Sprintf("%s_%s", classID, NormalizeInstanceID(instanceID))
}
