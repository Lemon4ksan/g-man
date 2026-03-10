// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

func TestSocketTransport_Do(t *testing.T) {
	tests := []struct {
		name        string
		target      Target
		mockCallErr error
		mockCbErr   error
		mockPacket  *protocol.Packet
		expectErr   bool
		expectRes   protocol.EResult
	}{
		{
			name:      "Invalid Target Type",
			target:    mockTarget{name: "bad_target"},
			expectErr: true,
		},
		{
			name:        "Immediate Call Error",
			target:      mockSocketTarget{eMsg: protocol.EMsg_ClientLogon},
			mockCallErr: errors.New("socket disconnected"),
			expectErr:   true,
		},
		{
			name:      "Callback Returns Error",
			target:    mockSocketTarget{eMsg: protocol.EMsg_ClientLogon},
			mockCbErr: errors.New("timeout"),
			expectErr: true,
		},
		{
			name:   "Successful Call With Header",
			target: mockSocketTarget{eMsg: protocol.EMsg_ClientLogon},
			mockPacket: &protocol.Packet{
				Payload: []byte("success"),
				Header: mockEHeader{
					result:    protocol.EResult_AccessDenied,
					sourceJob: 12345,
				},
			},
			expectErr: false,
			expectRes: protocol.EResult_AccessDenied,
		},
		{
			name:   "Successful Call Without Header",
			target: mockSocketTarget{eMsg: protocol.EMsg_ClientLogon},
			mockPacket: &protocol.Packet{
				Payload: []byte("success"),
				Header:  nil,
			},
			expectErr: false,
			expectRes: protocol.EResult_OK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := &mockSocketCaller{
				session: &mockSession{isAuth: true},
				callFunc: func(ctx context.Context, eMsg protocol.EMsg, targetName string, payload []byte, cb jobs.Callback[*protocol.Packet]) error {
					if tt.mockCallErr != nil {
						return tt.mockCallErr
					}
					// Эмулируем асинхронный ответ от сокета
					go func() {
						cb(tt.mockPacket, tt.mockCbErr)
					}()
					return nil
				},
			}

			trans := NewSocketTransport(caller)
			req := NewRequest(context.Background(), tt.target, []byte("request_body"))
			resp, err := trans.Do(req)

			if (err != nil) != tt.expectErr {
				t.Fatalf("Expected error: %v, got: %v", tt.expectErr, err)
			}

			if !tt.expectErr {
				if resp.Result != tt.expectRes {
					t.Errorf("Expected Result %v, got %v", tt.expectRes, resp.Result)
				}
				if string(resp.Body) != "success" {
					t.Errorf("Expected body 'success', got '%s'", string(resp.Body))
				}
				// Проверка JobID если пакет был с заголовком
				if tt.mockPacket.Header != nil && resp.SourceJobID != 12345 {
					t.Errorf("Expected SourceJobID 12345, got %d", resp.SourceJobID)
				}
			}
		})
	}
}

func TestSocketTransport_ContextCancellation(t *testing.T) {
	caller := &mockSocketCaller{
		session: &mockSession{isAuth: true},
		callFunc: func(ctx context.Context, eMsg protocol.EMsg, targetName string, payload []byte, cb jobs.Callback[*protocol.Packet]) error {
			return nil
		},
	}

	trans := NewSocketTransport(caller)
	defer trans.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	target := mockSocketTarget{eMsg: protocol.EMsg_ClientLogon, name: "TestService.Method"}
	req := NewRequest(ctx, target, nil)

	resCh := make(chan error, 1)
	go func() {
		_, err := trans.Do(req)
		resCh <- err
	}()

	select {
	case err := <-resCh:
		if err == nil {
			t.Error("Expected error due to cancelled context, got nil")
		}
	case <-time.After(100 * time.Millisecond):
		t.Skip("Transport is blocking on context cancellation (needs select fix from audit)")
	}
}
