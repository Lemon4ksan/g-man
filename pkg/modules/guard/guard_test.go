// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules/auth"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

type mockConfService struct {
	mu                  sync.Mutex
	getConfErr          error
	getConfResponse     *ConfirmationsList
	respondErr          error
	lastAcceptedConfID  uint64
	lastRejectedConfID  uint64
	lastAcceptedConfKey string
	lastRejectedConfKey string
}

func (m *mockConfService) GetConfirmations(ctx context.Context, deviceID string, steamID uint64, confKey string, timestamp int64) (*ConfirmationsList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getConfResponse, m.getConfErr
}

func (m *mockConfService) RespondToConfirmation(ctx context.Context, conf *Confirmation, accept bool, deviceID string, steamID uint64, confKey string, timestamp int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if accept {
		m.lastAcceptedConfID = conf.ID
		m.lastAcceptedConfKey = confKey
	} else {
		m.lastRejectedConfID = conf.ID
		m.lastRejectedConfKey = confKey
	}
	return m.respondErr
}

type mockInitContext struct {
	eventBus *bus.Bus
}

func newMockInitContext() *mockInitContext {
	return &mockInitContext{
		eventBus: bus.NewBus(),
	}
}

func (m *mockInitContext) Bus() *bus.Bus        { return m.eventBus }
func (m *mockInitContext) Logger() log.Logger   { return log.Discard }
func (m *mockInitContext) Config() steam.Config { return steam.Config{} }

func (m *mockInitContext) GetModule(name string) steam.Module {
	panic("unimplemented")
}
func (m *mockInitContext) Proto() api.LegacyRequester {
	panic("unimplemented")
}
func (m *mockInitContext) RegisterPacketHandler(eMsg protocol.EMsg, handler socket.Handler) {
	panic("unimplemented")
}
func (m *mockInitContext) RegisterServiceHandler(method string, handler socket.Handler) {
	panic("unimplemented")
}
func (m *mockInitContext) Unified() api.UnifiedRequester {
	panic("unimplemented")
}
func (m *mockInitContext) UnregisterPacketHandler(eMsg protocol.EMsg) {
	panic("unimplemented")
}
func (m *mockInitContext) UnregisterServiceHandler(method string) {
	panic("unimplemented")
}
func (m *mockInitContext) WebAPI() api.WebAPIRequester {
	panic("unimplemented")
}

