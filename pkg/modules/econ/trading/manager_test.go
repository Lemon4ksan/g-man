// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/modules/auth"
	"github.com/lemon4ksan/g-man/pkg/modules/econ"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

type mockWebAPIRequester struct {
	mu          sync.Mutex
	calls       map[string]int
	responses   map[string]any
	responseErr error
}

func newMockWebAPI() *mockWebAPIRequester {
	return &mockWebAPIRequester{
		calls:     make(map[string]int),
		responses: make(map[string]any),
	}
}

func (m *mockWebAPIRequester) CallWebAPI(ctx context.Context, httpMethod, iface, method string, version int, respMsg any, mods ...api.RequestModifier) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := iface + "/" + method
	m.calls[key]++

	if m.responseErr != nil {
		return m.responseErr
	}
	if resp, ok := m.responses[key]; ok {
		jsonBytes, _ := json.Marshal(resp)
		_ = json.Unmarshal(jsonBytes, respMsg)
	}
	return nil
}

func (m *mockWebAPIRequester) Do(*tr.Request) (*tr.Response, error) {
	panic("Not implemented")
}

type mockCommunityClient struct {
	mu          sync.Mutex
	responses   map[string]string // map[url]html_body
	responseErr error
}

func (m *mockCommunityClient) Get(ctx context.Context, path string, mods ...api.RequestModifier) (*tr.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.responseErr != nil {
		return nil, m.responseErr
	}

	body, ok := m.responses[path]
	if !ok {
		return nil, errors.New("mock page not found")
	}

	return &tr.Response{
		StatusCode: http.StatusOK,
		Body:       []byte(body),
	}, nil
}

func (m *mockCommunityClient) SessionID(url string) string {
	panic("unimplemented")
}
func (m *mockCommunityClient) Do(req *tr.Request) (*tr.Response, error) {
	panic("unimplemented")
}
func (m *mockCommunityClient) GetJSON(ctx context.Context, path string, params url.Values, target any, mods ...api.RequestModifier) error {
	panic("unimplemented")
}
func (m *mockCommunityClient) PostForm(ctx context.Context, path string, data url.Values, mods ...api.RequestModifier) (*tr.Response, error) {
	panic("unimplemented")
}
func (m *mockCommunityClient) PostJSON(ctx context.Context, path string, payload any, mods ...api.RequestModifier) (*tr.Response, error) {
	panic("unimplemented")
}

func newMockCommunity() *mockCommunityClient {
	return &mockCommunityClient{
		responses: make(map[string]string),
	}
}

type mockManagerOfferHandler struct {
	enqueueCount int
	mu           sync.Mutex
}

func (h *mockManagerOfferHandler) ProcessOffer(ctx context.Context, offer *TradeOffer) (ActionDecision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enqueueCount++
	return ActionDecision{Action: ActionSkip}, nil
}
func (h *mockManagerOfferHandler) OnActionFailed(ctx context.Context, offer *TradeOffer, action ActionType, reason string, err error) {
}

func TestManager_StateTransitions(t *testing.T) {
	m := New(DefaultConfig())
	m.bus = &bus.Bus{} // Простая заглушка

	// StartPolling
	err := m.StartPolling(context.Background())
	if err != nil {
		t.Fatalf("StartPolling failed: %v", err)
	}
	if m.state.Load() != int32(StatePolling) {
		t.Errorf("expected state Polling, got %s", State(m.state.Load()).String())
	}

	// Попытка запустить снова
	err = m.StartPolling(context.Background())
	if !errors.Is(err, ErrManagerPolling) {
		t.Errorf("expected ErrManagerPolling, got %v", err)
	}

	// StopPolling
	m.StopPolling()
	if m.state.Load() != int32(StateStopped) {
		t.Errorf("expected state Stopped, got %s", State(m.state.Load()).String())
	}

	// Close
	_ = m.Close()
	if m.state.Load() != int32(StateClosed) {
		t.Errorf("expected state Closed, got %s", State(m.state.Load()).String())
	}
}

