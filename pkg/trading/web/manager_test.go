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
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/notifications"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/processor"
	"github.com/lemon4ksan/g-man/test/mock"
)

const (
	TestOfferID    = 12345
	OtherAccountID = 45678
)

type testFixture struct {
	manager *Manager
	web     *mock.ServiceMock
	comm    *mock.HTTPStub
}

func newTestFixture(t *testing.T) *testFixture {
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

	return &testFixture{
		manager: m,
		web:     web,
		comm:    comm,
	}
}

func waitState(t *testing.T, m *Manager, expected State) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	t.Cleanup(cancel)

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		if m.fsm.CurrentState() == expected {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for state %s, current: %s", expected.String(), m.fsm.CurrentState().String())
		case <-ticker.C:
		}
	}
}

type mockAuthContext struct {
	module.AuthContext
	comm *mock.HTTPStub
}

func (m *mockAuthContext) Community() community.Requester {
	return m.comm
}

func TestNew_SmallInterval_OverridesToDefault(t *testing.T) {
	t.Parallel()

	cfg := Config{
		PollInterval: 500 * time.Millisecond,
	}

	m := New(cfg)
	if m.config.PollInterval != 30*time.Second {
		t.Errorf("expected 30s poll interval, got %v", m.config.PollInterval)
	}
}

func TestStart_PersistentContext_StartsBaseLifecycle(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	err := f.manager.Start(t.Context())
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
}

func TestLifecycle_StateTransitions_BehavesExpectedly(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	m := f.manager

	if m.fsm.CurrentState() != StateStopped {
		t.Errorf("expected current state to be %s, got %s", StateStopped.String(), m.fsm.CurrentState().String())
	}

	err := m.StartPolling()
	if err != nil {
		t.Fatalf("unexpected error starting polling: %v", err)
	}

	if m.fsm.CurrentState() != StatePolling {
		t.Errorf("expected state %s, got %s", StatePolling.String(), m.fsm.CurrentState().String())
	}

	err = m.StartPolling()
	if !errors.Is(err, ErrManagerPolling) {
		t.Errorf("expected ErrManagerPolling, got %v", err)
	}

	m.StopPolling()

	if m.fsm.CurrentState() != StateStopped {
		t.Errorf("expected state %s, got %s", StateStopped.String(), m.fsm.CurrentState().String())
	}

	err = m.Close()
	if err != nil {
		t.Fatalf("unexpected error closing: %v", err)
	}

	if m.fsm.CurrentState() != StateClosed {
		t.Errorf("expected state %s, got %s", StateClosed.String(), m.fsm.CurrentState().String())
	}
}

func TestStartAuthed(t *testing.T) {
	t.Parallel()

	t.Run("valid_session", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		authCtx := &mockAuthContext{comm: f.comm}

		err := f.manager.StartAuthed(t.Context(), authCtx)
		if err != nil {
			t.Fatalf("failed to start authed: %v", err)
		}

		t.Cleanup(f.manager.StopPolling)

		if f.manager.fsm.CurrentState() != StatePolling {
			t.Errorf("expected state %s, got %s", StatePolling.String(), f.manager.fsm.CurrentState().String())
		}
	})

	t.Run("already_polling", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		authCtx := &mockAuthContext{comm: f.comm}

		if err := f.manager.StartPolling(); err != nil {
			t.Fatalf("failed to start polling: %v", err)
		}

		t.Cleanup(f.manager.StopPolling)

		err := f.manager.StartAuthed(t.Context(), authCtx)
		if err != nil {
			t.Fatalf("failed to start authed: %v", err)
		}

		if f.manager.fsm.CurrentState() != StatePolling {
			t.Errorf("expected state %s, got %s", StatePolling.String(), f.manager.fsm.CurrentState().String())
		}
	})
}

func TestPoll_NewAndUpdatedOffers_PublishesEvents(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	subNew := f.manager.Bus.Subscribe(&NewOfferEvent{})
	subChanged := f.manager.Bus.Subscribe(&OfferChangedEvent{})

	f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
		"response": map[string]any{
			"trade_offers_received": []any{
				map[string]any{
					"tradeofferid":      "12345",
					"trade_offer_state": int(trading.OfferStateActive),
					"accountid_other":   999,
					"time_updated":      1710000000,
				},
			},
		},
	})

	f.manager.doPoll(t.Context())

	select {
	case ev := <-subNew.C():
		event := ev.(*NewOfferEvent)
		if event.Offer.ID != 12345 {
			t.Errorf("expected offer ID 12345, got %d", event.Offer.ID)
		}

		if event.Offer.State != trading.OfferStateActive {
			t.Errorf("expected offer state %v, got %v", trading.OfferStateActive, event.Offer.State)
		}

	case <-t.Context().Done():
		t.Fatal("timeout waiting for NewOfferEvent")
	}

	f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
		"response": map[string]any{
			"trade_offers_received": []any{
				map[string]any{
					"tradeofferid":      "12345",
					"trade_offer_state": int(trading.OfferStateAccepted),
					"accountid_other":   999,
					"time_updated":      1710000100,
				},
			},
		},
	})

	f.manager.doPoll(t.Context())

	select {
	case ev := <-subChanged.C():
		event := ev.(*OfferChangedEvent)
		if event.Offer.ID != 12345 {
			t.Errorf("expected offer ID 12345, got %d", event.Offer.ID)
		}

		if event.OldState != trading.OfferStateActive {
			t.Errorf("expected old state %v, got %v", trading.OfferStateActive, event.OldState)
		}

		if event.Offer.State != trading.OfferStateAccepted {
			t.Errorf("expected current state %v, got %v", trading.OfferStateAccepted, event.Offer.State)
		}

	case <-t.Context().Done():
		t.Fatal("timeout waiting for OfferChangedEvent")
	}
}

