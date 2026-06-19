// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/test/module"
)

const validSecret = "SGVsbG8gV29ybGQ="

type MockConfService struct {
	mock.Mock
}

func (m *MockConfService) GetConfirmations(
	ctx context.Context,
	deviceID string,
	steamID id.ID,
	confKey string,
	timestamp int64,
) (*ConfirmationsList, error) {
	args := m.Called(ctx, deviceID, steamID, confKey, timestamp)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*ConfirmationsList), args.Error(1)
}

func (m *MockConfService) RespondToConfirmation(
	ctx context.Context,
	conf *Confirmation,
	accept bool,
	deviceID string,
	steamID id.ID,
	confKey string,
	timestamp int64,
) error {
	return m.Called(ctx, conf, accept, deviceID, steamID, confKey, timestamp).Error(0)
}

func (m *MockConfService) RespondToMultiple(
	ctx context.Context,
	confs []*Confirmation,
	accept bool,
	deviceID string,
	steamID id.ID,
	confKey string,
	timestamp int64,
) error {
	return m.Called(ctx, confs, accept, deviceID, steamID, confKey, timestamp).Error(0)
}

func setupGuardian(t *testing.T, cfg Config) (*Guardian, *module.InitContext, *MockConfService) {
	g, err := New(cfg)
	require.NoError(t, err)

	ictx := module.NewInitContext()
	err = g.Init(ictx)
	require.NoError(t, err)

	// Inject mock service
	mockSvc := new(MockConfService)
	g.service = mockSvc

	return g, ictx, mockSvc
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			"valid",
			Config{
				IdentitySecret: validSecret,
				DeviceID:       "android:123",
				RateLimit:      time.Second,
			},
			false,
		},
		{"missing secret", Config{DeviceID: "android:123"}, true},
		{"invalid device prefix", Config{IdentitySecret: validSecret, DeviceID: "pc:123"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGuardian_Lifecycle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdentitySecret = validSecret
	cfg.DeviceID = "android:123"

	g, _, _ := setupGuardian(t, cfg)
	ctx := context.Background()

	// 1. Start normally
	err := g.Start(ctx)
	assert.NoError(t, err)

	// 2. StartAuthed
	sid := id.ID(76561198000000001)
	actx := module.NewAuthContext(sid)
	err = g.StartAuthed(ctx, actx)
	assert.NoError(t, err)
	assert.Equal(t, StateStopped, g.fsm.CurrentState())

	// 3. Close
	err = g.Close()
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, g.fsm.CurrentState())
}

func TestGuardian_FetchConfirmations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdentitySecret = validSecret
	cfg.DeviceID = "android:123"
	cfg.RateLimit = 0 // Disable rate limit for test speed

	g, _, mockSvc := setupGuardian(t, cfg)
	g.steamID = id.ID(123)

	t.Run("Success", func(t *testing.T) {
		expectedConfs := []*Confirmation{{ID: 1, Title: "Trade"}}
		mockSvc.On("GetConfirmations", mock.Anything, cfg.DeviceID, g.steamID, mock.Anything, mock.Anything).
			Return(&ConfirmationsList{Success: true, Confirmations: expectedConfs}, nil).Once()

		confs, err := g.FetchConfirmations(context.Background())
		assert.NoError(t, err)
		assert.Len(t, confs, 1)
		assert.Equal(t, int64(1), g.metrics.TotalFetched.Load())
	})

	t.Run("Steam Error and Event", func(t *testing.T) {
		sub := g.Bus.Subscribe(&NeedAuthEvent{})
		defer sub.Unsubscribe()

		mockSvc.On("GetConfirmations", mock.Anything, cfg.DeviceID, g.steamID, mock.Anything, mock.Anything).
			Return(&ConfirmationsList{Success: false, NeedAuth: true, Message: "reauth"}, nil).Once()

		_, err := g.FetchConfirmations(context.Background())
		assert.Error(t, err)

		select {
		case ev := <-sub.C():
			assert.Equal(t, "reauth", ev.(*NeedAuthEvent).Message)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("did not receive NeedAuthEvent")
		}
	})
}

func TestGuardian_AcceptReject(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdentitySecret = validSecret
	cfg.DeviceID = "android:123"
	g, _, mockSvc := setupGuardian(t, cfg)
	g.steamID = id.ID(123)

	conf := &Confirmation{ID: 99, Title: "Test"}

	t.Run("Accept Single", func(t *testing.T) {
		mockSvc.On("RespondToConfirmation", mock.Anything, conf, true, cfg.DeviceID, g.steamID, mock.Anything, mock.Anything).
			Return(nil).
			Once()

		err := g.Accept(context.Background(), conf)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), g.metrics.TotalAccepted.Load())
	})

	t.Run("Cancel Multiple", func(t *testing.T) {
		confs := []*Confirmation{{ID: 1}, {ID: 2}}
		mockSvc.On("RespondToMultiple", mock.Anything, confs, false, cfg.DeviceID, g.steamID, mock.Anything, mock.Anything).
			Return(nil).
			Once()

		err := g.CancelMultiple(context.Background(), confs)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), g.metrics.TotalRejected.Load())
	})
}

