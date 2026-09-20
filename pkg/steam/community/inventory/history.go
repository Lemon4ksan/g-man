// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/x/codec/extract"
	"github.com/lemon4ksan/foundation/codec/json"
	"golang.org/x/net/html"

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
	doc     *html.Node
}

// NewHistoryParser initializes a HistoryParser.
func NewHistoryParser(rawHTML []byte) (*HistoryParser, error) {
	doc, err := html.Parse(bytes.NewReader(rawHTML))
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
	if findFirstWithClass(p.doc, "inventory_history_pagingrow") == nil {
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
	rawJSON, err := extract.Between(p.rawHTML, "var g_rgHistoryInventory = ", ";")
	if err != nil {
		return nil, ErrMalformedHistoryInventory
	}

	var inventory map[string]map[string]map[string]EconItem
	if err := json.UnmarshalNoCopy(bytes.TrimSpace(rawJSON), &inventory); err != nil {
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
	nextBtnNode := findFirstWithClass(p.doc, "inventory_history_nextbtn")
	if nextBtnNode == nil {
		return
	}

	var buttons []*html.Node
	findAllWithClass(nextBtnNode, "pagebtn", &buttons)

	for _, btn := range buttons {
		if hasHTMLClass(btn, "disabled") {
			continue
		}

		href := getHTMLAttr(btn, "href")
		if href != "" {
			p.extractPaginationParams(href, result)
		}
	}
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
	var rows []*html.Node
	findAllWithClass(p.doc, "tradehistoryrow", &rows)

	trades := make([]TradeHistoryRow, 0, len(rows))

	for _, rowNode := range rows {
		row := TradeHistoryRow{
			ItemsReceived: make([]EconItem, 0),
			ItemsGiven:    make([]EconItem, 0),

			OnHold: p.parseRowHoldStatus(rowNode),
			Date:   p.parseRowTimestamp(rowNode),
		}

		if descNode := findFirstWithClass(rowNode, "tradehistory_event_description"); descNode != nil {
			if partnerAnchor := findFirstElement(descNode, "a"); partnerAnchor != nil {
				row.PartnerName = getHTMLText(partnerAnchor)
				if profileLink := getHTMLAttr(partnerAnchor, "href"); profileLink != "" {
					p.parsePartnerProfile(profileLink, &row)
				}
			}
		}

		var items []*html.Node
		findAllWithClass(rowNode, "history_item", &items)

		for _, itemNode := range items {
			p.parseHistoryItem(itemNode, historyInventory, hoverMap, &row)
		}

		trades = append(trades, row)
	}

	return trades
}

func (p *HistoryParser) parseRowHoldStatus(rowNode *html.Node) bool {
	var spans []*html.Node
	findAllElements(rowNode, "span", &spans)

	if len(spans) >= 2 {
		holdText := getHTMLText(spans[1])
		return strings.Contains(strings.ToLower(holdText), "trade on hold")
	}

	return false
}

func (p *HistoryParser) parseRowTimestamp(rowNode *html.Node) time.Time {
	tsNode := findFirstWithClass(rowNode, "tradehistory_timestamp")
	if tsNode == nil {
		return time.Time{}
	}

	timeText := getHTMLText(tsNode)

	time24, err := convertTimeTo24h(timeText)
	if err != nil {
		return time.Time{}
	}

	dateNode := findFirstWithClass(rowNode, "tradehistory_date")
	if dateNode == nil {
		return time.Time{}
	}

	dateText := getHTMLText(dateNode)

	parsedTime, err := parseTradeDate(dateText, time24)
	if err != nil {
		return time.Time{}
	}

	return parsedTime
}

func (p *HistoryParser) parseHistoryItem(
	itemNode *html.Node,
	inventory map[string]map[string]map[string]EconItem,
	hoverMap map[string]hoverInfo,
	row *TradeHistoryRow,
) {
	elementID := getHTMLAttr(itemNode, "id")
	if elementID == "" {
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

func findFirstWithClass(n *html.Node, className string) *html.Node {
	if n == nil {
		return nil
	}

	if n.Type == html.ElementNode && hasHTMLClass(n, className) {
		return n
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if res := findFirstWithClass(c, className); res != nil {
			return res
		}
	}

	return nil
}

func findAllWithClass(n *html.Node, className string, out *[]*html.Node) {
	if n == nil {
		return
	}

	if n.Type == html.ElementNode && hasHTMLClass(n, className) {
		*out = append(*out, n)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		findAllWithClass(c, className, out)
	}
}

func findFirstElement(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}

	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if res := findFirstElement(c, tag); res != nil {
			return res
		}
	}

	return nil
}

func findAllElements(n *html.Node, tag string, out *[]*html.Node) {
	if n == nil {
		return
	}

	if n.Type == html.ElementNode && n.Data == tag {
		*out = append(*out, n)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		findAllElements(c, tag, out)
	}
}

func hasHTMLClass(n *html.Node, className string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			if slices.Contains(strings.Fields(a.Val), className) {
				return true
			}
		}
	}

	return false
}

func getHTMLAttr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}

	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}

	return ""
}

func getHTMLText(n *html.Node) string {
	if n == nil {
		return ""
	}

	var (
		sb   strings.Builder
		walk func(*html.Node)
	)

	walk = func(curr *html.Node) {
		if curr.Type == html.TextNode {
			sb.WriteString(curr.Data)
		}

		for c := curr.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)

	return sb.String()
}