func TestGetEscrowDuration(t *testing.T) {
	t.Parallel()

	genHTML := func(my, their int) string {
		return fmt.Sprintf("var g_daysMyEscrow = %d; var g_DaysTheirEscrow = %d;", my, their)
	}

	tests := []struct {
		name    string
		offerID uint64
		html    string
		wantErr error
		want    processor.Details
	}{
		{
			name:    "hold_7_days",
			offerID: 100,
			html:    genHTML(0, 7),
			want:    processor.Details{MyDays: 0, TheirDays: 7},
		},
		{
			name:    "no_hold",
			offerID: 200,
			html:    genHTML(0, 0),
			want:    processor.Details{MyDays: 0, TheirDays: 0},
		},
		{
			name:    "parsing_error",
			offerID: 300,
			html:    "<html>No data here</html>",
			wantErr: processor.ErrEscrowNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTestFixture(t)
			path := fmt.Sprintf("tradeoffer/%d/", tt.offerID)
			f.comm.SetHTMLResponse(path, 200, tt.html)

			details, err := f.manager.GetEscrowDuration(t.Context(), tt.offerID)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if details != tt.want {
					t.Errorf("expected details %+v, got %+v", tt.want, details)
				}
			}
		})
	}
}

func TestCheckEscrow_WithHold_ReturnsTrue(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	f.comm.SetHTMLResponse("tradeoffer/123/", 200, "var g_daysMyEscrow = 0; var g_DaysTheirEscrow = 15;")

	off := &trading.TradeOffer{ID: 123}

	hold, err := f.manager.CheckEscrow(t.Context(), off)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hold {
		t.Errorf("expected hold to be true")
	}
}

func TestListenEvents(t *testing.T) {
	t.Parallel()

	t.Run("auth_disconnect", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		sub := f.manager.Bus.Subscribe(&auth.StateEvent{})
		f.manager.Go(func(ctx context.Context) {
			f.manager.listenEvents(ctx, sub)
		})

		err := f.manager.StartPolling()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		f.manager.Bus.Publish(&auth.StateEvent{
			Old: auth.StateLoggedOn,
			New: auth.StateDisconnected,
		})

		waitState(t, f.manager, StateStopped)
	})

	t.Run("other_auth_state", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		sub := f.manager.Bus.Subscribe(&auth.StateEvent{})
		f.manager.Go(func(ctx context.Context) {
			f.manager.listenEvents(ctx, sub)
		})

		err := f.manager.StartPolling()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		f.manager.Bus.Publish(&auth.StateEvent{
			Old: auth.StateDisconnected,
			New: auth.StateLoggedOn,
		})

		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()

		select {
		case <-t.Context().Done():
		case <-timer.C:
		}

		if f.manager.fsm.CurrentState() != StatePolling {
			t.Errorf(
				"expected state to remain %s, got %s",
				StatePolling.String(),
				f.manager.fsm.CurrentState().String(),
			)
		}
	})

	t.Run("sub_closed", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		sub := f.manager.Bus.Subscribe(&auth.StateEvent{})
		f.manager.Go(func(ctx context.Context) {
			f.manager.listenEvents(ctx, sub)
		})

		sub.Unsubscribe()
	})
}

func TestParseTradeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		url       string
		wantID    uint64
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid_url_with_token",
			url:       "https://steamcommunity.com/tradeoffer/new/?partner=12345678&token=xxxxxxxx",
			wantID:    76561197972611406,
			wantToken: "xxxxxxxx",
			wantErr:   false,
		},
		{
			name:      "valid_url_without_token",
			url:       "https://steamcommunity.com/tradeoffer/new/?partner=87654321",
			wantID:    76561198047920049,
			wantToken: "",
			wantErr:   false,
		},
		{
			name:    "missing_partner",
			url:     "https://steamcommunity.com/tradeoffer/new/?token=xxxxxxxx",
			wantErr: true,
		},
		{
			name:    "malformed_url",
			url:     "://invalid-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sid, token, err := ParseTradeURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if sid.Uint64() != tt.wantID {
					t.Errorf("expected partner ID %d, got %d", tt.wantID, sid.Uint64())
				}

				if token != tt.wantToken {
					t.Errorf("expected token %q, got %q", tt.wantToken, token)
				}
			}
		})
	}
}

func TestPollData_GetAndSet_SavesAndRestoresState(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	pd := f.manager.GetPollData()
	if pd.OffersSince != 0 {
		t.Errorf("expected OffersSince to be 0, got %d", pd.OffersSince)
	}

	if len(pd.Sent) != 0 {
		t.Errorf("expected empty sent, got %d", len(pd.Sent))
	}

	if len(pd.Received) != 0 {
		t.Errorf("expected empty received, got %d", len(pd.Received))
	}

	customData := trading.PollData{
		OffersSince: 1700000000,
		Sent: map[uint64]trading.OfferState{
			111: trading.OfferStateActive,
		},
		Received: map[uint64]trading.OfferState{
			222: trading.OfferStateAccepted,
		},
	}
	f.manager.SetPollData(customData)

	pd = f.manager.GetPollData()
	if pd.OffersSince != 1700000000 {
		t.Errorf("expected OffersSince 1700000000, got %d", pd.OffersSince)
	}

	if pd.Sent[111] != trading.OfferStateActive {
		t.Errorf("expected Sent[111] %v, got %v", trading.OfferStateActive, pd.Sent[111])
	}

	if pd.Received[222] != trading.OfferStateAccepted {
		t.Errorf("expected Received[222] %v, got %v", trading.OfferStateAccepted, pd.Received[222])
	}
}

