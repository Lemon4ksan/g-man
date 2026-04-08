// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/modules/auth"
	"github.com/lemon4ksan/g-man/pkg/modules/econ"
	"github.com/lemon4ksan/g-man/test"
)

const (
	TestOfferID    = 12345
	OtherAccountID = 45678
)

func setupTrading(t *testing.T) (*Manager, *test.MockRequester, *test.MockCommunityRequester) {
	t.Helper()
	web := test.NewMockRequester()
	comm := test.NewMockCommunityRequester()

	init := test.NewMockInitContext()
	init.SetService(web)

	m := New(DefaultConfig())

	if err := m.Init(init); err != nil {
		t.Fatalf("failed to init module: %v", err)
	}

	m.community = comm

	return m, web, comm
}

func TestManager_Lifecycle(t *testing.T) {
	m, _, _ := setupTrading(t)

	t.Run("State Transitions", func(t *testing.T) {
		if m.State.Load() != StateStopped {
			t.Errorf("expected Stopped, got %d", m.State.Load())
		}

		if err := m.StartPolling(); err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		if m.State.Load() != StatePolling {
			t.Errorf("expected Polling, got %d", m.State.Load())
		}

		if err := m.StartPolling(); !errors.Is(err, ErrManagerPolling) {
			t.Errorf("expected ErrManagerPolling, got %v", err)
		}

		m.StopPolling()
		if m.State.Load() != StateStopped {
			t.Errorf("expected Stopped after stop, got %d", m.State.Load())
		}

		_ = m.Close()
		if m.State.Load() != StateClosed {
			t.Errorf("expected Closed, got %d", m.State.Load())
		}
	})
}

func TestManager_PollingLogic(t *testing.T) {
	m, web, _ := setupTrading(t)

	ctx := m.Ctx

	subNew := m.Bus.Subscribe(&NewOfferEvent{})
	subChanged := m.Bus.Subscribe(&OfferChangedEvent{})

	t.Run("Detect New Offer", func(t *testing.T) {
		web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
			"response": map[string]any{
				"trade_offers_received": []any{
					map[string]any{
						"tradeofferid":      fmt.Sprint(TestOfferID),
						"trade_offer_state": int(econ.TradeOfferStateActive),
						"accountid_other":   OtherAccountID,
					},
				},
			},
		})

		m.doPoll(ctx)

		select {
		case ev := <-subNew.C():
			if ev.(*NewOfferEvent).Offer.ID != TestOfferID {
				t.Error("wrong offer ID in NewOfferEvent")
			}
		case <-time.After(1 * time.Second):
			t.Fatal("NewOfferEvent not received")
		}
	})

	t.Run("Detect State Change", func(t *testing.T) {
		web.SetJSONResponse("IEconService", "GetTradeOffers", map[string]any{
			"response": map[string]any{
				"trade_offers_received": []any{
					map[string]any{
						"tradeofferid":      fmt.Sprint(TestOfferID),
						"trade_offer_state": int(econ.TradeOfferStateAccepted),
					},
				},
			},
		})

		m.doPoll(ctx)

		select {
		case ev := <-subChanged.C():
			evC := ev.(*OfferChangedEvent)
			if evC.OldState != econ.TradeOfferStateActive || evC.Offer.State != econ.TradeOfferStateAccepted {
				t.Errorf("unexpected state transition: %v -> %v", evC.OldState, evC.Offer.State)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("OfferChangedEvent not received")
		}
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
		want    EscrowDetails
		wantErr error
	}{
		{
			name:    "Hold 7 days",
			offerID: 100,
			html:    genHTML(0, 7),
			want:    EscrowDetails{MyDays: 0, TheirDays: 7},
		},
		{
			name:    "No hold",
			offerID: 200,
			html:    genHTML(0, 0),
			want:    EscrowDetails{MyDays: 0, TheirDays: 0},
		},
		{
			name:    "Parsing error",
			offerID: 300,
			html:    "<html>No data here</html>",
			wantErr: ErrEscrowNotFound,
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

			if err != nil && tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("got error %v, want %v", err, tt.wantErr)
			}
			if err == nil && tt.wantErr != nil {
				t.Fatal("expected error, got nil")
			}
			if err == nil && !reflect.DeepEqual(details, tt.want) {
				t.Errorf("got details %+v, want %+v", details, tt.want)
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

	if m.State.Load() != StateStopped {
		t.Errorf("expected state Stopped after Disconnect event, got %d", m.State.Load())
	}
}
