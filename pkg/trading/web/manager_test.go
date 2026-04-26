// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	bm "github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/tf2/backpack"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/processor"
	"github.com/lemon4ksan/g-man/test/community"
	"github.com/lemon4ksan/g-man/test/module"
	"github.com/lemon4ksan/g-man/test/requester"
)

const (
	TestOfferID    = 12345
	OtherAccountID = 45678
)

type backpackMock struct {
	bm.Base

	mu          sync.Mutex
	lockedItems map[uint64]bool
	lockCalls   int
	unlockCalls int
}

func newBackpackMock() *backpackMock {
	return &backpackMock{lockedItems: make(map[uint64]bool)}
}

func (m *backpackMock) LockItems(ids []uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lockCalls++
	for _, id := range ids {
		m.lockedItems[id] = true
	}
}

func (m *backpackMock) UnlockItems(ids []uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.unlockCalls++
	for _, id := range ids {
		delete(m.lockedItems, id)
	}
}

func (m *backpackMock) GetLockedAssetIDs() []uint64 { return nil }

func setupTrading(t *testing.T) (*Manager, *requester.Mock, *community.Mock) {
	t.Helper()

	web := requester.New()
	comm := community.New()

	init := module.NewInitContext()
	init.SetService(web)
	init.SetModule(backpack.ModuleName, newBackpackMock())

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