func TestPoll_StateChanges_EmitsPollDataEvent(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	sub := f.manager.Bus.Subscribe(&PollDataEvent{})
	t.Cleanup(sub.Unsubscribe)

	f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
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

	f.manager.doPoll(t.Context())

	select {
	case ev := <-sub.C():
		eventData := ev.(*PollDataEvent).PollData
		if eventData.OffersSince != 1710000000 {
			t.Errorf("expected OffersSince 1710000000, got %d", eventData.OffersSince)
		}

		if eventData.Received[123] != trading.OfferStateActive {
			t.Errorf("expected Received[123] %v, got %v", trading.OfferStateActive, eventData.Received[123])
		}

	case <-t.Context().Done():
		t.Fatal("timeout waiting for PollDataEvent")
	}
}

func TestPoll_ExpiredSentOffers(t *testing.T) {
	t.Parallel()

	t.Run("cancels_successful", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.manager.config.CancelTime = 1 * time.Hour

		f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
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

		cancelChan := make(chan uint64, 1)

		f.web.OnDo = func(req *tr.Request) (*tr.Response, error) {
			target := req.Target()
			if webTarget, ok := target.(*service.WebAPITarget); ok && webTarget.Interface == "IEconService" &&
				webTarget.Method == "CancelTradeOffer" {
				idStr := req.Params().Get("tradeofferid")

				id, _ := strconv.ParseUint(idStr, 10, 64)
				cancelChan <- id

				return tr.NewResponse(
					io.NopCloser(bytes.NewReader([]byte("{}"))),
					tr.HTTPMetadata{StatusCode: 200},
				), nil
			}

			return nil, nil
		}

		f.manager.doPoll(t.Context())

		select {
		case cid := <-cancelChan:
			if cid != 999 {
				t.Errorf("expected cancelled offer ID 999, got %d", cid)
			}
		case <-t.Context().Done():
			t.Fatal("timeout waiting for CancelTradeOffer call")
		}
	})

	t.Run("cancels_fails_logs_error", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.manager.config.CancelTime = 1 * time.Hour

		f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
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

		cancelChan := make(chan struct{})

		f.web.OnDo = func(req *tr.Request) (*tr.Response, error) {
			target := req.Target()
			if webTarget, ok := target.(*service.WebAPITarget); ok && webTarget.Interface == "IEconService" &&
				webTarget.Method == "CancelTradeOffer" {
				close(cancelChan)
				return nil, errors.New("api error on cancellation")
			}

			return nil, nil
		}

		f.manager.doPoll(t.Context())

		select {
		case <-cancelChan:
		case <-t.Context().Done():
			t.Fatal("timeout waiting for failed CancelTradeOffer call")
		}
	})
}

