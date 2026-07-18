// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/log"
	"github.com/lemon4ksan/miyako/sync/lazy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	module "github.com/lemon4ksan/g-man/test/mock"
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

type mockServiceDoer struct {
	t *testing.T
}

func (m *mockServiceDoer) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	targetStr := fmt.Sprintf("%v", req.Target())
	if strings.Contains(targetStr, "Time") {
		timeResp := &pb.CTwoFactor_Time_Response{
			ServerTime: proto.Uint64(uint64(time.Now().Unix() + 10)),
		}
		body, _ := proto.Marshal(timeResp)

		return tr.NewResponse(io.NopCloser(bytes.NewReader(body)), tr.SocketMetadata{Result: enums.EResult_OK}), nil
	}

	if strings.Contains(targetStr, "Status") {
		statusResp := &pb.CTwoFactor_Status_Response{
			DeviceIdentifier: proto.String("android:mock_id"),
		}
		body, _ := proto.Marshal(statusResp)

		return tr.NewResponse(io.NopCloser(bytes.NewReader(body)), tr.SocketMetadata{Result: enums.EResult_OK}), nil
	}

	return tr.NewResponse(io.NopCloser(bytes.NewReader(nil)), tr.SocketMetadata{Result: enums.EResult_OK}), nil
}

type mockInitContextWithService struct {
	*module.InitContext
	doer service.Doer
}

func (m *mockInitContextWithService) Service() service.Doer {
	return m.doer
}

func (m *mockInitContextWithService) Logger() log.Logger {
	return log.Discard
}

func (m *mockInitContextWithService) Bus() *bus.Bus {
	return bus.New()
}

func defaultValidConfig() Config {
	return Config{
		IdentitySecret: validSecret,
		DeviceID:       "android:123",
		RateLimit:      0,
	}
}

func setupGuardian(t *testing.T, cfg Config) (*Guardian, *module.InitContext, *MockConfService) {
	t.Helper()

	g, err := New(cfg)
	require.NoError(t, err)

	ictx := module.NewInitContext()
	err = g.Init(ictx)
	require.NoError(t, err)

	mockSvc := new(MockConfService)
	g.service = mockSvc

	return g, ictx, mockSvc
}

func setupAuthenticatedGuardian(
	t *testing.T,
	cfg Config,
	steamID id.ID,
) (*Guardian, *module.InitContext, *MockConfService) {
	t.Helper()
	g, ictx, mockSvc := setupGuardian(t, cfg)

	actx := module.NewAuthContext(steamID)
	err := g.StartAuthed(t.Context(), actx)
	require.NoError(t, err)

	g.service = mockSvc

	return g, ictx, mockSvc
}

func TestFrom_NilClient_ReturnsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, From(nil))
}

func TestWithModule_ValidOption_RegistersGuardian(t *testing.T) {
	t.Parallel()

	opt := WithModule(defaultValidConfig())
	assert.NotNil(t, opt)
}

func TestConfigValidate_VariousConfigs_ReturnsExpectedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errStr  string
	}{
		{
			name: "valid",
			cfg: Config{
				IdentitySecret: validSecret,
				DeviceID:       "android:123",
				RateLimit:      time.Second,
			},
			wantErr: false,
		},
		{
			name: "missing_secret",
			cfg: Config{
				DeviceID: "android:123",
			},
			wantErr: true,
			errStr:  "identity secret is required",
		},
		{
			name: "invalid_device_prefix",
			cfg: Config{
				IdentitySecret: validSecret,
				DeviceID:       "pc:123",
			},
			wantErr: true,
			errStr:  "must start with 'android:' or 'ios:'",
		},
		{
			name: "missing_device_id",
			cfg: Config{
				IdentitySecret: validSecret,
				DeviceID:       "",
			},
			wantErr: true,
			errStr:  "device ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)

				if tt.errStr != "" {
					assert.ErrorContains(t, err, tt.errStr)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("config_string_mask", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "GuardConfig{DeviceID: andr...2345}", Config{DeviceID: "android:12345"}.String())
	})
}

func TestInit_NilService_DoesNotInitializeTwoFactorSvc(t *testing.T) {
	t.Parallel()

	g, err := New(defaultValidConfig())
	require.NoError(t, err)

	ictx := &mockInitContextWithService{
		InitContext: module.NewInitContext(),
		doer:        nil,
	}
	err = g.Init(ictx)
	require.NoError(t, err)

	assert.Nil(t, g.twoFactorSvc)
}

