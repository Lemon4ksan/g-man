// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/sein"
)

// HAREntry describes an individual HTTP request-response transaction in a HAR archive.
type HAREntry struct {
	Request struct {
		Method string `json:"method"`
		URL    string `json:"url"`
	} `json:"request"`
	Response struct {
		Status  int `json:"status"`
		Content struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"content"`
	} `json:"response"`
}

// HARLog models the top-level container of a W3C HAR 1.2 archive.
type HARLog struct {
	Log struct {
		Entries []HAREntry `json:"entries"`
	} `json:"log"`
}

// SteamEmulator is an in-memory high-fidelity Steam Web / Community / OpenID / Trade server simulator powered by sein.
type SteamEmulator struct {
	server       *sein.Server
	httpServer   *httptest.Server
	mu           sync.RWMutex
	offerCounter atomic.Uint64

	// Stateful state
	inventories  map[string][]byte
	tradeOffers  map[string]map[string]any
	harResponses map[string]HAREntry
	errorMocks   map[string]int
}

// NewSteamEmulator creates and initializes an in-memory Steam emulator.
func NewSteamEmulator() *SteamEmulator {
	emu := &SteamEmulator{
		server:       sein.New(),
		inventories:  make(map[string][]byte),
		tradeOffers:  make(map[string]map[string]any),
		harResponses: make(map[string]HAREntry),
		errorMocks:   make(map[string]int),
	}
	emu.offerCounter.Store(100000)

	emu.registerRoutes()
	emu.httpServer = httptest.NewServer(emu.server)
	return emu
}

// Close shuts down the emulator.
func (e *SteamEmulator) Close() {
	if e.httpServer != nil {
		e.httpServer.Close()
	}
}

// BaseURL returns the mock Steam server base URL.
func (e *SteamEmulator) BaseURL() string {
	return e.httpServer.URL
}

// Client returns an aoni Client pre-configured to communicate with the in-memory emulator.
func (e *SteamEmulator) Client() *aoni.Client {
	return aoni.NewClient(e.httpServer.Client(), option.WithBaseURL(e.BaseURL()))
}

// LoadHAR parses a W3C HAR 1.2 archive and populates the emulator's endpoint playback cache.
func (e *SteamEmulator) LoadHAR(r io.Reader) error {
	var har HARLog
	if err := json.NewDecoder(r).Decode(&har); err != nil {
		return fmt.Errorf("failed to decode HAR: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, entry := range har.Log.Entries {
		key := entry.Request.Method + ":" + sanitizeURLPath(entry.Request.URL)
		e.harResponses[key] = entry
	}
	return nil
}

// SetInventory sets the raw JSON response for an inventory endpoint.
func (e *SteamEmulator) SetInventory(steamID uint64, appID, contextID int, data []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := fmt.Sprintf("%d/%d/%d", steamID, appID, contextID)
	e.inventories[key] = data
}

// SetTradeOffer sets or overrides the state of a simulated trade offer.
func (e *SteamEmulator) SetTradeOffer(offerID string, state map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tradeOffers[offerID] = state
}

// SimulateError forces a route to return an error status code (e.g. 429, 500, 503).
func (e *SteamEmulator) SimulateError(path string, statusCode int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.errorMocks[path] = statusCode
}

// ClearErrors removes all simulated errors.
func (e *SteamEmulator) ClearErrors() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.errorMocks = make(map[string]int)
}

func sanitizeURLPath(rawURL string) string {
	if idx := strings.Index(rawURL, "://"); idx != -1 {
		rawURL = rawURL[idx+3:]
	}
	if slashIdx := strings.Index(rawURL, "/"); slashIdx != -1 {
		return rawURL[slashIdx:]
	}
	return "/"
}

func (e *SteamEmulator) checkError(path string) (int, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for p, code := range e.errorMocks {
		if strings.Contains(path, p) {
			return code, true
		}
	}
	return 0, false
}

func (e *SteamEmulator) registerRoutes() {
	// Middleware: Error simulation & HAR fallback interceptor
	e.server.Use(func(next sein.RawHandler) sein.RawHandler {
		return func(req *sein.Request) (any, error) {
			if code, ok := e.checkError(req.Path()); ok {
				return sein.StatusWith(code, fmt.Sprintf(`{"error":"simulated error %d"}`, code), http.Header{
					header.ContentType: []string{header.MIMEApplicationJSONCharsetUTF8},
				}), nil
			}
			return next(req)
		}
	})

	// 1. OpenID Mock Login
	sein.Handle(e.server, http.MethodGet, "/openid/login", func(req *sein.Request) (any, error) {
		claimedID := "https://steamcommunity.com/openid/id/76561198000000001"
		redirectURL := req.Query("openid.return_to").String()
		if redirectURL == "" {
			return "Missing openid.return_to", nil
		}

		separator := "?"
		if strings.Contains(redirectURL, "?") {
			separator = "&"
		}
		target := fmt.Sprintf("%s%sopenid.mode=id_res&openid.claimed_id=%s&openid.identity=%s",
			redirectURL, separator, claimedID, claimedID)

		return sein.Redirect(target, http.StatusSeeOther), nil
	})

	// 2. Inventory Endpoint
	sein.Handle(e.server, http.MethodGet, "/inventory/:steamid/:appid/:contextid", func(req *sein.Request) (any, error) {
		steamID := req.Param("steamid").String()
		appID := req.Param("appid").String()
		contextID := req.Param("contextid").String()
		key := fmt.Sprintf("%s/%s/%s", steamID, appID, contextID)

		e.mu.RLock()
		data, exists := e.inventories[key]
		e.mu.RUnlock()

		if exists {
			return sein.OK[any](data).
				WithHeader(header.ContentType, header.MIMEApplicationJSONCharsetUTF8), nil
		}

		emptyInv := `{"assets":[],"descriptions":[],"total_inventory_count":0,"success":1,"rwgrsn":-2}`
		return sein.OK[any](emptyInv).
			WithHeader(header.ContentType, header.MIMEApplicationJSONCharsetUTF8), nil
	})

	// 3. Trade Offer Send
	sein.Handle(e.server, http.MethodPost, "/tradeoffer/new/send", func(_ *sein.Request) (any, error) {
		newID := e.offerCounter.Add(1)
		offerIDStr := fmt.Sprintf("%d", newID)

		e.mu.Lock()
		e.tradeOffers[offerIDStr] = map[string]any{
			"tradeofferid":              offerIDStr,
			"trade_offer_state":         2, // Active
			"is_our_offer":              true,
			"time_created":              1700000000,
			"time_updated":              1700000000,
			"from_real_time_trade":      false,
			"escrow_end_date":           0,
			"confirmation_method":       0,
			"needs_mobile_confirmation": false,
			"needs_email_confirmation":  false,
		}
		e.mu.Unlock()

		resp := fmt.Sprintf(`{"tradeofferid":"%s","needs_mobile_confirmation":false,"needs_email_confirmation":false}`, offerIDStr)
		return sein.OK[any](resp).
			WithHeader(header.ContentType, header.MIMEApplicationJSONCharsetUTF8), nil
	})

	// 4. IEconService: GetTradeOffers
	getOffersHandler := func(_ *sein.Request) (any, error) {
		e.mu.RLock()
		offersList := make([]map[string]any, 0, len(e.tradeOffers))
		for _, off := range e.tradeOffers {
			offersList = append(offersList, off)
		}
		e.mu.RUnlock()

		result := map[string]any{
			"response": map[string]any{
				"trade_offers_sent":     offersList,
				"trade_offers_received": []any{},
			},
		}
		data, _ := json.Marshal(result)
		return sein.OK[any](data).
			WithHeader(header.ContentType, header.MIMEApplicationJSONCharsetUTF8), nil
	}
	sein.Handle(e.server, http.MethodGet, "/IEconService/GetTradeOffers/v1", getOffersHandler)
	sein.Handle(e.server, http.MethodGet, "/IEconService/GetTradeOffers/v1/", getOffersHandler)

	// 5. IEconService: GetTradeOffer
	getOfferHandler := func(req *sein.Request) (any, error) {
		offerID := req.Query("tradeofferid").String()
		e.mu.RLock()
		offer, exists := e.tradeOffers[offerID]
		e.mu.RUnlock()

		if !exists {
			return sein.NotFound("NOT_FOUND", "trade offer not found"), nil
		}

		result := map[string]any{
			"response": map[string]any{
				"offer": offer,
			},
		}
		data, _ := json.Marshal(result)
		return sein.OK[any](data).
			WithHeader(header.ContentType, header.MIMEApplicationJSONCharsetUTF8), nil
	}
	sein.Handle(e.server, http.MethodGet, "/IEconService/GetTradeOffer/v1", getOfferHandler)
	sein.Handle(e.server, http.MethodGet, "/IEconService/GetTradeOffer/v1/", getOfferHandler)

	// 6. Trade Offer Accept
	sein.Handle(e.server, http.MethodPost, "/tradeoffer/:id/accept", func(req *sein.Request) (any, error) {
		idStr := req.Param("id").String()
		e.mu.Lock()
		if offer, ok := e.tradeOffers[idStr]; ok {
			offer["trade_offer_state"] = 3 // Accepted
			offer["tradeid"] = "900000001"
		}
		e.mu.Unlock()

		return sein.OK[any](`{"tradeid":"900000001","needs_mobile_confirmation":false,"needs_email_confirmation":false}`).
			WithHeader(header.ContentType, header.MIMEApplicationJSONCharsetUTF8), nil
	})

	// 7. Trade Offer Decline & Cancel
	sein.Handle(e.server, http.MethodPost, "/tradeoffer/:id/decline", func(req *sein.Request) (any, error) {
		idStr := req.Param("id").String()
		e.mu.Lock()
		if offer, ok := e.tradeOffers[idStr]; ok {
			offer["trade_offer_state"] = 7 // Declined
		}
		e.mu.Unlock()
		return sein.OK[any](`{"tradeofferid":"`+idStr+`"}`).
			WithHeader(header.ContentType, header.MIMEApplicationJSONCharsetUTF8), nil
	})

	sein.Handle(e.server, http.MethodPost, "/tradeoffer/:id/cancel", func(req *sein.Request) (any, error) {
		idStr := req.Param("id").String()
		e.mu.Lock()
		if offer, ok := e.tradeOffers[idStr]; ok {
			offer["trade_offer_state"] = 6 // Canceled
		}
		e.mu.Unlock()
		return sein.OK[any](`{"tradeofferid":"`+idStr+`"}`).
			WithHeader(header.ContentType, header.MIMEApplicationJSONCharsetUTF8), nil
	})
}