func TestPoll_SentOffersExceedLimit(t *testing.T) {
	t.Parallel()

	t.Run("cancels_successful", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.manager.config.CancelOfferCount = 2
		f.manager.config.CancelOfferCountMinAge = 5 * time.Minute

		f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
			"response": map[string]any{
				"trade_offers_sent": []any{
					map[string]any{
						"tradeofferid":      "1001",
						"trade_offer_state": int(trading.OfferStateActive),
						"is_our_offer":      true,
						"time_updated":      time.Now().Add(-10 * time.Minute).Unix(),
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

		cancelChan := make(chan uint64, 1)

		f.web.OnDo = func(req *tr.Request) (*tr.Response, error) {
			target := req.Target()
			if webTarget, ok := target.(*service.WebAPITarget); ok && webTarget.Interface == "IEconService" &&
				webTarget.Method == "CancelTradeOffer" {
				idStr := req.Params().Get("tradeofferid")

				id, _ := strconv.ParseUint(idStr, 10, 64)
				cancelChan <- id

				return tr.NewResponse(
					io.NopCloser(bytes.NewReader([]byte("{}"))),
					tr.HTTPMetadata{StatusCode: 200},
				), nil
			}

			return nil, nil
		}

		f.manager.doPoll(t.Context())

		select {
		case cid := <-cancelChan:
			if cid != 1001 {
				t.Errorf("expected cancelled offer ID 1001, got %d", cid)
			}
		case <-t.Context().Done():
			t.Fatal("timeout waiting for oldest offer cancellation")
		}
	})

	t.Run("cancels_fails_logs_error", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.manager.config.CancelOfferCount = 2
		f.manager.config.CancelOfferCountMinAge = 5 * time.Minute

		f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
			"response": map[string]any{
				"trade_offers_sent": []any{
					map[string]any{
						"tradeofferid":      "1001",
						"trade_offer_state": int(trading.OfferStateActive),
						"is_our_offer":      true,
						"time_updated":      time.Now().Add(-10 * time.Minute).Unix(),
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

		cancelChan := make(chan struct{})

		f.web.OnDo = func(req *tr.Request) (*tr.Response, error) {
			target := req.Target()
			if webTarget, ok := target.(*service.WebAPITarget); ok && webTarget.Interface == "IEconService" &&
				webTarget.Method == "CancelTradeOffer" {
				close(cancelChan)
				return nil, errors.New("api error on limit cancellation")
			}

			return nil, nil
		}

		f.manager.doPoll(t.Context())

		select {
		case <-cancelChan:
		case <-t.Context().Done():
			t.Fatal("timeout waiting for failed limit CancelTradeOffer call")
		}
	})
}

func TestGCKnownOffers_StaleOffers_RemovesFromMemory(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	m := f.manager

	now := time.Now()

	m.sentOffers[101] = trading.OfferStateActive
	m.lastSeenOffers[101] = now.Add(-2 * time.Hour)

	m.sentOffers[102] = trading.OfferStateAccepted
	m.lastSeenOffers[102] = now.Add(-2 * time.Hour)

	m.receivedOffers[103] = trading.OfferStateAccepted
	m.lastSeenOffers[103] = now.Add(-30 * time.Minute)

	m.receivedOffers[104] = trading.OfferStateActive
	m.lastSeenOffers[104] = now.Add(-2 * time.Hour)

	m.gcKnownOffers(now)

	if _, ok := m.sentOffers[101]; !ok {
		t.Errorf("expected active sent offer 101 to be kept")
	}

	if _, ok := m.lastSeenOffers[101]; !ok {
		t.Errorf("expected lastSeenOffers for active offer 101 to be kept")
	}

	if _, ok := m.sentOffers[102]; ok {
		t.Errorf("expected stale accepted sent offer 102 to be garbage collected")
	}

	if _, ok := m.lastSeenOffers[102]; ok {
		t.Errorf("expected lastSeenOffers for stale offer 102 to be garbage collected")
	}

	if _, ok := m.receivedOffers[103]; !ok {
		t.Errorf("expected recent received offer 103 to be kept")
	}

	if _, ok := m.lastSeenOffers[103]; !ok {
		t.Errorf("expected lastSeenOffers for recent offer 103 to be kept")
	}

	if _, ok := m.receivedOffers[104]; !ok {
		t.Errorf("expected active received offer 104 to be kept")
	}
}

func TestGetExchangeDetails(t *testing.T) {
	t.Parallel()

	t.Run("completed_trade", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.web.SetJSONResponse("IEconService", "GetTradeStatus", map[string]any{
			"response": map[string]any{
				"trades": []any{
					map[string]any{
						"tradeid":       "88888",
						"steamid_other": "76561198083721406",
						"time_init":     1710000000,
						"status":        3,
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

		details, err := f.manager.GetExchangeDetails(t.Context(), 88888)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if details.Status != 3 {
			t.Errorf("expected status 3, got %d", details.Status)
		}

		if details.TimeInit != 1710000000 {
			t.Errorf("expected time_init 1710000000, got %d", details.TimeInit)
		}

		if len(details.AssetsReceived) != 1 {
			t.Errorf("expected 1 asset received, got %d", len(details.AssetsReceived))
		} else if details.AssetsReceived[0].NewAssetID != 222 {
			t.Errorf("expected new_assetid 222, got %d", details.AssetsReceived[0].NewAssetID)
		}

		if len(details.AssetsGiven) != 1 {
			t.Errorf("expected 1 asset given, got %d", len(details.AssetsGiven))
		} else if details.AssetsGiven[0].NewAssetID != 444 {
			t.Errorf("expected new_assetid 444, got %d", details.AssetsGiven[0].NewAssetID)
		}
	})

	t.Run("empty_trades_response", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.web.SetJSONResponse("IEconService", "GetTradeStatus", map[string]any{
			"response": map[string]any{
				"trades": []any{},
			},
		})

		_, err := f.manager.GetExchangeDetails(t.Context(), 111)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected error containing 'not found', got: %v", err)
		}
	})

	t.Run("web_api_failure", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.web.SetErrorResponse("IEconService", "GetTradeStatus", errors.New("api down"))

		_, err := f.manager.GetExchangeDetails(t.Context(), 111)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestSendOffer(t *testing.T) {
	t.Parallel()

	t.Run("valid_params", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.comm.SetHTMLResponse("tradeoffer/new/send", 200, `{"tradeofferid":"999999","needs_mobile_confirmation":true}`)

		params := trading.OfferParams{
			PartnerID: id.FromAccountID(123456),
			Token:     "token_abc",
			Message:   "hello",
			ItemsToGive: []*trading.Item{
				{AppID: 440, ContextID: 2, AssetID: 111, Amount: 1},
			},
		}

		sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})

		offerID, err := f.manager.SendOffer(t.Context(), params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if offerID != 999999 {
			t.Errorf("expected offer ID 999999, got %d", offerID)
		}

		select {
		case ev := <-sub.C():
			event := ev.(*guard.ConfirmationRequiredEvent)
			if event.TradeOfferID != "999999" {
				t.Errorf("expected trade offer ID '999999', got %q", event.TradeOfferID)
			}

			if !event.IsAppConfirm {
				t.Errorf("expected IsAppConfirm to be true")
			}

		case <-t.Context().Done():
			t.Fatal("timeout waiting for ConfirmationRequiredEvent")
		}
	})

	t.Run("nil_community", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.manager.community = nil

		params := trading.OfferParams{
			PartnerID: id.FromAccountID(123456),
		}

		_, err := f.manager.SendOffer(t.Context(), params)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "community client not authenticated") {
			t.Errorf("expected community error, got: %v", err)
		}
	})

	t.Run("post_form_error", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.comm.SetHTMLResponse("tradeoffer/new/send", 500, "Internal Server Error")

		params := trading.OfferParams{
			PartnerID: id.FromAccountID(123456),
		}

		_, err := f.manager.SendOffer(t.Context(), params)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestAcceptOffer(t *testing.T) {
	t.Parallel()

	t.Run("valid_id", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.comm.SetHTMLResponse(
			"tradeoffer/12345/accept",
			200,
			`{"needs_mobile_confirmation":true,"email_domain":"gmail.com"}`,
		)

		sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})

		err := f.manager.AcceptOffer(t.Context(), 12345)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		select {
		case ev := <-sub.C():
			event := ev.(*guard.ConfirmationRequiredEvent)
			if event.TradeOfferID != "12345" {
				t.Errorf("expected TradeOfferID '12345', got %q", event.TradeOfferID)
			}

			if !event.IsAppConfirm {
				t.Errorf("expected IsAppConfirm to be true")
			}

			if event.EmailDomain != "gmail.com" {
				t.Errorf("expected EmailDomain 'gmail.com', got %q", event.EmailDomain)
			}

		case <-t.Context().Done():
			t.Fatal("timeout waiting for ConfirmationRequiredEvent")
		}
	})

	t.Run("nil_community", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.manager.community = nil

		err := f.manager.AcceptOffer(t.Context(), TestOfferID)
		if !errors.Is(err, ErrCommunityNotReady) {
			t.Errorf("expected ErrCommunityNotReady, got %v", err)
		}
	})
}

func TestDeclineOffer_ValidID_DeclinesSuccessfully(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	f.web.SetJSONResponse("IEconService", "DeclineTradeOffer", map[string]any{
		"response": map[string]any{},
	})

	err := f.manager.DeclineOffer(t.Context(), 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCancelOffer_ValidID_CancelsSuccessfully(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	f.web.SetJSONResponse("IEconService", "CancelTradeOffer", map[string]any{
		"response": map[string]any{},
	})

	err := f.manager.CancelOffer(t.Context(), 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetOffer(t *testing.T) {
	t.Parallel()

	t.Run("valid_id", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": map[string]any{
					"tradeofferid": "888",
					"items_to_give": []any{
						map[string]any{
							"appid":      440,
							"contextid":  "2",
							"assetid":    "111",
							"classid":    "222",
							"instanceid": "333",
							"amount":     "1",
						},
					},
				},
				"descriptions": []any{
					map[string]any{
						"appid":            440,
						"classid":          "222",
						"instanceid":       "333",
						"name":             "Mann Co. Supply Crate Key",
						"market_hash_name": "Mann Co. Supply Crate Key",
						"tradable":         1,
					},
				},
			},
		})

		offer, err := f.manager.GetOffer(t.Context(), 888)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if offer.ID != 888 {
			t.Errorf("expected offer ID 888, got %d", offer.ID)
		}

		if len(offer.ItemsToGive) != 1 {
			t.Fatalf("expected 1 item, got %d", len(offer.ItemsToGive))
		}

		if offer.ItemsToGive[0].Name != "Mann Co. Supply Crate Key" {
			t.Errorf("expected Name 'Mann Co. Supply Crate Key', got %q", offer.ItemsToGive[0].Name)
		}

		if !offer.ItemsToGive[0].Tradable {
			t.Errorf("expected Tradable to be true")
		}
	})

	t.Run("offer_not_found", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": nil,
			},
		})

		_, err := f.manager.GetOffer(t.Context(), 888)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected error containing 'not found', got: %v", err)
		}
	})

	t.Run("web_api_failure", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.web.SetErrorResponse("IEconService", "GetTradeOffer", errors.New("api down"))

		_, err := f.manager.GetOffer(t.Context(), 888)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestGetPartnerInventory(t *testing.T) {
	t.Parallel()

	t.Run("valid_partner", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		partnerID := id.ID(76561198000000001)

		respJSON := `{
			"success": 1,
			"total_inventory_count": 1,
			"assets": [
				{"appid":440,"contextid":"2","assetid":"111","id":"111","classid":"222","instanceid":"333","amount":"1"}
			],
			"descriptions": [
				{"appid":440,"classid":"222","instanceid":"333","name":"Mann Co. Supply Crate Key","market_hash_name":"Mann Co. Supply Crate Key","tradable":1}
			],
			"rgInventory": {
				"111": {
					"id": "111",
					"classid": "222",
					"instanceid": "333",
					"amount": "1"
				}
			},
			"rgDescriptions": {
				"222_333": {
					"appid": 440,
					"classid": "222",
					"instanceid": "333",
					"name": "Mann Co. Supply Crate Key",
					"market_hash_name": "Mann Co. Supply Crate Key",
					"tradable": 1
				}
			}
		}`

		registerInventoryResponse(f.comm, partnerID, respJSON)

		items, err := f.manager.GetPartnerInventory(t.Context(), partnerID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}

		if items[0].AssetID != 111 {
			t.Errorf("expected AssetID 111, got %d", items[0].AssetID)
		}

		if items[0].Name != "Mann Co. Supply Crate Key" {
			t.Errorf("expected Name 'Mann Co. Supply Crate Key', got %q", items[0].Name)
		}
	})

	t.Run("nil_community", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.manager.community = nil

		_, err := f.manager.GetPartnerInventory(t.Context(), id.ID(OtherAccountID))
		if !errors.Is(err, ErrCommunityNotReady) {
			t.Errorf("expected ErrCommunityNotReady, got %v", err)
		}
	})

	t.Run("malformed_inventory_asset_id", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		partnerID := id.ID(76561198000000001)

		respJSON := `{
			"success": 1,
			"assets": [
				{"appid":440,"contextid":"2","assetid":"not_a_uint","id":"not_a_uint","classid":"222","instanceid":"333","amount":"1"}
			]
		}`

		registerInventoryResponse(f.comm, partnerID, respJSON)

		items, err := f.manager.GetPartnerInventory(t.Context(), partnerID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(items) != 0 {
			t.Errorf("expected empty items list, got %d items", len(items))
		}
	})
}