func TestLifecycle_AuthAndClose_TransitionsPollingState(t *testing.T) {
	t.Parallel()

	g, _, _ := setupGuardian(t, defaultValidConfig())
	ctx := t.Context()

	err := g.Start(ctx)
	assert.NoError(t, err)

	sid := id.ID(76561198000000001)
	actx := module.NewAuthContext(sid)
	err = g.StartAuthed(ctx, actx)
	assert.NoError(t, err)
	assert.Equal(t, PollingStopped, g.PollingState())

	err = g.Close()
	assert.NoError(t, err)
	assert.Equal(t, PollingStopped, g.PollingState())
}

func TestStartAuthed_ValidDoer_SynchronizesTimeAndLogsStatus(t *testing.T) {
	t.Parallel()

	g, err := New(defaultValidConfig())
	require.NoError(t, err)

	doer := &mockServiceDoer{t: t}
	ictx := &mockInitContextWithService{
		InitContext: module.NewInitContext(),
		doer:        doer,
	}

	err = g.Init(ictx)
	require.NoError(t, err)

	ctx := t.Context()
	sid := id.ID(76561198000000001)
	actx := module.NewAuthContext(sid)

	err = g.StartAuthed(ctx, actx)
	assert.NoError(t, err)
}

func TestGuardian_SetConfig_UpdatesConfig(t *testing.T) {
	t.Parallel()

	cfg := defaultValidConfig()
	g, _, _ := setupGuardian(t, cfg)

	newCfg := Config{IdentitySecret: "new-secret", DeviceID: "ios:555"}
	g.SetConfig(newCfg)

	assert.Equal(t, newCfg, g.Config())
}

func TestGuardian_Service_ReturnsConfService(t *testing.T) {
	t.Parallel()

	g, _, mockSvc := setupGuardian(t, defaultValidConfig())
	assert.Equal(t, mockSvc, g.Service())
}

func TestFetchConfirmations_VariousResponses_HandlesExpectedly(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		cfg := defaultValidConfig()
		g, _, mockSvc := setupAuthenticatedGuardian(t, cfg, id.ID(123))

		expectedConfs := []*Confirmation{{ID: 1, Title: "Trade"}}
		mockSvc.On("GetConfirmations", mock.Anything, cfg.DeviceID, g.SteamID(), mock.Anything, mock.Anything).
			Return(&ConfirmationsList{Success: true, Confirmations: expectedConfs}, nil).Once()

		confs, err := g.FetchConfirmations(t.Context())
		assert.NoError(t, err)
		assert.Len(t, confs, 1)
		assert.Equal(t, int64(1), g.metrics.TotalFetched.Load())
	})

	t.Run("steam_error_and_event", func(t *testing.T) {
		t.Parallel()

		cfg := defaultValidConfig()
		g, _, mockSvc := setupAuthenticatedGuardian(t, cfg, id.ID(123))

		sub := g.Bus.Subscribe(&NeedAuthEvent{})
		defer sub.Unsubscribe()

		mockSvc.On("GetConfirmations", mock.Anything, cfg.DeviceID, g.SteamID(), mock.Anything, mock.Anything).
			Return(&ConfirmationsList{Success: false, NeedAuth: true, Message: "reauth"}, nil).Once()

		_, err := g.FetchConfirmations(t.Context())
		assert.Error(t, err)

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			assert.Equal(t, "reauth", ev.(*NeedAuthEvent).Message)
		case <-waitCtx.Done():
			t.Fatal("timeout waiting for NeedAuthEvent")
		}
	})

	t.Run("steam_error_without_need_auth", func(t *testing.T) {
		t.Parallel()

		g, _, mockSvc := setupAuthenticatedGuardian(t, defaultValidConfig(), id.ID(123))

		mockSvc.On("GetConfirmations", mock.Anything, mock.Anything, g.SteamID(), mock.Anything, mock.Anything).
			Return(&ConfirmationsList{Success: false, NeedAuth: false, Message: "invalid_key"}, nil).Once()

		_, err := g.FetchConfirmations(t.Context())
		assert.ErrorContains(t, err, "steam rejected request: invalid_key")
		assert.Equal(t, int64(1), g.metrics.TotalErrors.Load())
	})

	t.Run("service_network_error", func(t *testing.T) {
		t.Parallel()

		g, _, mockSvc := setupAuthenticatedGuardian(t, defaultValidConfig(), id.ID(123))

		mockSvc.On("GetConfirmations", mock.Anything, mock.Anything, g.SteamID(), mock.Anything, mock.Anything).
			Return(nil, errors.New("network timeout")).Once()

		_, err := g.FetchConfirmations(t.Context())
		assert.ErrorContains(t, err, "network timeout")
		assert.Equal(t, int64(1), g.metrics.TotalErrors.Load())
	})

	t.Run("error_not_authenticated", func(t *testing.T) {
		t.Parallel()

		g := &Guardian{}
		_, err := g.FetchConfirmations(t.Context())
		assert.ErrorIs(t, err, ErrNotAuthenticated)
	})

	t.Run("error_invalid_secret", func(t *testing.T) {
		t.Parallel()

		cfg := Config{IdentitySecret: "invalid-b64-!!!", DeviceID: "android:123"}
		g, _, _ := setupAuthenticatedGuardian(t, cfg, id.ID(123))

		_, err := g.FetchConfirmations(t.Context())
		assert.ErrorContains(t, err, "key generation")
	})

	t.Run("error_not_configured", func(t *testing.T) {
		t.Parallel()

		var g *Guardian

		_, err := g.FetchConfirmations(t.Context())
		assert.ErrorIs(t, err, ErrNotConfigured)
	})
}

