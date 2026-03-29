// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/lemon4ksan/g-man/test"
)

type mockRoundTripper struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

type MockDupeChecker struct {
	Responses map[uint64]HistoryStatus
	Err       error
}

func (m *MockDupeChecker) CheckHistory(ctx context.Context, id uint64) (HistoryStatus, error) {
	if m.Err != nil {
		return HistoryStatus{}, m.Err
	}
	return m.Responses[id], nil
}

func TestPlayerInventory_IsDuped(t *testing.T) {
	mockAPI := test.NewMockRequester()
	mockAPI.SetJSONResponse("IEconItems_440", "GetPlayerItems", PlayerItemsResponse{
		Result: struct {
			Status           int       `json:"status"`
			StatusDetail     string    `json:"statusDetail"`
			NumBackpackSlots int       `json:"num_backpack_slots"`
			Items            []TF2Item `json:"items"`
		}{
			Status: 1,
			Items: []TF2Item{
				{ID: 100, OriginalID: 50},
				{ID: 200, OriginalID: 200},
			},
		},
	})

	checker1 := &MockDupeChecker{
		Responses: map[uint64]HistoryStatus{
			200: {Recorded: true, IsDuped: false},
			50:  {Recorded: true, IsDuped: true},
		},
	}

	inv := New(7656119, mockAPI, checker1)

	tests := []struct {
		name      string
		assetID   uint64
		wantDuped *bool
		wantErr   error
	}{
		{
			name:      "Clean item",
			assetID:   200,
			wantDuped: boolPtr(false),
		},
		{
			name:      "Duped via OriginalID",
			assetID:   100,
			wantDuped: boolPtr(true),
		},
		{
			name:    "Item not in inventory",
			assetID: 999,
			wantErr: ErrItemNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inv.IsDuped(context.Background(), tt.assetID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("IsDuped() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantDuped == nil {
				if got != nil {
					t.Errorf("expected nil result, got %v", *got)
				}
			} else {
				if got == nil || *got != *tt.wantDuped {
					t.Errorf("IsDuped() = %v, want %v", got, tt.wantDuped)
				}
			}
		})
	}
}

func TestPlayerInventory_MultipleCheckers(t *testing.T) {
	checker1 := &MockDupeChecker{
		Responses: map[uint64]HistoryStatus{
			100: {Recorded: false},
		},
	}
	checker2 := &MockDupeChecker{
		Responses: map[uint64]HistoryStatus{
			100: {Recorded: true, IsDuped: true},
		},
	}

	inv := New(7656119, nil, checker1, checker2)

	got, _ := inv.IsDuped(context.Background(), 100)
	if got == nil || !*got {
		t.Error("expected IsDuped to be true from second checker")
	}
}

func boolPtr(b bool) *bool { return &b }
