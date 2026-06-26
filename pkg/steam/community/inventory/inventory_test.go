// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/community/inventory"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/test/mock"
)

const testUserID = uint64(76561198000000000)

func newBuffer(t *testing.T, b []byte) io.ReadCloser {
	t.Helper()
	return io.NopCloser(bytes.NewReader(b))
}

func jsonResponse(t *testing.T, v any) []byte {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal json response: %v", err)
	}

	return b
}

func mockEmptyHistory(t *testing.T, m *mock.ServiceMock) {
	t.Helper()

	m.OnRest = func(method, path string, body any) (*http.Response, error) {
		assert.Contains(t, path, "inventoryhistory")

		return &http.Response{
			StatusCode: http.StatusOK,
			Body: newBuffer(
				t,
				[]byte(
					`<div class="inventory_history_pagingrow"></div><script>var g_rgHistoryInventory = {};</script>`,
				),
			),
		}, nil
	}
}

type inventoryResponseMock struct {
	Success      bool                    `json:"success"`
	Error        string                  `json:"error"`
	Assets       []inventory.Asset       `json:"assets"`
	Descriptions []inventory.Description `json:"descriptions"`
	MoreItems    bool                    `json:"more_items"`
	LastAssetID  string                  `json:"last_assetid"`
	TotalCount   int                     `json:"total_inventory_count"`
}

func TestGetUserInventoryContents_VariousResponses_ReturnsExpectedContents(t *testing.T) {
	t.Parallel()

	appID := uint32(730)
	contextID := int64(2)

	tests := []struct {
		name        string
		tradable    bool
		lang        string
		mockSetup   func(t *testing.T, m *mock.ServiceMock)
		wantInvLen  int
		wantCurrLen int
		wantTotal   int
		wantErr     bool
		errContent  string
	}{
		{
			name:     "success_single_page",
			tradable: false,
			lang:     "",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				resp := inventoryResponseMock{
					Success:    true,
					TotalCount: 2,
					Assets: []inventory.Asset{
						{AssetID: "1", ClassID: "100", InstanceID: "1", Amount: "1"},
						{AssetID: "2", ClassID: "200", InstanceID: "2", Amount: "1", CurrencyID: "500"},
					},
					Descriptions: []inventory.Description{
						{ClassID: "100", InstanceID: "1", Name: "Item 1", Tradable: 1},
						{ClassID: "200", InstanceID: "2", Name: "Currency 1", Tradable: 1},
					},
					MoreItems: false,
				}
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					assert.Equal(t, "inventory/{steamID}/{appID}/{contextID}", path)

					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, jsonResponse(t, resp)),
					}, nil
				}
			},
			wantInvLen:  1,
			wantCurrLen: 1,
			wantTotal:   2,
		},
		{
			name:     "success_pagination",
			tradable: false,
			lang:     "russian",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				callCount := 0

				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					callCount++

					var resp inventoryResponseMock
					if callCount == 1 {
						resp = inventoryResponseMock{
							Success:      true,
							TotalCount:   2,
							MoreItems:    true,
							LastAssetID:  "100",
							Assets:       []inventory.Asset{{AssetID: "1", ClassID: "A", InstanceID: "A"}},
							Descriptions: []inventory.Description{{ClassID: "A", InstanceID: "A"}},
						}
					} else {
						resp = inventoryResponseMock{
							Success:      true,
							TotalCount:   2,
							MoreItems:    false,
							Assets:       []inventory.Asset{{AssetID: "2", ClassID: "B", InstanceID: "B"}},
							Descriptions: []inventory.Description{{ClassID: "B", InstanceID: "B"}},
						}
					}

					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, jsonResponse(t, resp)),
					}, nil
				}
			},
			wantInvLen: 2,
			wantTotal:  2,
		},
		{
			name: "empty_inventory",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				resp := inventoryResponseMock{Success: true, TotalCount: 0, Assets: nil}

				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, jsonResponse(t, resp)),
					}, nil
				}
			},
			wantInvLen: 0,
			wantTotal:  0,
		},
		{
			name: "asset_missing_description_skipping",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				resp := inventoryResponseMock{
					Success:    true,
					TotalCount: 1,
					Assets: []inventory.Asset{
						{AssetID: "1", ClassID: "999", InstanceID: "999"},
					},
					Descriptions: []inventory.Description{},
				}

				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, jsonResponse(t, resp)),
					}, nil
				}
			},
			wantInvLen: 0,
			wantTotal:  1,
		},
		{
			name:     "tradable_only_filter",
			tradable: true,
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				resp := inventoryResponseMock{
					Success:    true,
					TotalCount: 2,
					Assets: []inventory.Asset{
						{AssetID: "1", ClassID: "1", InstanceID: "1"},
						{AssetID: "2", ClassID: "2", InstanceID: "2"},
					},
					Descriptions: []inventory.Description{
						{ClassID: "1", InstanceID: "1", Tradable: 1},
						{ClassID: "2", InstanceID: "2", Tradable: 0},
					},
				}

				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, jsonResponse(t, resp)),
					}, nil
				}
			},
			wantInvLen: 1,
			wantTotal:  2,
		},
		{
			name: "steam_error_response",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				resp := inventoryResponseMock{
					Success: false,
					Error:   "Rate limit exceeded",
				}

				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, jsonResponse(t, resp)),
					}, nil
				}
			},
			wantErr:    true,
			errContent: "steam error: Rate limit exceeded",
		},
		{
			name: "requester_error",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return nil, errors.New("network fail")
				}
			},
			wantErr:    true,
			errContent: "network fail",
		},
		{
			name: "non_ok_http_status_code",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Body:       newBuffer(t, []byte("Too Many Requests")),
					}, nil
				}
			},
			wantErr: true,
		},
		{
			name: "malformed_json_response",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte("{invalid json")),
					}, nil
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockService := mock.NewServiceMock()
			if tt.mockSetup != nil {
				tt.mockSetup(t, mockService)
			}

			inv, curr, total, err := inventory.GetUserInventoryContents(
				t.Context(), mockService, testUserID, appID, contextID, tt.tradable, tt.lang,
			)

			if tt.wantErr {
				require.Error(t, err)

				if tt.errContent != "" {
					assert.Contains(t, err.Error(), tt.errContent)
				}

				return
			}

			require.NoError(t, err)
			assert.Len(t, inv, tt.wantInvLen)
			assert.Len(t, curr, tt.wantCurrLen)

			if tt.wantTotal != 0 || tt.wantInvLen > 0 {
				assert.Equal(t, tt.wantTotal, total)
			}
		})
	}
}