func TestRespond_AcceptOrCancel_UpdatesMetricsAndCallsService(t *testing.T) {
	t.Parallel()

	conf := &Confirmation{ID: 99, Title: "Test"}

	t.Run("accept_single", func(t *testing.T) {
		t.Parallel()

		cfg := defaultValidConfig()
		g, _, mockSvc := setupAuthenticatedGuardian(t, cfg, id.ID(123))

		mockSvc.On("RespondToConfirmation", mock.Anything, conf, true, cfg.DeviceID, g.SteamID(), mock.Anything, mock.Anything).
			Return(nil).
			Once()

		err := g.Accept(t.Context(), conf)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), g.metrics.TotalAccepted.Load())
	})

	t.Run("accept_multiple", func(t *testing.T) {
		t.Parallel()

		cfg := defaultValidConfig()
		g, _, mockSvc := setupAuthenticatedGuardian(t, cfg, id.ID(123))

		confs := []*Confirmation{{ID: 1}, {ID: 2}}
		mockSvc.On("RespondToMultiple", mock.Anything, confs, true, cfg.DeviceID, g.SteamID(), mock.Anything, mock.Anything).
			Return(nil).
			Once()

		err := g.AcceptMultiple(t.Context(), confs)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), g.metrics.TotalAccepted.Load())
	})

	t.Run("cancel_multiple", func(t *testing.T) {
		t.Parallel()

		cfg := defaultValidConfig()
		g, _, mockSvc := setupAuthenticatedGuardian(t, cfg, id.ID(123))

		confs := []*Confirmation{{ID: 1}, {ID: 2}}
		mockSvc.On("RespondToMultiple", mock.Anything, confs, false, cfg.DeviceID, g.SteamID(), mock.Anything, mock.Anything).
			Return(nil).
			Once()

		err := g.CancelMultiple(t.Context(), confs)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), g.metrics.TotalRejected.Load())
	})

	t.Run("respond_single_error", func(t *testing.T) {
		t.Parallel()

		g, _, mockSvc := setupAuthenticatedGuardian(t, defaultValidConfig(), id.ID(123))

		mockSvc.On("RespondToConfirmation", mock.Anything, mock.Anything, true, mock.Anything, g.SteamID(), mock.Anything, mock.Anything).
			Return(errors.New("respond fail")).
			Once()

		err := g.Accept(t.Context(), &Confirmation{})
		assert.ErrorContains(t, err, "respond fail")
		assert.Equal(t, int64(1), g.metrics.TotalErrors.Load())
	})

	t.Run("respond_multiple_error", func(t *testing.T) {
		t.Parallel()

		g, _, mockSvc := setupAuthenticatedGuardian(t, defaultValidConfig(), id.ID(123))

		confs := []*Confirmation{{ID: 1}, {ID: 2}}
		mockSvc.On("RespondToMultiple", mock.Anything, confs, true, mock.Anything, g.SteamID(), mock.Anything, mock.Anything).
			Return(errors.New("multiple fail")).
			Once()

		err := g.AcceptMultiple(t.Context(), confs)
		assert.ErrorContains(t, err, "multiple fail")
		assert.Equal(t, int64(1), g.metrics.TotalErrors.Load())
	})

	t.Run("respond_key_generation_error", func(t *testing.T) {
		t.Parallel()

		cfg := defaultValidConfig()
		cfg.IdentitySecret = "invalid-b64-!!!"
		g, _, _ := setupAuthenticatedGuardian(t, cfg, id.ID(123))

		err := g.Accept(t.Context(), &Confirmation{})
		assert.ErrorContains(t, err, "illegal base64 data")
	})

	t.Run("nil_guardian_respond_error", func(t *testing.T) {
		t.Parallel()

		var g *Guardian

		err := g.Accept(t.Context(), &Confirmation{})
		assert.ErrorIs(t, err, ErrNotConfigured)
	})
}

