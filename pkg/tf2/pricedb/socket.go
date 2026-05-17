// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pricedb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lemon4ksan/g-man/pkg/log"
)

// SocketManager handles the real-time price updates via Socket.IO.
type SocketManager struct {
	url    string
	logger log.Logger

	mu   sync.Mutex
	conn *websocket.Conn

	onPrice func(price *Price)
}

// NewSocketManager creates a new Socket.IO client for PriceDB.
func NewSocketManager(rawURL string, logger log.Logger) *SocketManager {
	if rawURL == "" {
		rawURL = "ws://ws.pricedb.io/"
	}

	return &SocketManager{
		url:    rawURL,
		logger: logger.With(log.Module("pricedb_socket")),
	}
}

// OnPrice sets the callback for when a price update is received.
func (s *SocketManager) OnPrice(fn func(price *Price)) {
	s.onPrice = fn
}

// Run starts the socket connection and maintains it.
func (s *SocketManager) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := s.connectAndListen(ctx); err != nil {
				s.logger.Warn("Socket.IO connection failed, retrying...", log.Err(err))
				time.Sleep(5 * time.Second)
			}
		}
	}
}

func (s *SocketManager) connectAndListen(ctx context.Context) error {
	u, err := url.Parse(s.url)
	if err != nil {
		return err
	}

	// Socket.IO v4 path and params
	u.Path = "/socket.io/"
	q := u.Query()
	q.Set("EIO", "4")
	q.Set("transport", "websocket")
	u.RawQuery = q.Encode()

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}

	s.logger.Debug("Connecting to PriceDB Socket.IO...", log.String("url", u.String()))

	dialer := websocket.DefaultDialer

	conn, resp, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		_ = s.conn.Close()
		s.conn = nil
		s.mu.Unlock()
	}()

	// Socket.IO Handshake sequence
	// 1. Wait for Engine.IO "open" packet (0)
	_, p, err := conn.ReadMessage()
	if err != nil {
		return err
	}

	if !strings.HasPrefix(string(p), "0") {
		return fmt.Errorf("unexpected handshake packet: %s", string(p))
	}

	// 2. Send Socket.IO "connect" packet (40)
	if err := conn.WriteMessage(websocket.TextMessage, []byte("40")); err != nil {
		return err
	}

	// 3. Main listen loop
	for {
		_, p, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		packet := string(p)
		if len(packet) == 0 {
			continue
		}

		switch packet[0] {
		case '2': // Engine.IO Ping
			// Respond with Pong (3)
			if err := conn.WriteMessage(websocket.TextMessage, []byte("3")); err != nil {
				return err
			}

		case '4': // Socket.IO Packet
			if len(packet) < 2 {
				continue
			}

			if packet[1] == '2' { // Event
				s.handleEvent(packet[2:])
			}
		}
	}
}

func (s *SocketManager) handleEvent(payload string) {
	// Socket.IO event format: ["event_name", data]
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return
	}

	if len(raw) < 2 {
		return
	}

	var eventName string
	if err := json.Unmarshal(raw[0], &eventName); err != nil {
		return
	}

	if eventName != "price" {
		return
	}

	var price Price
	if err := json.Unmarshal(raw[1], &price); err != nil {
		s.logger.Warn("Failed to unmarshal price update from socket", log.Err(err))
		return
	}

	if s.onPrice != nil {
		s.onPrice(&price)
	}
}