func TestManager_PollingLogic(t *testing.T) {
	webAPI := newMockWebAPI()
	m := New(DefaultConfig())
	m.pollingCtx = context.Background()
	m.bus = bus.NewBus()
	m.web = webAPI

	handler := &mockManagerOfferHandler{}
	m.SetOfferHandler(context.Background(), handler)

	subNew := m.bus.Subscribe(&NewOfferEvent{})
	subChanged := m.bus.Subscribe(&OfferChangedEvent{})

	webAPI.responses["IEconService/GetTradeOffers"] = map[string]any{
		"response": map[string]any{
			"trade_offers_received": []any{
				map[string]any{"tradeofferid": "123", "trade_offer_state": econ.TradeOfferStateActive},
			},
		},
	}

	m.doPoll()
	time.Sleep(50 * time.Millisecond)

	select {
	case ev := <-subNew.C():
		if ev.(*NewOfferEvent).Offer.ID != 123 {
			t.Errorf("expected new offer event for ID 123")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for NewOfferEvent")
	}

	if handler.enqueueCount != 1 {
		t.Errorf("expected offer to be enqueued, count=%d", handler.enqueueCount)
	}

	webAPI.responses["IEconService/GetTradeOffers"] = map[string]any{
		"response": map[string]any{
			"trade_offers_received": []any{
				map[string]any{"tradeofferid": "123", "trade_offer_state": econ.TradeOfferStateAccepted},
			},
		},
	}

	m.doPoll()

	select {
	case ev := <-subChanged.C():
		changedEv := ev.(*OfferChangedEvent)
		if changedEv.Offer.ID != 123 || changedEv.OldState != econ.TradeOfferStateActive {
			t.Errorf("invalid OfferChangedEvent data: %+v", changedEv)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for OfferChangedEvent")
	}

	m.doPoll()
	select {
	case <-time.After(50 * time.Millisecond):
	case <-subNew.C():
		t.Fatal("unexpected NewOfferEvent")
	case <-subChanged.C():
		t.Fatal("unexpected OfferChangedEvent")
	}
}

func TestManager_GetEscrowDuration(t *testing.T) {
	createEscrowHTML := func(myDays, theirDays int) string {
		return fmt.Sprintf(`
			<html><body>
				<script type="text/javascript">
					var g_daysMyEscrow = %d;
					var g_DaysTheirEscrow = %d;
				</script>
			</body></html>
		`, myDays, theirDays)
	}

	community := newMockCommunity()
	m := New(DefaultConfig())
	m.community = community

	tests := []struct {
		name        string
		setupMock   func()
		offerID     uint64
		wantDetails EscrowDetails
		wantErr     error
	}{
		{
			name: "Success - 7 day hold",
			setupMock: func() {
				community.responseErr = nil
				community.responses["tradeoffer/100/"] = createEscrowHTML(0, 7)
			},
			offerID:     100,
			wantDetails: EscrowDetails{MyDays: 0, TheirDays: 7},
			wantErr:     nil,
		},
		{
			name: "Success - No hold",
			setupMock: func() {
				community.responseErr = nil
				community.responses["tradeoffer/200/"] = createEscrowHTML(0, 0)
			},
			offerID:     200,
			wantDetails: EscrowDetails{MyDays: 0, TheirDays: 0},
			wantErr:     nil,
		},
		{
			name: "Community client not ready",
			setupMock: func() {
				m.community = nil
			},
			offerID: 300,
			wantErr: ErrCommunityNotReady,
		},
		{
			name: "Escrow data not found on page",
			setupMock: func() {
				m.community = community
				community.responseErr = nil
				community.responses["tradeoffer/400/"] = "<html><body>Invalid HTML</body></html>"
			},
			offerID: 400,
			wantErr: ErrEscrowNotFound,
		},
		{
			name: "Community HTTP error",
			setupMock: func() {
				community.responseErr = errors.New("steam down")
			},
			offerID: 500,
			wantErr: errors.New("failed to fetch offer page: steam down"), // Проверяем wrapping
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			details, err := m.GetEscrowDuration(context.Background(), tt.offerID)

			if !reflect.DeepEqual(details, tt.wantDetails) {
				t.Errorf("GetEscrowDuration() details = %v, want %v", details, tt.wantDetails)
			}

			if (err != nil && tt.wantErr == nil) || (err == nil && tt.wantErr != nil) || (err != nil && tt.wantErr != nil && err.Error() != tt.wantErr.Error()) {
				t.Errorf("GetEscrowDuration() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManager_handleStateChange(t *testing.T) {
	m := New(DefaultConfig())
	m.bus = &bus.Bus{}

	ctx, cancel := context.WithCancel(context.Background())
	m.pollingCtx = ctx
	m.pollingCancel = cancel

	m.handleStateChange(&auth.StateEvent{New: auth.StateDisconnected})

	select {
	case <-m.pollingCtx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected polling context to be canceled on disconnect")
	}
}