func TestHelpers_StateAndMasking_ReturnsExpectedStrings(t *testing.T) {
	t.Parallel()

	t.Run("polling_state_string", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "stopped", PollingStopped.String())
		assert.Equal(t, "polling", PollingActive.String())
		assert.Equal(t, "unknown", PollingState(99).String())
	})

	t.Run("device_id_masking", func(t *testing.T) {
		t.Parallel()
		assert.Contains(t, maskDeviceID("android:123456789"), "andr...")
		assert.Equal(t, "****", maskDeviceID("short"))
	})

	t.Run("confirmation_type_string", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "generic", ConfTypeGeneric.String())
		assert.Equal(t, "trade", ConfTypeTrade.String())
		assert.Equal(t, "market", ConfTypeMarket.String())
		assert.Equal(t, "login", ConfTypeLogin.String())
		assert.Equal(t, "account_change", ConfTypeAccountChange.String())
		assert.Equal(t, "unknown", ConfirmationType(99).String())
	})
}

func TestClock_OffsetAndSystem_ReturnsTimeWithinLimits(t *testing.T) {
	t.Parallel()

	t.Run("offset_clock", func(t *testing.T) {
		t.Parallel()

		oc := &OffsetClock{}
		oc.SetOffset(10 * time.Second)
		assert.WithinDuration(t, time.Now().Add(10*time.Second), oc.Now(), 2*time.Second)
	})

	t.Run("system_clock", func(t *testing.T) {
		t.Parallel()

		sc := SystemClock{}
		assert.WithinDuration(t, time.Now(), sc.Now(), 2*time.Second)
	})
}

func TestConfirmation_ExpiryCalculation_ReportsCorrectTimeAndString(t *testing.T) {
	t.Parallel()

	t.Run("expiry_calculation", func(t *testing.T) {
		t.Parallel()

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
	})
}

func TestInit_InvalidStartAuthedOrConfig_ReturnsExpectedErrors(t *testing.T) {
	t.Parallel()

	t.Run("start_authed_failure", func(t *testing.T) {
		t.Parallel()

		g := &Guardian{}
		err := g.StartAuthed(t.Context(), nilCommunityAuthContext{})
		assert.ErrorContains(t, err, "community client is required")
	})

	t.Run("new_validation_failure", func(t *testing.T) {
		t.Parallel()

		_, err := New(Config{})
		assert.Error(t, err)
	})
}

func TestMetrics_Always_ReturnsNonNilMetrics(t *testing.T) {
	t.Parallel()

	g := &Guardian{metrics: &GuardianMetrics{}}
	assert.NotNil(t, g.Metrics())
}

func TestGenerateAuthCode_WithOrWithoutSecret_GeneratesExpectedCode(t *testing.T) {
	t.Parallel()

	g := &Guardian{clock: &OffsetClock{}}
	res := g.GenerateAuthCode()
	assert.True(t, res.IsSuccess())
	optCode, err := res.Unwrap()
	assert.NoError(t, err)
	assert.False(t, optCode.IsPresent())

	g.config.SharedSecret = validSecret
	res = g.GenerateAuthCode()
	assert.True(t, res.IsSuccess())
	optCode, err = res.Unwrap()
	assert.NoError(t, err)
	assert.True(t, optCode.IsPresent())
	code, _ := optCode.Value()
	assert.NotEmpty(t, code)
}

func TestFetchAndAccept_RateLimiterWithCanceledContext_ReturnsCanceledError(t *testing.T) {
	t.Parallel()

	t.Run("fetch_confirmations_timeout", func(t *testing.T) {
		t.Parallel()

		cfg := defaultValidConfig()
		cfg.RateLimit = time.Hour

		g, _, _ := setupAuthenticatedGuardian(t, cfg, id.ID(123))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := g.FetchConfirmations(ctx)
		assert.Error(t, err)
	})

	t.Run("accept_confirmation_timeout", func(t *testing.T) {
		t.Parallel()

		cfg := defaultValidConfig()
		cfg.RateLimit = time.Hour

		g, _, _ := setupAuthenticatedGuardian(t, cfg, id.ID(123))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := g.Accept(ctx, &Confirmation{})
		assert.Error(t, err)
	})
}

func TestTwoFactorService_QueryErrors_HandlesGracefullyWithoutPanic(t *testing.T) {
	t.Parallel()

	g := &Guardian{
		clock: &OffsetClock{},
	}
	g.twoFactorSvc = lazy.New(func() (*TwoFactorService, error) {
		return nil, errors.New("lazy fetch error")
	})

	g.synchronizeTimeOffset(t.Context())
	g.logGuardStatus(t.Context(), nilCommunityAuthContext{})
}

type nilCommunityAuthContext struct {
	module.AuthContext
}

func (nilCommunityAuthContext) Community() community.Requester { return nil }
func (nilCommunityAuthContext) SteamID() id.ID                 { return 0 }