func registerInventoryResponse(comm *mock.HTTPStub, partnerID id.ID, respJSON string) {
	basePaths := []string{
		fmt.Sprintf("profiles/%d/inventory/json/440/2", partnerID),
		fmt.Sprintf("profiles/%d/inventory/json/440/2/", partnerID),
		fmt.Sprintf("inventory/%d/440/2", partnerID),
		fmt.Sprintf("inventory/%d/440/2/", partnerID),
	}

	querySuffixes := []string{
		"",
		"/",
		"?l=english",
		"?trading=1",
		"?trading=true",
		"?trading=1&l=english",
		"?trading=true&l=english",
		"?l=english&trading=1",
		"?l=english&trading=true",
		"?l=english&trading=1&count=5000",
		"?l=english&trading=true&count=5000",
		"?trading=1&l=english&count=5000",
		"?trading=true&l=english&count=5000",
		"?count=5000&l=english&trading=1",
		"?count=5000&l=english&trading=true",
		"?count=5000&trading=1&l=english",
		"?count=5000&trading=true&l=english",
		"?count=5000&trading=1",
		"?count=5000&trading=true",
		"?trading=1&count=5000",
		"?trading=true&count=5000",
		"?count=5000",
		"?count=2000",
		"?trading=1&count=2000",
		"?l=english&count=5000",
		"?l=english&count=2000",
	}
	for _, basePath := range basePaths {
		for _, q := range querySuffixes {
			comm.SetHTMLResponse(basePath+q, 200, respJSON)
			comm.SetHTMLResponse("/"+basePath+q, 200, respJSON)
			comm.SetHTMLResponse("https://steamcommunity.com/"+basePath+q, 200, respJSON)
			comm.SetHTMLResponse("http://steamcommunity.com/"+basePath+q, 200, respJSON)
		}
	}
}