func TestHelpers(t *testing.T) {
	// Coverage for helper functions
	assert.Equal(t, "stopped", StateStopped.String())
	assert.Equal(t, "polling", StatePolling.String())
	assert.Equal(t, "closed", StateClosed.String())
	assert.Equal(t, "unknown", State(99).String())

	assert.Contains(t, maskDeviceID("android:123456789"), "andr...")
	assert.Equal(t, "****", maskDeviceID("short"))

	// ConfirmationType String
	assert.Equal(t, "generic", ConfTypeGeneric.String())
	assert.Equal(t, "trade", ConfTypeTrade.String())
	assert.Equal(t, "market", ConfTypeMarket.String())
	assert.Equal(t, "login", ConfTypeLogin.String())
	assert.Equal(t, "account_change", ConfTypeAccountChange.String())
	assert.Equal(t, "unknown", ConfirmationType(99).String())
}

func TestClock(t *testing.T) {
	oc := &OffsetClock{}
	oc.SetOffset(10 * time.Second)
	assert.WithinDuration(t, time.Now().Add(10*time.Second), oc.Now(), 2*time.Second)

	sc := SystemClock{}
	assert.WithinDuration(t, time.Now(), sc.Now(), 2*time.Second)
}

func TestConfig_Validate_Errors(t *testing.T) {
	cfg := Config{IdentitySecret: validSecret, DeviceID: ""}
	assert.ErrorContains(t, cfg.Validate(), "device ID is required")

	cfg2 := Config{IdentitySecret: validSecret, DeviceID: "pc:123"}
	assert.ErrorContains(t, cfg2.Validate(), "must start with 'android:' or 'ios:'")

	cfg3 := Config{IdentitySecret: "", DeviceID: "android:123"}
	assert.ErrorContains(t, cfg3.Validate(), "identity secret is required")

	assert.Equal(t, "GuardConfig{DeviceID: andr...2345}", Config{DeviceID: "android:12345"}.String())
}

func TestConfirmationModel(t *testing.T) {
	c := &Confirmation{
		ID:    1,
		Title: "Trade offer to accept",
		Time:  "12:34",
		Type:  ConfTypeTrade,
	}
	assert.Equal(t, 2*time.Minute, c.TimeRemaining())
	assert.False(t, c.IsExpired())

	c.expiresAt = time.Now().Add(-10 * time.Second)
	assert.True(t, c.IsExpired())
	assert.Less(t, c.TimeRemaining(), time.Duration(0))

	assert.Contains(t, c.String(), "Confirmation{ID=1, Type=trade, Title=\"Trade offer to ac...\", ExpiresIn=")
}

type nilCommunityAuthContext struct {
	module.AuthContext
}

func (nilCommunityAuthContext) Community() community.Requester { return nil }
func (nilCommunityAuthContext) SteamID() id.ID                 { return 0 }

func TestGuardian_StartAuthed_Failure(t *testing.T) {
	g := &Guardian{}
	err := g.StartAuthed(context.Background(), nilCommunityAuthContext{})
	assert.ErrorContains(t, err, "community client is required")
}

func TestGuardian_New_ValidationFailure(t *testing.T) {
	_, err := New(Config{})
	assert.Error(t, err)
}

func TestGuardian_FetchConfirmations_Errors(t *testing.T) {
	g := &Guardian{}
	// service nil
	_, err := g.FetchConfirmations(context.Background())
	assert.ErrorIs(t, err, ErrNotAuthenticated)
}

func TestGuardian_Metrics(t *testing.T) {
	g := &Guardian{metrics: &GuardianMetrics{}}
	assert.NotNil(t, g.Metrics())
}

func TestGuardian_GenerateAuthCode(t *testing.T) {
	g := &Guardian{clock: &OffsetClock{}}
	// Empty shared secret
	code, err := g.GenerateAuthCode()
	assert.NoError(t, err)
	assert.Empty(t, code)

	g.config.SharedSecret = validSecret
	code, err = g.GenerateAuthCode()
	assert.NoError(t, err)
	assert.NotEmpty(t, code)
}

func TestGuardian_FetchConfirmations_InvalidSecret(t *testing.T) {
	cfg := Config{IdentitySecret: "invalid-b64-!!!", DeviceID: "android:123"}
	g, _, _ := setupGuardian(t, cfg)
	g.steamID = id.ID(123)

	_, err := g.FetchConfirmations(context.Background())
	assert.ErrorContains(t, err, "key generation")
}
