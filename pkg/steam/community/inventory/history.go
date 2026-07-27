// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	json "github.com/goccy/go-json"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

var (
	// ErrMalformedPagingRow indicates trade history page HTML lacked expected paging elements.
	ErrMalformedPagingRow = errors.New("history: malformed page (paging row not found)")
	// ErrMalformedHistoryInventory indicates trade history page HTML lacked expected inventory JSON blocks.
	ErrMalformedHistoryInventory = errors.New("history: malformed page (g_rgHistoryInventory not found)")
)

// HistoryParser extracts trade history records, assets, and pagination keys from raw HTML pages.
type HistoryParser struct {
	rawHTML []byte
	doc     *goquery.Document
}

// NewHistoryParser initializes a HistoryParser.
func NewHistoryParser(rawHTML []byte) (*HistoryParser, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(rawHTML))
	if err != nil {
		return nil, fmt.Errorf("history: failed to parse HTML document: %w", err)
	}

	return &HistoryParser{
		rawHTML: rawHTML,
		doc:     doc,
	}, nil
}

// Parse extracts trade history rows and pagination tokens.
func (p *HistoryParser) Parse() (*TradeHistoryResult, error) {
	if p.doc.Find(".inventory_history_pagingrow").Length() == 0 {
		return nil, ErrMalformedPagingRow
	}

	inventory, err := p.extractHistoryInventory()
	if err != nil {
		return nil, err
	}

	result := &TradeHistoryResult{
		Trades: p.parseRows(inventory, p.extractHovers()),
	}

	p.parsePagination(result)

	return result, nil
}

func (p *HistoryParser) extractHistoryInventory() (map[string]map[string]map[string]EconItem, error) {
	match := rxHistoryInventory.FindSubmatch(p.rawHTML)
	if len(match) != 2 {
		return nil, ErrMalformedHistoryInventory
	}

	var inventory map[string]map[string]map[string]EconItem
	if err := json.Unmarshal(match[1], &inventory); err != nil {
		return nil, fmt.Errorf("history: failed to parse history inventory JSON: %w", err)
	}

	return inventory, nil
}

func (p *HistoryParser) extractHovers() map[string]hoverInfo {
	hoverMap := make(map[string]hoverInfo)
	hovers := rxHoverScript.FindAllSubmatch(p.rawHTML, -1)

	for _, hover := range hovers {
		if len(hover) != 6 {
			continue
		}

		elementID := string(hover[1])
		amount, _ := strconv.Atoi(string(hover[5]))
		hoverMap[elementID] = hoverInfo{
			AppID:     string(hover[2]),
			ContextID: string(hover[3]),
			AssetID:   string(hover[4]),
			Amount:    amount,
		}
	}

	return hoverMap
}

func (p *HistoryParser) parsePagination(result *TradeHistoryResult) {
	p.doc.Find(".inventory_history_nextbtn .pagebtn:not(.disabled)").Each(func(_ int, buttonSel *goquery.Selection) {
		href, exists := buttonSel.Attr("href")
		if !exists {
			return
		}

		p.extractPaginationParams(href, result)
	})
}

func (p *HistoryParser) extractPaginationParams(href string, result *TradeHistoryResult) {
	timeMatch := rxPaginationTime.FindStringSubmatch(href)
	tradeMatch := rxPaginationTrade.FindStringSubmatch(href)

	if len(timeMatch) != 2 || len(tradeMatch) != 2 {
		return
	}

	unixTime, err := strconv.ParseInt(timeMatch[1], 10, 64)
	if err != nil {
		return
	}

	timestamp := time.Unix(unixTime, 0).UTC()

	tradeID, err := strconv.ParseUint(tradeMatch[1], 10, 64)
	if err != nil {
		return
	}

	if strings.Contains(href, "prev=1") {
		result.FirstTradeTime = &timestamp
		result.FirstTradeID = &tradeID
	} else {
		result.LastTradeTime = &timestamp
		result.LastTradeID = &tradeID
	}
}

func (p *HistoryParser) parseRows(
	historyInventory map[string]map[string]map[string]EconItem,
	hoverMap map[string]hoverInfo,
) []TradeHistoryRow {
	var trades []TradeHistoryRow

	p.doc.Find(".tradehistoryrow").Each(func(_ int, rowSel *goquery.Selection) {
		row := TradeHistoryRow{
			ItemsReceived: make([]EconItem, 0),
			ItemsGiven:    make([]EconItem, 0),
		}

		row.OnHold = p.parseRowHoldStatus(rowSel)
		row.Date = p.parseRowTimestamp(rowSel)

		partnerAnchor := rowSel.Find(".tradehistory_event_description a")
		row.PartnerName = partnerAnchor.Text()

		if profileLink, exists := partnerAnchor.Attr("href"); exists {
			p.parsePartnerProfile(profileLink, &row)
		}

		rowSel.Find(".history_item").Each(func(_ int, itemSel *goquery.Selection) {
			p.parseHistoryItem(itemSel, historyInventory, hoverMap, &row)
		})

		trades = append(trades, row)
	})

	return trades
}

func (p *HistoryParser) parseRowHoldStatus(rowSel *goquery.Selection) bool {
	holdText := rowSel.Find("span:nth-of-type(2)").Text()

	return strings.Contains(strings.ToLower(holdText), "trade on hold")
}

func (p *HistoryParser) parseRowTimestamp(rowSel *goquery.Selection) time.Time {
	timeText := rowSel.Find(".tradehistory_timestamp").Text()

	time24, err := convertTimeTo24h(timeText)
	if err != nil {
		return time.Time{}
	}

	dateText := rowSel.Find(".tradehistory_date").Text()

	parsedTime, err := parseTradeDate(dateText, time24)
	if err != nil {
		return time.Time{}
	}

	return parsedTime
}

func (p *HistoryParser) parseHistoryItem(
	itemSel *goquery.Selection,
	inventory map[string]map[string]map[string]EconItem,
	hoverMap map[string]hoverInfo,
	row *TradeHistoryRow,
) {
	elementID, exists := itemSel.Attr("id")
	if !exists {
		return
	}

	hover, exists := hoverMap[elementID]
	if !exists {
		return
	}

	itemDetail, exists := lookupInventoryItem(inventory, hover)
	if !exists {
		return
	}

	itemDetail.Amount = hover.Amount

	if strings.Contains(elementID, "received") {
		row.ItemsReceived = append(row.ItemsReceived, itemDetail)
	} else {
		row.ItemsGiven = append(row.ItemsGiven, itemDetail)
	}
}

func (p *HistoryParser) parsePartnerProfile(profileLink string, row *TradeHistoryRow) {
	parts := strings.Split(strings.TrimRight(profileLink, "/"), "/")
	if len(parts) == 0 {
		return
	}

	lastPart := parts[len(parts)-1]

	if strings.Contains(profileLink, "/profiles/") {
		sidVal, _ := strconv.ParseUint(lastPart, 10, 64)
		row.PartnerSteamID = id.ID(sidVal)
	} else {
		row.PartnerVanityURL = lastPart
	}
}
