// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/g-man/pkg/rest"
	inv "github.com/lemon4ksan/g-man/pkg/tf2/inventory"
)

// BackpackTFChecker implements duplicate checking via the backpack.tf website.
// Even though this is scraping, we use the transport and settings from Client.
type BackpackTFChecker struct {
	bptfClient *Client
}

// NewBackpackTFChecker creates a new checker instance.
// It takes a Client, which already contains API tokens and logger settings.
func NewBackpackTFChecker(client *Client) *BackpackTFChecker {
	return &BackpackTFChecker{
		bptfClient: client,
	}
}

// CheckHistory checks the item's history on the backpack.tf website.
func (c *BackpackTFChecker) CheckHistory(ctx context.Context, assetID uint64) (inv.HistoryStatus, error) {
	path := "https://backpack.tf/item/" + strconv.FormatUint(assetID, 10)

	resp, err := c.bptfClient.REST().Request(ctx, http.MethodGet, path, nil, nil)

	if err != nil {
		if apiErr, ok := err.(*rest.APIError); ok && apiErr.StatusCode == http.StatusNotFound {
			return inv.HistoryStatus{Recorded: false}, nil
		}
		return inv.HistoryStatus{}, fmt.Errorf("bptf dupe check request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return inv.HistoryStatus{}, fmt.Errorf("bptf returned unexpected status: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return inv.HistoryStatus{}, fmt.Errorf("failed to parse bptf HTML: %w", err)
	}

	if doc.Find("table").Length() != 1 {
		return inv.HistoryStatus{Recorded: false}, nil
	}

	isDuped := doc.Find("#dupe-modal-btn").Length() > 0

	return inv.HistoryStatus{
		Recorded: true,
		IsDuped:  isDuped,
	}, nil
}