func TestEnrichItemsDescriptions(t *testing.T) {
	t.Parallel()

	t.Run("chunking_over_fifty", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.web.SetJSONResponse("ISteamEconomy", "GetAssetClassInfo", map[string]any{
			"result": map[string]any{
				"success": true,
				"100": map[string]any{
					"classid":          "100",
					"instanceid":       "0",
					"name":             "Chunked Item",
					"market_name":      "Chunked Item",
					"market_hash_name": "Chunked Item",
					"tradable":         1,
				},
			},
		})

		items := make([]*trading.Item, 55)
		for i := range 55 {
			items[i] = &trading.Item{
				AppID:      440,
				ContextID:  2,
				ClassID:    100,
				InstanceID: 0,
			}
		}

		err := f.manager.enrichItemsDescriptions(t.Context(), items)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if items[0].Name != "Chunked Item" {
			t.Errorf("expected first item name to be 'Chunked Item', got %q", items[0].Name)
		}

		if items[54].Name != "Chunked Item" {
			t.Errorf("expected last item name to be 'Chunked Item', got %q", items[54].Name)
		}
	})

	t.Run("get_asset_class_info_failure", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.web.SetErrorResponse("ISteamEconomy", "GetAssetClassInfo", errors.New("api down"))

		items := []*trading.Item{
			{AppID: 440, ContextID: 2, ClassID: 222, InstanceID: 333},
		}

		err := f.manager.enrichItemsDescriptions(t.Context(), items)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("malformed_json_asset_value", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.web.SetJSONResponse("ISteamEconomy", "GetAssetClassInfo", map[string]any{
			"result": map[string]any{
				"success": true,
				"100":     "not_a_valid_json_object",
			},
		})

		items := []*trading.Item{
			{AppID: 440, ContextID: 2, ClassID: 100, InstanceID: 0},
		}

		err := f.manager.enrichItemsDescriptions(t.Context(), items)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if items[0].MarketHashName != "" {
			t.Errorf("expected empty MarketHashName, got %q", items[0].MarketHashName)
		}
	})
}

func TestEnrichItemsDescriptions_DuplicateSeenKeys(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	f.web.SetJSONResponse("ISteamEconomy", "GetAssetClassInfo", map[string]any{
		"result": map[string]any{
			"success": true,
			"100": map[string]any{
				"classid":          "100",
				"instanceid":       "0",
				"name":             "Duplicate Key Item",
				"market_name":      "Duplicate Key Item",
				"market_hash_name": "Duplicate Key Item",
				"tradable":         1,
			},
		},
	})

	items := []*trading.Item{
		{AppID: 440, ContextID: 2, ClassID: 100, InstanceID: 0},
		{AppID: 440, ContextID: 2, ClassID: 100, InstanceID: 0},
	}

	err := f.manager.enrichItemsDescriptions(t.Context(), items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if items[0].Name != "Duplicate Key Item" {
		t.Errorf("expected first item name to be 'Duplicate Key Item', got %q", items[0].Name)
	}

	if items[1].Name != "Duplicate Key Item" {
		t.Errorf("expected second item name to be 'Duplicate Key Item', got %q", items[1].Name)
	}
}

func TestEnrichItemsDescriptions_WithTags(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	f.web.SetJSONResponse("ISteamEconomy", "GetAssetClassInfo", map[string]any{
		"result": map[string]any{
			"success": true,
			"100": map[string]any{
				"classid":          "100",
				"instanceid":       "0",
				"name":             "Tagged Item",
				"market_name":      "Tagged Item",
				"market_hash_name": "Tagged Item",
				"tradable":         1,
				"tags": []map[string]any{
					{
						"category":                "Quality",
						"internal_name":           "Unique",
						"localized_category_name": "Quality",
						"localized_tag_name":      "Unique",
					},
					{
						"category":                "Type",
						"internal_name":           "Cosmetic",
						"localized_category_name": "Type",
						"name":                    "Cosmetic",
					},
				},
			},
		},
	})

	items := []*trading.Item{
		{AppID: 440, ContextID: 2, ClassID: 100, InstanceID: 0},
	}

	err := f.manager.enrichItemsDescriptions(t.Context(), items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items[0].Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(items[0].Tags))
	}

	if items[0].Tags[0].LocalizedName != "Unique" {
		t.Errorf("expected tag 0 localized name 'Unique', got %q", items[0].Tags[0].LocalizedName)
	}

	if items[0].Tags[1].LocalizedName != "Cosmetic" {
		t.Errorf("expected tag 1 localized name 'Cosmetic', got %q", items[0].Tags[1].LocalizedName)
	}
}

