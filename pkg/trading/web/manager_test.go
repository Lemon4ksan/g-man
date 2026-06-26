// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/processor"
	"github.com/lemon4ksan/g-man/test/mock"
)

const (
	TestOfferID    = 12345
	OtherAccountID = 45678
)

func setupTrading(t *testing.T) (*Manager, *mock.ServiceMock, *mock.HTTPStub) {
	t.Helper()

	web := mock.NewServiceMock()
	comm := mock.NewHTTPStub()

	init := mock.NewInitContext()
	init.SetService(web)

	m := New(DefaultConfig())
	m.rateLimiter = rate.NewLimiter(rate.Inf, 0)

	if err := m.Init(init); err != nil {
		t.Fatalf("failed to init module: %v", err)
	}

	m.community = comm

	return m, web, comm
}

func TestManager_Lifecycle(t *testing.T) {
	m, _, _ := setupTrading(t)

	t.Run("State Transitions", func(t *testing.T) {
		if m.fsm.CurrentState() != StateStopped {
			t.Errorf("expected Stopped, got %d", m.fsm.CurrentState())
		}

		if err := m.StartPolling(); err != nil {
			t.Fatalf("failed to start: %v", err)
		}

		if m.fsm.CurrentState() != StatePolling {
			t.Errorf("expected Polling, got %d", m.fsm.CurrentState())
		}

		if err := m.StartPolling(); !errors.Is(err, ErrManagerPolling) {
			t.Errorf("expected ErrManagerPolling, got %v", err)
		}

		m.StopPolling()

		if m.fsm.CurrentState() != StateStopped {
			t.Errorf("expected Stopped after stop, got %d", m.fsm.CurrentState())
		}

		_ = m.Close()
		if m.fsm.CurrentState() != StateClosed {
			t.Errorf("expected Closed, got %d", m.fsm.CurrentState())
		}
	})
}

func TestManager_PollingLogic(t *testing.T) {
	m, web, _ := setupTrading(t)
	ctx := context.Background()

	subNew := m.Bus.Subscribe(&NewOfferEvent{})

	t.Run("Detect New Offer", func(t *testing.T) {
		web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
			"response": map[string]any{
				"trade_offers_received": []any{
					map[string]any{
						"tradeofferid":      strconv.FormatUint(12345, 10),
						"trade_offer_state": int(trading.OfferStateActive),
						"accountid_other":   999,
					},
				},
			},
		})

		m.doPoll(ctx)

		select {
		case ev := <-subNew.C():
			if ev.(*NewOfferEvent).Offer.ID != 12345 {
				t.Error("wrong offer ID in NewOfferEvent")
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("NewOfferEvent not received")
		}
	})

	t.Run("Detect State Change", func(t *testing.T) {
		web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
			"response": map[string]any{
				"trade_offers_received": []any{
					map[string]any{
						"tradeofferid":      strconv.Itoa(TestOfferID),
						"trade_offer_state": int(trading.OfferStateAccepted),
					},
				},
			},
		})
	})
}

func TestManager_GetEscrowDuration(t *testing.T) {
	m, _, comm := setupTrading(t)

	genHTML := func(my, their int) string {
		return fmt.Sprintf("var g_daysMyEscrow = %d; var g_DaysTheirEscrow = %d;", my, their)
	}

	tests := []struct {
		name    string
		offerID uint64
		html    string
		mockErr error
		want    processor.Details
		wantErr error
	}{
		{
			name:    "Hold 7 days",
			offerID: 100,
			html:    genHTML(0, 7),
			want:    processor.Details{MyDays: 0, TheirDays: 7},
		},
		{
			name:    "No hold",
			offerID: 200,
			html:    genHTML(0, 0),
			want:    processor.Details{MyDays: 0, TheirDays: 0},
		},
		{
			name:    "Parsing error",
			offerID: 300,
			html:    "<html>No data here</html>",
			wantErr: processor.ErrEscrowNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := fmt.Sprintf("tradeoffer/%d/", tt.offerID)
			if tt.mockErr != nil {
				comm.ResponseErrs[path] = tt.mockErr
			} else {
				comm.SetHTMLResponse(path, 200, tt.html)
			}

			details, err := m.GetEscrowDuration(context.Background(), tt.offerID)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}

				if !errors.Is(err, tt.wantErr) {
					t.Errorf("got error %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if !reflect.DeepEqual(details, tt.want) {
					t.Errorf("got details %+v, want %+v", details, tt.want)
				}
			}
		})
	}
}