func TestGetUserInventoryContexts_VariousResponses_ReturnsExpectedContexts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mockSetup  func(t *testing.T, m *mock.ServiceMock)
		wantErr    bool
		errContent string
		validateFn func(t *testing.T, contexts map[string]*inventory.AppContext)
	}{
		{
			name: "success_scraped_app_context",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				htmlContent := `
					var g_rgAppContextData = {
						"730": {
							"appid": 730,
							"name": "Counter-Strike 2",
							"icon": "https://media.steampowered.com/apps/730.jpg",
							"link": "https://steamcommunity.com/app/730",
							"asset_count": 5,
							"rgContexts": {
								"2": {
									"id": "2",
									"name": "Backpack",
									"asset_count": 5
								}
							}
						}
					};
				`
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					assert.Equal(t, "profiles/{userID}/inventory", path)

					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(htmlContent)),
					}, nil
				}
			},
			validateFn: func(t *testing.T, contexts map[string]*inventory.AppContext) {
				require.Len(t, contexts, 1)

				cs2 := contexts["730"]
				require.NotNil(t, cs2)
				assert.Equal(t, uint32(730), cs2.AppID)
				assert.Equal(t, "Counter-Strike 2", cs2.Name)
				assert.Equal(t, 5, cs2.AssetCount)
				assert.Contains(t, cs2.Contexts, "2")
				assert.Equal(t, "Backpack", cs2.Contexts["2"].Name)
			},
		},
		{
			name: "success_empty_context_list",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(`var g_rgAppContextData = [];`)),
					}, nil
				}
			},
			validateFn: func(t *testing.T, contexts map[string]*inventory.AppContext) {
				assert.Empty(t, contexts)
			},
		},
		{
			name: "private_profile_error",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(`This profile is private.`)),
					}, nil
				}
			},
			wantErr:    true,
			errContent: "profile is private",
		},
		{
			name: "private_inventory_error_1",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(`The inventory is currently private.`)),
					}, nil
				}
			},
			wantErr:    true,
			errContent: "inventory is private",
		},
		{
			name: "private_inventory_error_2",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(`inventory is currently private`)),
					}, nil
				}
			},
			wantErr:    true,
			errContent: "inventory is private",
		},
		{
			name: "regex_extraction_error",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(`<html>no appContextData here</html>`)),
					}, nil
				}
			},
			wantErr:    true,
			errContent: "g_rgAppContextData not found",
		},
		{
			name: "unmarshal_error",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(`var g_rgAppContextData = {invalid: json};`)),
					}, nil
				}
			},
			wantErr:    true,
			errContent: "failed to parse context data JSON",
		},
		{
			name: "requester_html_failure",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return nil, errors.New("network failure")
				}
			},
			wantErr:    true,
			errContent: "failed to fetch inventory page",
		},
		{
			name: "non_ok_http_status_code",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       newBuffer(t, []byte("Internal Server Error")),
					}, nil
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockService := mock.NewServiceMock()
			if tt.mockSetup != nil {
				tt.mockSetup(t, mockService)
			}

			contexts, err := inventory.GetUserInventoryContexts(t.Context(), mockService, testUserID)

			if tt.wantErr {
				require.Error(t, err)

				if tt.errContent != "" {
					assert.Contains(t, err.Error(), tt.errContent)
				}

				return
			}

			require.NoError(t, err)

			if tt.validateFn != nil {
				tt.validateFn(t, contexts)
			}
		})
	}
}