func TestTriggerPoll_NotificationEvent_TriggersImmediatePoll(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	err := f.manager.StartPolling()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(f.manager.StopPolling)

	notifSub := f.manager.Bus.Subscribe(&notifications.UserNotificationsEvent{})
	f.manager.Go(func(ctx context.Context) {
		f.manager.listenNotifications(ctx, notifSub)
	})

	pollChan := make(chan struct{}, 1)

	f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
		"response": map[string]any{},
	})

	f.web.OnDo = func(req *tr.Request) (*tr.Response, error) {
		target := req.Target()
		if webTarget, ok := target.(*service.WebAPITarget); ok && webTarget.Interface == "IEconService" &&
			webTarget.Method == "GetTradeOffers" {
			select {
			case pollChan <- struct{}{}:
			default:
			}
		}

		return nil, nil
	}

	f.manager.Bus.Publish(&notifications.UserNotificationsEvent{
		Notifications: map[notifications.NotificationType]uint32{
			notifications.NotificationTradeOffer: 1,
		},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	select {
	case <-pollChan:
	case <-ctx.Done():
		t.Fatal("timeout waiting for immediate poll trigger")
	}
}

func TestListenNotifications(t *testing.T) {
	t.Parallel()

	t.Run("no_trade_offer_notification", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		sub := f.manager.Bus.Subscribe(&notifications.UserNotificationsEvent{})
		f.manager.Go(func(ctx context.Context) {
			f.manager.listenNotifications(ctx, sub)
		})

		err := f.manager.StartPolling()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		t.Cleanup(f.manager.StopPolling)

		f.manager.Bus.Publish(&notifications.UserNotificationsEvent{
			Notifications: map[notifications.NotificationType]uint32{
				notifications.NotificationTradeOffer: 0,
			},
		})

		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()

		select {
		case <-t.Context().Done():
		case <-timer.C:
		}
	})

	t.Run("other_notification_types", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		sub := f.manager.Bus.Subscribe(&notifications.UserNotificationsEvent{})
		f.manager.Go(func(ctx context.Context) {
			f.manager.listenNotifications(ctx, sub)
		})

		err := f.manager.StartPolling()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		t.Cleanup(f.manager.StopPolling)

		f.manager.Bus.Publish(&notifications.UserNotificationsEvent{
			Notifications: map[notifications.NotificationType]uint32{
				notifications.NotificationType(999): 5,
			},
		})

		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()

		select {
		case <-t.Context().Done():
		case <-timer.C:
		}
	})

	t.Run("channel_closed", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		sub := f.manager.Bus.Subscribe(&notifications.UserNotificationsEvent{})
		f.manager.Go(func(ctx context.Context) {
			f.manager.listenNotifications(ctx, sub)
		})

		sub.Unsubscribe()
	})
}

