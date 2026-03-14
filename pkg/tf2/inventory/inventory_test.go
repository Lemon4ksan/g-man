// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/transport"
)

type mockHTTPClient struct {
	responses map[string]*http.Response
	err       error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	if resp, ok := m.responses[req.URL.String()]; ok {
		return resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}, nil
}

type mockWebAPIRequester struct {
	apiError error
	response PlayerItemsResponse
}

func (m *mockWebAPIRequester) CallWebAPI(ctx context.Context, httpMethod, iface, method string, version int, respMsg any, mods ...api.RequestModifier) error {
	if m.apiError != nil {
		return m.apiError
	}

	val := reflect.ValueOf(respMsg)
	if val.Kind() == reflect.Ptr {
		val.Elem().Set(reflect.ValueOf(m.response))
	}
	return nil
}

func (m *mockWebAPIRequester) Do(req *transport.Request) (*transport.Response, error) {
	return nil, nil
}

func createBptfHTML(hasTable bool, isDuped bool) string {
	var sb strings.Builder
	sb.WriteString("<html><body>")
	if hasTable {
		sb.WriteString("<table><tr><td>History</td></tr></table>")
		if isDuped {
			sb.WriteString(`<button id="dupe-modal-btn">Duplicated</button>`)
		}
	}
	sb.WriteString("</body></html>")
	return sb.String()
}

func TestPlayerInventory_getItemHistory(t *testing.T) {
	tests := []struct {
		name         string
		assetID      uint64
		httpResponse *http.Response
		httpErr      error
		wantResult   ItemHistoryResult
		wantErr      bool
	}{
		{
			name:    "HTTP Error",
			assetID: 123,
			httpErr: errors.New("timeout"),
			wantErr: true,
		},
		{
			name:    "Status 404 - Not recorded",
			assetID: 123,
			httpResponse: &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			},
			wantResult: ItemHistoryResult{Recorded: false, IsDuped: false},
			wantErr:    false,
		},
		{
			name:    "Status 500 - Server Error",
			assetID: 123,
			httpResponse: &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			},
			wantErr: true,
		},
		{
			name:    "Recorded, Clean (Table exists, no dupe button)",
			assetID: 123,
			httpResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(createBptfHTML(true, false))),
			},
			wantResult: ItemHistoryResult{Recorded: true, IsDuped: false},
			wantErr:    false,
		},
		{
			name:    "Recorded, Duped (Table exists, dupe button exists)",
			assetID: 123,
			httpResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(createBptfHTML(true, true))),
			},
			wantResult: ItemHistoryResult{Recorded: true, IsDuped: true},
			wantErr:    false,
		},
		{
			name:    "Not recorded, OK status (No table)",
			assetID: 123,
			httpResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(createBptfHTML(false, false))),
			},
			wantResult: ItemHistoryResult{Recorded: false, IsDuped: false},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHTTP := &mockHTTPClient{
				err: tt.httpErr,
				responses: map[string]*http.Response{
					"https://backpack.tf/item/123": tt.httpResponse,
				},
			}

			// Хак для обхода проблемы с типизацией: создаем структуру напрямую.
			// Если PlayerInventory использует интерфейс, подставьте его.
			inv := &PlayerInventory{
				httpClient: mockHTTP,
				bptfUserID: "test_uid",
			}

			res, err := inv.getItemHistory(context.Background(), tt.assetID)

			if (err != nil) != tt.wantErr {
				t.Errorf("getItemHistory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(res, tt.wantResult) {
				t.Errorf("getItemHistory() = %v, want %v", res, tt.wantResult)
			}
		})
	}
}