func TestGetInventoryHistory_VariousOptions_ReturnsExpectedHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       inventory.HistoryOptions
		mockSetup  func(t *testing.T, m *mock.ServiceMock)
		wantErr    bool
		errContent string
		validateFn func(t *testing.T, result *inventory.TradeHistoryResult)
	}{
		{
			name: "network_failure",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return nil, errors.New("network fail")
				}
			},
			wantErr:    true,
			errContent: "failed to fetch inventory history page",
		},
		{
			name: "malformed_html_missing_paging_row",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(`<div>missing paging row</div>`)),
					}, nil
				}
			},
			wantErr:    true,
			errContent: "paging row not found",
		},
		{
			name: "malformed_html_missing_history_inventory_block",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(`<div class="inventory_history_pagingrow"></div>`)),
					}, nil
				}
			},
			wantErr:    true,
			errContent: "g_rgHistoryInventory not found",
		},
		{
			name: "malformed_html_invalid_history_inventory_json",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					htmlContent := `
						<div class="inventory_history_pagingrow"></div>
						<script>var g_rgHistoryInventory = {invalid};</script>
					`

					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(htmlContent)),
					}, nil
				}
			},
			wantErr:    true,
			errContent: "failed to parse history inventory JSON",
		},
		{
			name: "success_parsing_complete_page",
			opts: func() inventory.HistoryOptions {
				startTime := time.Unix(1700000000, 0).UTC()
				startTrade := uint64(11111111)

				return inventory.HistoryOptions{
					StartTime:  &startTime,
					StartTrade: &startTrade,
					Direction:  inventory.DirectionFuture,
				}
			}(),
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				htmlContent := `
					<div class="inventory_history_pagingrow">
						<div class="inventory_history_nextbtn">
							<a class="pagebtn">No Href At All</a>
							<a class="pagebtn" href="https://steamcommunity.com/profiles/76561198000000000/inventoryhistory?after_time=1700000000&after_trade=11111111&prev=1">Prev</a>
							<a class="pagebtn" href="https://steamcommunity.com/profiles/76561198000000000/inventoryhistory?after_time=1702000000&after_trade=22222222">Next</a>
							<a class="pagebtn disabled" href="https://steamcommunity.com/profiles/76561198000000000/inventoryhistory?after_time=1703000000&after_trade=33333333">Disabled</a>
						</div>
					</div>
					<script>
						var g_rgHistoryInventory = {
							"730": {
								"2": {
									"100": {
										"classid": "100",
										"instanceid": "1",
										"name": "AK-47 | Redline"
									}
								}
							}
						};

						HistoryPageCreateItemHover( 'item_received_1', 730, '2', '100', '1' );
						HistoryPageCreateItemHover( 'item_given_2', 730, '2', '100', '5' );

						HistoryPageCreateItemHover( 'item_malformed', 730 );
					</script>

					<div class="tradehistoryrow">
						<div class="tradehistory_timestamp">11:15 am</div>
						<div class="tradehistory_date">15 Oct, 2026</div>
						<div class="tradehistory_event_description">
							<a href="https://steamcommunity.com/profiles/76561198123456789">Some Partner</a>
						</div>
						<span>First dummy span</span>
						<span>Trade On Hold</span>
						<div class="history_item" id="item_received_1">AK Received</div>
						<div class="history_item" id="item_given_2">AK Given</div>
						<div class="history_item" id="item_missing_hover">Missing Hover</div>
						<div class="history_item">Missing ID</div>
					</div>

					<div class="tradehistoryrow">
						<div class="tradehistory_timestamp">12:00 AM</div>
						<div class="tradehistory_date">Oct 16</div>
						<div class="tradehistory_event_description">
							<a href="https://steamcommunity.com/id/vanity_partner">Some Vanity Partner</a>
						</div>
						<span>First dummy span</span>
						<span>Trade Completed</span>
					</div>

					<div class="tradehistoryrow">
						<div class="tradehistory_timestamp">05:45 pm</div>
						<div class="tradehistory_date">Oct 17, 2026</div>
						<div class="tradehistory_event_description">
							No anchor link
						</div>
						<span>First dummy span</span>
						<span>Trade Completed</span>
					</div>

					<div class="tradehistoryrow">
						<div class="tradehistory_timestamp">02:30 pm</div>
						<div class="tradehistory_date">Oct 18</div>
						<div class="tradehistory_event_description">
							No anchor link
						</div>
						<span>First dummy span</span>
						<span>Trade Completed</span>
					</div>

					<div class="tradehistoryrow">
						<div class="tradehistory_timestamp">Invalid_Time</div>
						<div class="tradehistory_date">Invalid_Date</div>
						<div class="tradehistory_event_description">
							<a href="">Empty Link</a>
						</div>
					</div>
				`

				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(htmlContent)),
					}, nil
				}
			},
			validateFn: func(t *testing.T, result *inventory.TradeHistoryResult) {
				require.NotNil(t, result)

				require.NotNil(t, result.FirstTradeTime)
				assert.Equal(t, int64(1700000000), result.FirstTradeTime.Unix())
				require.NotNil(t, result.FirstTradeID)
				assert.Equal(t, uint64(11111111), *result.FirstTradeID)

				require.NotNil(t, result.LastTradeTime)
				assert.Equal(t, int64(1702000000), result.LastTradeTime.Unix())
				require.NotNil(t, result.LastTradeID)
				assert.Equal(t, uint64(22222222), *result.LastTradeID)

				require.Len(t, result.Trades, 5)

				row1 := result.Trades[0]
				assert.True(t, row1.OnHold)
				assert.Equal(t, "Some Partner", row1.PartnerName)
				assert.Equal(t, id.ID(76561198123456789), row1.PartnerSteamID)
				assert.Empty(t, row1.PartnerVanityURL)

				expectedRow1Time := time.Date(2026, time.October, 15, 11, 15, 0, 0, time.UTC)
				assert.Equal(t, expectedRow1Time, row1.Date)

				require.Len(t, row1.ItemsReceived, 1)
				assert.Equal(t, "AK-47 | Redline", row1.ItemsReceived[0].Name)
				assert.Equal(t, 1, row1.ItemsReceived[0].Amount)

				require.Len(t, row1.ItemsGiven, 1)
				assert.Equal(t, "AK-47 | Redline", row1.ItemsGiven[0].Name)
				assert.Equal(t, 5, row1.ItemsGiven[0].Amount)

				row2 := result.Trades[1]
				assert.False(t, row2.OnHold)
				assert.Equal(t, "Some Vanity Partner", row2.PartnerName)
				assert.Equal(t, "vanity_partner", row2.PartnerVanityURL)
				assert.Equal(t, id.ID(0), row2.PartnerSteamID)

				currentYear := time.Now().UTC().Year()
				expectedRow2Time := time.Date(currentYear, time.October, 16, 0, 0, 0, 0, time.UTC)
				assert.Equal(t, expectedRow2Time, row2.Date)

				row3 := result.Trades[2]
				expectedRow3Time := time.Date(2026, time.October, 17, 17, 45, 0, 0, time.UTC)
				assert.Equal(t, expectedRow3Time, row3.Date)
				assert.Empty(t, row3.PartnerName)

				row4 := result.Trades[3]
				expectedRow4Time := time.Date(currentYear, time.October, 18, 14, 30, 0, 0, time.UTC)
				assert.Equal(t, expectedRow4Time, row4.Date)

				row5 := result.Trades[4]
				assert.True(t, row5.Date.IsZero())
			},
		},
		{
			name: "pagination_href_non_numeric_parsing",
			opts: inventory.HistoryOptions{
				Direction: inventory.DirectionPast,
			},
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				htmlContent := `
					<div class="inventory_history_pagingrow">
						<div class="inventory_history_nextbtn">
							<a class="pagebtn" href="https://steamcommunity.com/profiles/123/inventoryhistory?after_time=not_number&after_trade=987654321">Next</a>
							<a class="pagebtn" href="https://steamcommunity.com/profiles/123/inventoryhistory?after_time=1700000000&after_trade=not_number">Next</a>
						</div>
					</div>
					<script>var g_rgHistoryInventory = {};</script>
				`
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(htmlContent)),
					}, nil
				}
			},
			validateFn: func(t *testing.T, result *inventory.TradeHistoryResult) {
				assert.Nil(t, result.FirstTradeTime)
				assert.Nil(t, result.LastTradeTime)
			},
		},
		{
			name: "item_lookup_edge_cases",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				htmlContent := `
					<div class="inventory_history_pagingrow"></div>
					<script>
						var g_rgHistoryInventory = {
							"730": {
								"2": {
									"100": {
										"classid": "100",
										"instanceid": "1",
										"name": "AK-47 | Redline"
									}
								}
							}
						};

						HistoryPageCreateItemHover( 'item_missing_app', 999, '2', '100', '1' );
						HistoryPageCreateItemHover( 'item_missing_context', 730, '99', '100', '1' );
						HistoryPageCreateItemHover( 'item_missing_asset', 730, '2', '999', '1' );
					</script>
					<div class="tradehistoryrow">
						<div class="tradehistory_timestamp">11:15 am</div>
						<div class="tradehistory_date">15 Oct, 2026</div>
						<div class="history_item" id="item_missing_app">App Miss</div>
						<div class="history_item" id="item_missing_context">Context Miss</div>
						<div class="history_item" id="item_missing_asset">Asset Miss</div>
					</div>
				`
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       newBuffer(t, []byte(htmlContent)),
					}, nil
				}
			},
			validateFn: func(t *testing.T, result *inventory.TradeHistoryResult) {
				require.Len(t, result.Trades, 1)

				row := result.Trades[0]
				assert.Empty(t, row.ItemsReceived)
				assert.Empty(t, row.ItemsGiven)
			},
		},
		{
			name: "non_ok_http_status_code",
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusForbidden,
						Body:       newBuffer(t, []byte("Forbidden")),
					}, nil
				}
			},
			wantErr: true,
		},
		{
			name: "history_options_with_only_start_trade",
			opts: func() inventory.HistoryOptions {
				startTrade := uint64(11111111)

				return inventory.HistoryOptions{
					StartTrade: &startTrade,
				}
			}(),
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				mockEmptyHistory(t, m)
			},
		},
		{
			name: "history_options_with_only_start_time",
			opts: func() inventory.HistoryOptions {
				startTime := time.Now()

				return inventory.HistoryOptions{
					StartTime: &startTime,
				}
			}(),
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				mockEmptyHistory(t, m)
			},
		},
		{
			name: "verify_query_parameters",
			opts: func() inventory.HistoryOptions {
				startTime := time.Unix(1700000000, 0).UTC()
				startTrade := uint64(11111111)

				return inventory.HistoryOptions{
					StartTime:  &startTime,
					StartTrade: &startTrade,
					Direction:  inventory.DirectionFuture,
				}
			}(),
			mockSetup: func(t *testing.T, m *mock.ServiceMock) {
				mockEmptyHistory(t, m)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockService := mock.NewServiceMock()
			if tt.mockSetup != nil {
				tt.mockSetup(t, mockService)
			}

			result, err := inventory.GetInventoryHistory(t.Context(), mockService, id.ID(testUserID), tt.opts)

			if tt.wantErr {
				require.Error(t, err)

				if tt.errContent != "" {
					assert.Contains(t, err.Error(), tt.errContent)
				}

				return
			}

			require.NoError(t, err)

			if tt.validateFn != nil {
				tt.validateFn(t, result)
			}
		})
	}
}