func TestManager_HandleAuthEvents(t *testing.T) {
	m, _, _ := setupTrading(t)

	sub := m.Bus.Subscribe(auth.StateEvent{})
	m.Go(func(ctx context.Context) {
		m.listenEvents(ctx, sub)
	})

	if err := m.StartPolling(); err != nil {
		t.Fatal(err)
	}

	m.Bus.Publish(&auth.StateEvent{
		Old: auth.StateLoggedOn,
		New: auth.StateDisconnected,
	})

	time.Sleep(100 * time.Millisecond)

	if m.fsm.CurrentState() != StateStopped {
		t.Errorf("expected state Stopped after Disconnect event, got %d", m.fsm.CurrentState())
	}
}

func TestManager_ParseTradeURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantID    uint64
		wantToken string
		wantErr   bool
	}{
		{
			name:      "Valid trade URL with token",
			url:       "https://steamcommunity.com/tradeoffer/new/?partner=12345678&token=xxxxxxxx",
			wantID:    76561197972611406, // Account ID 12345678 converted to individual SteamID
			wantToken: "xxxxxxxx",
			wantErr:   false,
		},
		{
			name:      "Valid trade URL without token",
			url:       "https://steamcommunity.com/tradeoffer/new/?partner=87654321",
			wantID:    76561198047920049,
			wantToken: "",
			wantErr:   false,
		},
		{
			name:    "Invalid URL missing partner",
			url:     "https://steamcommunity.com/tradeoffer/new/?token=xxxxxxxx",
			wantErr: true,
		},
		{
			name:    "Malformed URL",
			url:     "://invalid-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sid, token, err := ParseTradeURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error: %v, got: %v", tt.wantErr, err)
			}

			if err == nil {
				if sid.Uint64() != tt.wantID {
					t.Errorf("got partner ID %d, want %d", sid.Uint64(), tt.wantID)
				}

				if token != tt.wantToken {
					t.Errorf("got token %s, want %s", token, tt.wantToken)
				}
			}
		})
	}
}

func TestManager_PollData(t *testing.T) {
	m, _, _ := setupTrading(t)

	// Verify initial empty state
	pd := m.GetPollData()
	if pd.OffersSince != 0 || len(pd.Sent) != 0 || len(pd.Received) != 0 {
		t.Errorf("expected empty PollData, got %+v", pd)
	}

	// Restore custom state
	customData := trading.PollData{
		OffersSince: 1700000000,
		Sent: map[uint64]trading.OfferState{
			111: trading.OfferStateActive,
		},
		Received: map[uint64]trading.OfferState{
			222: trading.OfferStateAccepted,
		},
	}
	m.SetPollData(customData)

	pd = m.GetPollData()
	if pd.OffersSince != 1700000000 {
		t.Errorf("expected OffersSince 1700000000, got %d", pd.OffersSince)
	}

	if pd.Sent[111] != trading.OfferStateActive {
		t.Errorf("expected sent 111 state to be Active, got %v", pd.Sent[111])
	}

	if pd.Received[222] != trading.OfferStateAccepted {
		t.Errorf("expected received 222 state to be Accepted, got %v", pd.Received[222])
	}
}

func TestManager_PollDataEvent(t *testing.T) {
	m, web, _ := setupTrading(t)
	ctx := context.Background()

	sub := m.Bus.Subscribe(&PollDataEvent{})
	defer sub.Unsubscribe()

	web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
		"response": map[string]any{
			"trade_offers_received": []any{
				map[string]any{
					"tradeofferid":      "123",
					"trade_offer_state": int(trading.OfferStateActive),
					"time_updated":      1710000000,
				},
			},
		},
	})

	m.doPoll(ctx)

	select {
	case ev := <-sub.C():
		eventData := ev.(*PollDataEvent).PollData
		if eventData.OffersSince != 1710000000 {
			t.Errorf("expected OffersSince in event to be 1710000000, got %d", eventData.OffersSince)
		}

		if eventData.Received[123] != trading.OfferStateActive {
			t.Errorf("expected received offer 123 to be Active in PollDataEvent, got %v", eventData.Received[123])
		}

	case <-time.After(500 * time.Millisecond):
		t.Fatal("PollDataEvent was not generated")
	}
}