func validConfig() Config {
	cfg := DefaultConfig()
	cfg.IdentitySecret = "dGhpcyBpcyBhIHZhbGlkIGJhc2U2NCBrZXk="
	cfg.DeviceID = "android:12345"
	cfg.PollInterval = 10 * time.Millisecond
	cfg.MaxBackoff = 50 * time.Millisecond
	cfg.RateLimit = 1 * time.Millisecond
	return cfg
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"Valid", validConfig(), false},
		{"Missing IdentitySecret", func() Config { cfg := validConfig(); cfg.IdentitySecret = ""; return cfg }(), true},
		{"Missing DeviceID", func() Config { cfg := validConfig(); cfg.DeviceID = ""; return cfg }(), true},
		{"Invalid PollInterval", func() Config { cfg := validConfig(); cfg.PollInterval = 0; return cfg }(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGuardian_PollingLogic(t *testing.T) {
	cfg := validConfig()
	g, _ := New(cfg)

	mockSvc := &mockConfService{}
	initCtx := newMockInitContext()
	g.service = mockSvc
	_ = g.Init(initCtx)

	sub := initCtx.eventBus.Subscribe(&ConfirmationReceivedEvent{})

	mockSvc.getConfResponse = &ConfirmationsList{
		Success: true,
		Confirmations: []*Confirmation{
			{ID: 101, Type: ConfTypeTrade, Title: "Trade with Alice"},
		},
	}

	_ = g.StartPolling(t.Context())

	select {
	case ev := <-sub.C():
		confEv := ev.(*ConfirmationReceivedEvent)
		if confEv.Confirmation.ID != 101 {
			t.Errorf("expected conf ID 101, got %d", confEv.Confirmation.ID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for ConfirmationReceivedEvent")
	}

	mockSvc.mu.Lock()
	mockSvc.getConfResponse.Confirmations = append(mockSvc.getConfResponse.Confirmations, &Confirmation{
		ID: 102, Type: ConfTypeMarket, Title: "Sell Item",
	})
	mockSvc.mu.Unlock()

	select {
	case ev := <-sub.C():
		confEv := ev.(*ConfirmationReceivedEvent)
		if confEv.Confirmation.ID != 102 {
			t.Errorf("expected new conf ID 102, got %d", confEv.Confirmation.ID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for new ConfirmationReceivedEvent")
	}
}

func TestGuardian_AutoAccept(t *testing.T) {
	cfg := validConfig()
	cfg.AutoAccept = true
	cfg.AutoAcceptTypes = []ConfirmationType{ConfTypeTrade}

	g, _ := New(cfg)
	mockSvc := &mockConfService{}
	initCtx := newMockInitContext()
	g.service = mockSvc
	_ = g.Init(initCtx)

	mockSvc.getConfResponse = &ConfirmationsList{
		Success: true,
		Confirmations: []*Confirmation{
			{ID: 201, Type: ConfTypeTrade, Title: "Auto-Accept Me"},
			{ID: 202, Type: ConfTypeMarket, Title: "Do Not Auto-Accept"},
		},
	}

	_ = g.StartPolling(t.Context())

	deadline := time.Now().Add(500 * time.Millisecond)
	success := false
	for time.Now().Before(deadline) {
		mockSvc.mu.Lock()
		accepted := mockSvc.lastAcceptedConfID
		mockSvc.mu.Unlock()
		if accepted == 201 {
			success = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !success {
		t.Error("expected trade confirmation 201 to be auto-accepted")
	}
}

func TestGuardian_PollingBackoff(t *testing.T) {
	cfg := validConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.MaxBackoff = 40 * time.Millisecond
	cfg.MaxPollFailures = 1 // Уменьшаем для теста

	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	mockSvc := &mockConfService{
		getConfErr: errors.New("steam is down"),
	}
	g.service = mockSvc
	_ = g.Init(newMockInitContext())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = g.StartPolling(ctx)

	// Ожидаем, пока цикл отработает.
	// 1-й фейл (сразу), 2-й фейл (через 10ms),
	// затем backoff до 20ms, потом до 40ms.
	time.Sleep(90 * time.Millisecond)

	// Мы не можем точно измерить интервал тикера, но можем проверить, что
	// при успешном ответе он сбрасывается обратно.
	mockSvc.mu.Lock()
	mockSvc.getConfErr = nil
	mockSvc.getConfResponse = &ConfirmationsList{Success: true}
	mockSvc.mu.Unlock()

	// Даем время на успешный полл и сброс
	time.Sleep(50 * time.Millisecond)

	// Если бы backoff не сбросился, следующий полл был бы через 40ms+,
	// а так он должен быть через 10ms. Точно проверить сложно, но
	// мы убедились, что код не падает в панику и продолжает работать.
}

func TestGuardian_Respond(t *testing.T) {
	cfg := validConfig()
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	mockSvc := &mockConfService{}
	g.service = mockSvc

	conf := &Confirmation{ID: 301}

	// Accept
	err = g.Accept(context.Background(), conf)
	if err != nil {
		t.Fatalf("Accept failed: %v", err)
	}
	if mockSvc.lastAcceptedConfID != 301 {
		t.Error("Accept did not call service correctly")
	}
	if mockSvc.lastAcceptedConfKey == "" {
		t.Error("expected a confirmation key to be generated for accept")
	}

	// Cancel
	err = g.Cancel(context.Background(), conf)
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	if mockSvc.lastRejectedConfID != 301 {
		t.Error("Cancel did not call service correctly")
	}
	if mockSvc.lastRejectedConfKey == "" {
		t.Error("expected a confirmation key to be generated for cancel")
	}

	if g.Metrics().TotalAccepted.Load() != 1 || g.Metrics().TotalRejected.Load() != 1 {
		t.Errorf("metrics not updated correctly: accepted=%d, rejected=%d",
			g.Metrics().TotalAccepted.Load(), g.Metrics().TotalRejected.Load())
	}
}

func TestGuardian_HandleStateChange(t *testing.T) {
	cfg := validConfig()
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	_ = g.Init(newMockInitContext())

	// Запускаем опрос, чтобы был активный pollingCtx
	ctx, cancel := context.WithCancel(context.Background())
	g.pollingCtx = ctx
	g.pollingCancel = cancel
	g.state.Store(int32(StatePolling))

	// Имитируем событие дисконнекта
	g.handleStateChange(&auth.StateEvent{New: auth.StateDisconnected})

	if g.State() != StateStopped {
		t.Errorf("expected state Stopped after disconnect, got %s", g.State())
	}

	// Проверяем, что контекст был отменен
	select {
	case <-g.pollingCtx.Done():
		// Успех
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected polling context to be canceled on disconnect")
	}
}
