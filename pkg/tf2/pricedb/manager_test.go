// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pricedb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
)

func TestPriceManager_WatchAndGet(t *testing.T) {
	logger := log.New(log.DefaultConfig(log.LevelError))
	client := NewClient(nil)

	manager := NewManager(client, logger)
	assert.Equal(t, BehaviorName, manager.Name())

	// Test initial state
	assert.Len(t, manager.GetWatchedSKUs(), 0)

	// Watch SKU
	manager.Watch("5021;6")
	assert.ElementsMatch(t, []string{"5021;6"}, manager.GetWatchedSKUs())

	// Watch duplicate SKU
	manager.Watch("5021;6")
	assert.ElementsMatch(t, []string{"5021;6"}, manager.GetWatchedSKUs())

	// Watch another SKU
	manager.Watch("5002;6")
	assert.ElementsMatch(t, []string{"5021;6", "5002;6"}, manager.GetWatchedSKUs())

	// Unwatch SKU
	manager.Unwatch("5021;6")
	assert.ElementsMatch(t, []string{"5002;6"}, manager.GetWatchedSKUs())

	// Test GetPrice for uncached SKU
	_, ok := manager.GetPrice("5002;6")
	assert.False(t, ok)
}

func TestPriceManager_UpdatesAndFetch(t *testing.T) {
	// Create mock HTTP server for PriceDB bulk API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/items-bulk", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var req bulkRequest

		_ = json.NewDecoder(r.Body).Decode(&req)

		allPrices := []*Price{
			{
				SKU:  "5021;6",
				Name: "Key",
				Buy:  Currencies{Metal: 75},
				Sell: Currencies{Metal: 75.11},
				Time: 1600000000,
			},
			{
				SKU:  "5002;6",
				Name: "Refined",
				Buy:  Currencies{Metal: 1},
				Sell: Currencies{Metal: 1},
				Time: 1600000000,
			},
		}

		var resp []*Price
		if len(req.SKUs) == 0 {
			resp = allPrices
		} else {
			skuMap := make(map[string]bool)
			for _, sku := range req.SKUs {
				skuMap[sku] = true
			}

			for _, p := range allPrices {
				if skuMap[p.SKU] {
					resp = append(resp, p)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger := log.New(log.DefaultConfig(log.LevelError))
	client := NewClient(nil)
	client.restClient = client.restClient.WithBaseURL(server.URL)

	manager := NewManager(client, logger)

	// Test seed empty
	err := manager.SeedFromBackpack(context.Background(), nil)
	require.NoError(t, err)

	// Test Seed from backpack
	err = manager.SeedFromBackpack(context.Background(), []string{"5021;6", "5002;6"})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"5021;6", "5002;6"}, manager.GetWatchedSKUs())

	// Verify cached values
	p1, ok := manager.GetPrice("5021;6")
	assert.True(t, ok)
	assert.Equal(t, "5021;6", p1.SKU)
	assert.Equal(t, 75.0, p1.Buy.Metal)

	p2, ok := manager.GetPrice("5002;6")
	assert.True(t, ok)
	assert.Equal(t, "5002;6", p2.SKU)
	assert.Equal(t, 1.0, p2.Buy.Metal)

	// Test Update
	err = manager.Update(context.Background())
	require.NoError(t, err)

	// Test Fetch
	fetched, err := manager.Fetch(context.Background(), []string{"5021;6"})
	require.NoError(t, err)
	assert.Len(t, fetched, 1)
	assert.Equal(t, 75.0, fetched["5021;6"].Buy.Metal)

	// Test Fetch empty
	fetchedEmpty, err := manager.Fetch(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, fetchedEmpty, 0)
}

func TestPriceManager_SocketUpdates(t *testing.T) {
	logger := log.New(log.DefaultConfig(log.LevelError))
	client := NewClient(nil)

	manager := NewManager(client, logger)

	// Test manual trigger of socket OnPrice event callback
	priceUpdate := &Price{
		SKU:  "5021;6",
		Name: "Mann Co. Supply Crate Key",
		Buy:  Currencies{Metal: 80},
		Sell: Currencies{Metal: 80.11},
		Time: 1700000000,
	}

	// Trigger callback directly
	manager.socket.onPrice(priceUpdate)

	p, ok := manager.GetPrice("5021;6")
	assert.True(t, ok)
	assert.Equal(t, 80.0, p.Buy.Metal)

	// Test invalid price does not update cache
	invalidPrice := &Price{
		SKU:  "5021;6",
		Buy:  Currencies{Metal: -5}, // invalid!
		Sell: Currencies{Metal: 80.11},
	}
	manager.socket.onPrice(invalidPrice)

	p, ok = manager.GetPrice("5021;6")
	assert.True(t, ok)
	assert.Equal(t, 80.0, p.Buy.Metal) // remains 80.0
}

func TestPriceManager_OrchestratorOption(t *testing.T) {
	logger := log.New(log.DefaultConfig(log.LevelError))
	client := NewClient(nil)

	orchestrator := behavior.NewOrchestrator(logger, bus.New())
	opt := WithPriceManager(client)

	assert.NotPanics(t, func() {
		orchestrator.Install(opt)
	})
}

func TestPriceManager_RealtimeWebsocketHandshake(t *testing.T) {
	upgrader := websocket.Upgrader{}

	var mockPriceSent sync.WaitGroup
	mockPriceSent.Add(1)

	// Spin up a mock WebSockets server simulating Socket.IO v4 Handshake
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// 1. Send Engine.IO Open Packet "0"
		err = conn.WriteMessage(
			websocket.TextMessage,
			[]byte(`0{"sid":"123","upgrades":[],"pingInterval":25000,"pingTimeout":5000}`),
		)
		if err != nil {
			return
		}

		// 2. Read Engine.IO Connection Packet "40"
		_, p, err := conn.ReadMessage()
		if err != nil || string(p) != "40" {
			return
		}

		// 3. Send Socket.IO price event packet "42"
		priceUpdatePayload := `42["price",{"sku":"5021;6","name":"Key","buy":{"metal":82},"sell":{"metal":82.11},"time":1800000000}]`

		err = conn.WriteMessage(websocket.TextMessage, []byte(priceUpdatePayload))
		if err != nil {
			return
		}

		mockPriceSent.Done()
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)

	logger := log.New(log.DefaultConfig(log.LevelError))
	client := NewClient(nil)

	manager := &Manager{
		client:       client,
		logger:       logger.With(log.Module(BehaviorName)),
		cache:        make(map[string]*Price),
		watchedSKUs:  make(map[string]struct{}),
		syncInterval: 10 * time.Millisecond,
	}

	var priceUpdated sync.WaitGroup
	priceUpdated.Add(1)

	manager.socket = NewSocketManager(wsURL, manager.logger)
	manager.socket.OnPrice(func(p *Price) {
		if p.SKU == "5021;6" && p.Buy.Metal == 82.0 {
			manager.mu.Lock()
			manager.cache[p.SKU] = p
			manager.mu.Unlock()
			priceUpdated.Done()
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start socket manager listener
	go func() {
		_ = manager.socket.Run(ctx)
	}()

	// Wait for price update to flow from WebSocket mock to manager cache
	select {
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for websocket price update event")
	case <-func() chan struct{} {
		ch := make(chan struct{})
		go func() {
			mockPriceSent.Wait()
			priceUpdated.Wait()
			close(ch)
		}()

		return ch
	}():
		// Success!
	}

	p, ok := manager.GetPrice("5021;6")
	assert.True(t, ok)
	assert.Equal(t, 82.0, p.Buy.Metal)
}