func TestManager_AutoCancellation(t *testing.T) {
	t.Run("CancelTime auto-cancellation", func(t *testing.T) {
		m, web, _ := setupTrading(t)
		m.config.CancelTime = 1 * time.Hour
		ctx := context.Background()

		// Set response with an offer that is old enough
		web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
			"response": map[string]any{
				"trade_offers_sent": []any{
					map[string]any{
						"tradeofferid":      "999",
						"trade_offer_state": int(trading.OfferStateActive),
						"is_our_offer":      true,
						"time_updated":      time.Now().Add(-2 * time.Hour).Unix(),
					},
				},
			},
		})

		// Track CancelTradeOffer calls using OnDo
		var (
			mu           sync.Mutex
			cancelCalled bool
			cancelID     uint64
		)

		web.OnDo = func(req *tr.Request) (*tr.Response, error) {
			target := req.Target()
			if webTarget, ok := target.(*service.WebAPITarget); ok && webTarget.Interface == "IEconService" &&
				webTarget.Method == "CancelTradeOffer" {
				mu.Lock()
				cancelCalled = true
				idStr := req.Params().Get("tradeofferid")
				cancelID, _ = strconv.ParseUint(idStr, 10, 64)
				mu.Unlock()

				return tr.NewResponse(
					io.NopCloser(bytes.NewReader([]byte("{}"))),
					tr.HTTPMetadata{StatusCode: 200},
				), nil
			}

			return nil, nil
		}

		m.doPoll(ctx)

		// Wait a little since cancellation runs in a goroutine
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		called := cancelCalled
		cid := cancelID
		mu.Unlock()

		if !called {
			t.Error("CancelTradeOffer was not called for expired offer")
		}

		if cid != 999 {
			t.Errorf("expected cancel offer 999, got %d", cid)
		}
	})

	t.Run("CancelOfferCount limit auto-cancellation", func(t *testing.T) {
		m, web, _ := setupTrading(t)
		m.config.CancelOfferCount = 2
		m.config.CancelOfferCountMinAge = 5 * time.Minute
		ctx := context.Background()

		// Set response with 2 active sent offers (reaches limit)
		web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
			"response": map[string]any{
				"trade_offers_sent": []any{
					map[string]any{
						"tradeofferid":      "1001",
						"trade_offer_state": int(trading.OfferStateActive),
						"is_our_offer":      true,
						"time_updated":      time.Now().Add(-10 * time.Minute).Unix(), // Oldest
					},
					map[string]any{
						"tradeofferid":      "1002",
						"trade_offer_state": int(trading.OfferStateActive),
						"is_our_offer":      true,
						"time_updated":      time.Now().Add(-6 * time.Minute).Unix(),
					},
				},
			},
		})

		var (
			mu           sync.Mutex
			cancelCalled bool
			cancelID     uint64
		)

		web.OnDo = func(req *tr.Request) (*tr.Response, error) {
			target := req.Target()
			if webTarget, ok := target.(*service.WebAPITarget); ok && webTarget.Interface == "IEconService" &&
				webTarget.Method == "CancelTradeOffer" {
				mu.Lock()
				cancelCalled = true
				idStr := req.Params().Get("tradeofferid")
				cancelID, _ = strconv.ParseUint(idStr, 10, 64)
				mu.Unlock()

				return tr.NewResponse(
					io.NopCloser(bytes.NewReader([]byte("{}"))),
					tr.HTTPMetadata{StatusCode: 200},
				), nil
			}

			return nil, nil
		}

		m.doPoll(ctx)

		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		called := cancelCalled
		cid := cancelID
		mu.Unlock()

		if !called {
			t.Error("CancelTradeOffer was not called when limit was reached")
		}

		if cid != 1001 {
			t.Errorf("expected oldest offer 1001 to be cancelled, got %d", cid)
		}
	})
}

func TestManager_GetExchangeDetails(t *testing.T) {
	m, web, _ := setupTrading(t)
	ctx := context.Background()

	web.SetJSONResponse("IEconService", "GetTradeStatus", map[string]any{
		"response": map[string]any{
			"trades": []any{
				map[string]any{
					"tradeid":       "88888",
					"steamid_other": "76561198083721406",
					"time_init":     1710000000,
					"status":        3, // Complete
					"assets_received": []any{
						map[string]any{
							"appid":         440,
							"contextid":     "2",
							"assetid":       "111",
							"new_assetid":   "222",
							"new_contextid": "2",
							"amount":        "1",
						},
					},
					"assets_given": []any{
						map[string]any{
							"appid":         440,
							"contextid":     "2",
							"assetid":       "333",
							"new_assetid":   "444",
							"new_contextid": "2",
							"amount":        "1",
						},
					},
				},
			},
		},
	})

	details, err := m.GetExchangeDetails(ctx, 88888)
	if err != nil {
		t.Fatalf("GetExchangeDetails failed: %v", err)
	}

	if details.Status != 3 {
		t.Errorf("got status %d, want 3", details.Status)
	}

	if details.TimeInit != 1710000000 {
		t.Errorf("got time_init %d, want 1710000000", details.TimeInit)
	}

	if len(details.AssetsReceived) != 1 || details.AssetsReceived[0].NewAssetID != 222 {
		t.Errorf("wrong assets received: %+v", details.AssetsReceived)
	}

	if len(details.AssetsGiven) != 1 || details.AssetsGiven[0].NewAssetID != 444 {
		t.Errorf("wrong assets given: %+v", details.AssetsGiven)
	}
}