func TestPlayerInventory_fetch(t *testing.T) {
	tests := []struct {
		name        string
		apiError    error
		apiResponse PlayerItemsResponse
		wantErr     bool
		wantSlots   int
		wantItems   int
	}{
		{
			name:     "API Transport Error",
			apiError: errors.New("network down"),
			wantErr:  true,
		},
		{
			name: "Steam API Status != 1",
			apiResponse: PlayerItemsResponse{
				Result: struct {
					Status           int       `json:"status"`
					StatusDetail     string    `json:"statusDetail"`
					NumBackpackSlots int       `json:"num_backpack_slots"`
					Items            []TF2Item `json:"items"`
				}{Status: 15, StatusDetail: "access denied"},
			},
			wantErr: true,
		},
		{
			name: "Success Fetch",
			apiResponse: PlayerItemsResponse{
				Result: struct {
					Status           int       `json:"status"`
					StatusDetail     string    `json:"statusDetail"`
					NumBackpackSlots int       `json:"num_backpack_slots"`
					Items            []TF2Item `json:"items"`
				}{
					Status:           1,
					NumBackpackSlots: 3000,
					Items: []TF2Item{
						{ID: 1, OriginalID: 1},
						{ID: 2, OriginalID: 1}, // Item with history ID
					},
				},
			},
			wantErr:   false,
			wantSlots: 3000,
			wantItems: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := &mockWebAPIRequester{
				apiError: tt.apiError,
				response: tt.apiResponse,
			}

			inv := &PlayerInventory{
				steamClient: mockAPI,
			}

			err := inv.fetch(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("fetch() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if inv.slots != tt.wantSlots {
					t.Errorf("slots = %v, want %v", inv.slots, tt.wantSlots)
				}
				if len(inv.items) != tt.wantItems {
					t.Errorf("items = %v, want %v", len(inv.items), tt.wantItems)
				}
				if !inv.fetched {
					t.Error("fetched flag should be true")
				}
			}
		})
	}
}

func TestPlayerInventory_IsDuped(t *testing.T) {
	mockHTTP := &mockHTTPClient{
		responses: map[string]*http.Response{
			// Asset 100: Recorded directly on ID, Clean
			"https://backpack.tf/item/100": {
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(createBptfHTML(true, false))),
			},
			// Asset 200: NOT recorded on ID. Original ID is 50.
			"https://backpack.tf/item/200": {
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			},
			// Asset 50 (Original for 200): Recorded, Duped!
			"https://backpack.tf/item/50": {
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(createBptfHTML(true, true))),
			},
			// Asset 300: Not recorded on ID, not recorded on Original ID (60)
			"https://backpack.tf/item/300": {
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			},
			"https://backpack.tf/item/60": {
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			},
		},
	}

	inv := &PlayerInventory{
		httpClient: mockHTTP,
		fetched:    true,
		items: []TF2Item{
			{ID: 100, OriginalID: 10},
			{ID: 200, OriginalID: 50},
			{ID: 300, OriginalID: 60},
		},
	}

	tests := []struct {
		name      string
		assetID   uint64
		wantDuped *bool
		wantErr   error
	}{
		{
			name:      "Direct hit on current ID, Clean",
			assetID:   100,
			wantDuped: boolPtr(false),
			wantErr:   nil,
		},
		{
			name:      "Fallback to Original ID, Duped",
			assetID:   200,
			wantDuped: boolPtr(true),
			wantErr:   nil,
		},
		{
			name:      "Not found in history (Never premium on bptf)",
			assetID:   300,
			wantDuped: nil,
			wantErr:   nil,
		},
		{
			name:      "Item not in inventory",
			assetID:   999,
			wantDuped: nil,
			wantErr:   ErrItemNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inv.IsDuped(context.Background(), tt.assetID)

			if err != tt.wantErr {
				t.Errorf("IsDuped() error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantDuped == nil {
				if got != nil {
					t.Errorf("IsDuped() = %v, want nil", *got)
				}
			} else {
				if got == nil {
					t.Errorf("IsDuped() = nil, want %v", *tt.wantDuped)
				} else if *got != *tt.wantDuped {
					t.Errorf("IsDuped() = %v, want %v", *got, *tt.wantDuped)
				}
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}
