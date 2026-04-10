// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

type mockSocketCaller struct {
	session     *mockSession
	mockCallErr error
	mockPacket  *protocol.Packet
	mockCbErr   error
}

func (m *mockSocketCaller) Session() socket.Session { return m.session }

func (m *mockSocketCaller) SendSync(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) (*protocol.Packet, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if m.mockCallErr != nil {
		return nil, m.mockCallErr
	}

	if m.mockCbErr != nil {
		return nil, m.mockCbErr
	}

	return m.mockPacket, nil
}

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
				session:     &mockSession{isAuth: true},
				mockCallErr: tt.mockCallErr,
				mockPacket:  tt.mockPacket,
				mockCbErr:   tt.mockCbErr,
			}

			trans := NewSocketTransport(caller)
			req := NewRequest(tt.target, []byte("request_body"))

			resp, err := trans.Do(t.Context(), req)

			if (err != nil) != tt.expectErr {
				t.Fatalf("Expected error: %v, got: %v", tt.expectErr, err)
			}

			if !tt.expectErr {
				meta, _ := resp.Socket()
				if meta.Result != tt.expectRes {
					t.Errorf("Expected Result %v, got %v", tt.expectRes, meta.Result)
				}
				if string(resp.Body) != "success" {
					t.Errorf("Expected body 'success', got '%s'", string(resp.Body))
				}
				if tt.mockPacket.Header != nil && meta.SourceJobID != 12345 {
					t.Errorf("Expected SourceJobID 12345, got %d", meta.SourceJobID)
				}
			}
		})
	}
}

func TestSocketTransport_ContextCancellation(t *testing.T) {
	caller := &mockSocketCaller{
		session: &mockSession{isAuth: true},
	}

	ctx, cancel := context.WithCancel(t.Context())
	target := mockSocketTarget{eMsg: protocol.EMsg_ClientLogon, name: "TestService.Method"}
	req := NewRequest(target, nil)

	resCh := make(chan error, 1)
	go func() {
		_, err := NewSocketTransport(caller).Do(ctx, req)
		resCh <- err
	}()
	cancel()

	select {
	case err := <-resCh:
		if err == nil {
			t.Fatal("Expected error due to cancelled context, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Transport is blocking and ignoring context cancellation")
	}
}