func TestUnmarshalFlexibleArray(t *testing.T) {
	t.Parallel()

	type Dummy struct {
		Value string `json:"value"`
	}

	t.Run("empty_string", func(t *testing.T) {
		t.Parallel()

		res, err := unmarshalFlexibleArray[Dummy]([]byte(`""`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if res != nil {
			t.Errorf("expected nil result, got %v", res)
		}
	})

	t.Run("regular_array", func(t *testing.T) {
		t.Parallel()

		res, err := unmarshalFlexibleArray[Dummy]([]byte(`[{"value":"first"},{"value":"second"}]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(res) != 2 {
			t.Fatalf("expected 2 elements, got %d", len(res))
		}

		if res[0].Value != "first" {
			t.Errorf("expected 'first', got %q", res[0].Value)
		}

		if res[1].Value != "second" {
			t.Errorf("expected 'second', got %q", res[1].Value)
		}
	})

	t.Run("indexed_object", func(t *testing.T) {
		t.Parallel()

		res, err := unmarshalFlexibleArray[Dummy]([]byte(`{"1":{"value":"second"},"0":{"value":"first"}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(res) != 2 {
			t.Fatalf("expected 2 elements, got %d", len(res))
		}

		if res[0].Value != "first" {
			t.Errorf("expected 'first', got %q", res[0].Value)
		}

		if res[1].Value != "second" {
			t.Errorf("expected 'second', got %q", res[1].Value)
		}
	})

	t.Run("indexed_object_non_integer_key", func(t *testing.T) {
		t.Parallel()

		res, err := unmarshalFlexibleArray[Dummy]([]byte(`{"invalid_key":{"value":"ignored"},"0":{"value":"first"}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(res) != 1 {
			t.Fatalf("expected 1 element, got %d", len(res))
		}

		if res[0].Value != "first" {
			t.Errorf("expected 'first', got %q", res[0].Value)
		}
	})

	t.Run("indexed_object_invalid_json", func(t *testing.T) {
		t.Parallel()

		_, err := unmarshalFlexibleArray[Dummy]([]byte(`{"0": "invalid_type_mismatch"}`))
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		t.Parallel()

		_, err := unmarshalFlexibleArray[Dummy]([]byte(`{invalid`))
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("unsupported_type", func(t *testing.T) {
		t.Parallel()

		res, err := unmarshalFlexibleArray[any]([]byte("123"))
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		if res != nil {
			t.Errorf("expected nil result, got %v", res)
		}
	})
}

func TestFlexibleDescriptions_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("valid_array", func(t *testing.T) {
		t.Parallel()

		var fd flexibleDescriptions

		err := fd.UnmarshalJSON([]byte(`[{"value":"test"}]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(fd) != 1 {
			t.Fatalf("expected 1 element, got %d", len(fd))
		}

		if fd[0].Value != "test" {
			t.Errorf("expected 'test', got %q", fd[0].Value)
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		t.Parallel()

		var fd flexibleDescriptions

		err := fd.UnmarshalJSON([]byte(`{invalid`))
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestFlexibleTags_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("valid_indexed_object", func(t *testing.T) {
		t.Parallel()

		var ft flexibleTags

		err := ft.UnmarshalJSON([]byte(`{"0":{"name":"tag_name","category":"cat"}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(ft) != 1 {
			t.Fatalf("expected 1 element, got %d", len(ft))
		}

		if ft[0].Name != "tag_name" {
			t.Errorf("expected 'tag_name', got %q", ft[0].Name)
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		t.Parallel()

		var ft flexibleTags

		err := ft.UnmarshalJSON([]byte(`{invalid`))
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestGetActiveSentOffers_ActiveSentOffers_ReturnsSuccessfully(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
		"response": map[string]any{
			"trade_offers_sent": []any{
				map[string]any{
					"tradeofferid":      "999",
					"trade_offer_state": int(trading.OfferStateActive),
					"is_our_offer":      true,
					"time_updated":      1710000000,
				},
			},
		},
	})

	offers, err := f.manager.GetActiveSentOffers(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(offers) != 1 {
		t.Fatalf("expected 1 offer, got %d", len(offers))
	}

	if offers[0].ID != 999 {
		t.Errorf("expected offer ID 999, got %d", offers[0].ID)
	}

	if !offers[0].IsOurOffer {
		t.Errorf("expected IsOurOffer to be true")
	}
}

func TestSetOfferHandler_ValidHandler_StartsProcessor(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	type mockOfferHandler struct{ processor.OfferHandler }

	type mockBackpack struct{ processor.BackpackProvider }

	err := f.manager.Start(t.Context())
	if err != nil {
		t.Fatalf("unexpected error starting manager: %v", err)
	}

	f.manager.SetOfferHandler(t.Context(), &mockOfferHandler{}, &mockBackpack{})

	f.manager.mu.RLock()
	proc := f.manager.processor
	f.manager.mu.RUnlock()

	if proc == nil {
		t.Errorf("expected non-nil processor")
	}
}

func TestTriggerPoll_DuplicateCalls(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)

	f.manager.TriggerPoll()
	f.manager.TriggerPoll()
}

func TestManagerGetters(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	if f.manager.Web() != f.web {
		t.Errorf("Web getter returned incorrect service")
	}

	if f.manager.Community() != f.comm {
		t.Errorf("Community getter returned incorrect service")
	}
}

func TestDoPoll_RateLimiterFailure(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	f.manager.rateLimiter = rate.NewLimiter(rate.Limit(0), 0)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	f.manager.doPoll(ctx)
}

func TestDoPoll_WebAPIFailure_CancelledContext(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	f.web.SetErrorResponse("IEconService", "GetTradeOffers", errors.New("some error"))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	f.manager.doPoll(ctx)
}

func TestDoPoll_WithNilOffers(t *testing.T) {
	t.Parallel()

	f := newTestFixture(t)
	f.web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
		"response": map[string]any{
			"trade_offers_sent": []any{
				map[string]any{
					"tradeofferid":      "1001",
					"trade_offer_state": int(trading.OfferStateActive),
					"is_our_offer":      true,
					"time_updated":      time.Now().Add(-1 * time.Minute).Unix(),
				},
			},
			"trade_offers_received": []any{
				nil,
			},
		},
	})

	f.manager.doPoll(t.Context())
}

func TestIsPollDataEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    trading.PollData
		b    trading.PollData
		want bool
	}{
		{
			name: "equal",
			a: trading.PollData{
				OffersSince: 100,
				Sent:        map[uint64]trading.OfferState{1: trading.OfferStateActive},
				Received:    map[uint64]trading.OfferState{2: trading.OfferStateAccepted},
			},
			b: trading.PollData{
				OffersSince: 100,
				Sent:        map[uint64]trading.OfferState{1: trading.OfferStateActive},
				Received:    map[uint64]trading.OfferState{2: trading.OfferStateAccepted},
			},
			want: true,
		},
		{
			name: "diff_offers_since",
			a:    trading.PollData{OffersSince: 100},
			b:    trading.PollData{OffersSince: 200},
			want: false,
		},
		{
			name: "diff_sent_len",
			a: trading.PollData{
				Sent: map[uint64]trading.OfferState{1: trading.OfferStateActive},
			},
			b: trading.PollData{
				Sent: map[uint64]trading.OfferState{},
			},
			want: false,
		},
		{
			name: "diff_received_len",
			a: trading.PollData{
				Received: map[uint64]trading.OfferState{1: trading.OfferStateActive},
			},
			b: trading.PollData{
				Received: map[uint64]trading.OfferState{},
			},
			want: false,
		},
		{
			name: "diff_sent_value",
			a: trading.PollData{
				Sent: map[uint64]trading.OfferState{1: trading.OfferStateActive},
			},
			b: trading.PollData{
				Sent: map[uint64]trading.OfferState{1: trading.OfferStateAccepted},
			},
			want: false,
		},
		{
			name: "diff_received_value",
			a: trading.PollData{
				Received: map[uint64]trading.OfferState{1: trading.OfferStateActive},
			},
			b: trading.PollData{
				Received: map[uint64]trading.OfferState{1: trading.OfferStateAccepted},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := isPollDataEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("expected isPollDataEqual(...) = %v, got %v", tt.want, got)
			}
		})
	}
}
