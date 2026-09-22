// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/guard"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/test/mock"
)

func TestConfirmationPoller_Lifecycle(t *testing.T) {
	httpStub := mock.NewHTTPStub()
	mobileConf := guard.NewMobileConf(httpStub)

	// Mock empty confirmations list
	httpStub.SetRawResponse("mobileconf/getlist", 200, []byte(`{"success":true,"conf":[]}`))

	poller := guard.NewPoller(mobileConf, guard.PollerConfig{
		DeviceID:       "android:00000000-0000-0000-0000-000000000000",
		SteamID:        id.ID(76561198000000001),
		IdentitySecret: "YWJjZGVmZ2hpamtsbW5vcA==",
		IdleInterval:   100 * time.Millisecond,
		BurstInterval:  20 * time.Millisecond,
		BurstDuration:  100 * time.Millisecond,
		AutoConfirmAll: true,
	})

	ctx := t.Context()

	// 1. Start poller
	poller.Start(ctx)

	// 2. Trigger burst mode
	poller.Trigger()

	time.Sleep(50 * time.Millisecond)

	// 3. Stop poller
	poller.Stop()
}

func TestConfirmationPoller_PollOnce(t *testing.T) {
	httpStub := mock.NewHTTPStub()
	mobileConf := guard.NewMobileConf(httpStub)

	// Mock confirmations with one pending trade confirmation
	httpStub.SetRawResponse(
		"mobileconf/getlist",
		200,
		[]byte(
			`{"success":true,"conf":[{"id":1001,"nonce":2002,"type":2,"type_name":"Trade","creator_id":76561198000000001}]}`,
		),
	)
	httpStub.SetRawResponse("mobileconf/multiajaxop", 200, []byte(`{"success":true}`))

	var confirmedID uint64

	poller := guard.NewPoller(mobileConf, guard.PollerConfig{
		DeviceID:       "android:00000000-0000-0000-0000-000000000000",
		SteamID:        id.ID(76561198000000001),
		IdentitySecret: "YWJjZGVmZ2hpamtsbW5vcA==",
		OnConfirmation: func(conf *guard.Confirmation) bool {
			confirmedID = conf.ID
			return true
		},
	})

	confs, err := poller.PollOnce(context.Background())
	require.NoError(t, err)
	assert.Len(t, confs, 1)
	assert.Equal(t, uint64(1001), confirmedID)
}

func TestConfirmationPoller_StopConcurrency(t *testing.T) {
	t.Parallel()

	httpStub := mock.NewHTTPStub()
	mobileConf := guard.NewMobileConf(httpStub)

	poller := guard.NewPoller(mobileConf, guard.PollerConfig{
		DeviceID:       "android:00000000-0000-0000-0000-000000000000",
		SteamID:        id.ID(76561198000000001),
		IdentitySecret: "YWJjZGVmZ2hpamtsbW5vcA==",
		IdleInterval:   100 * time.Millisecond,
	})

	poller.Start(t.Context())

	// Test concurrent Stop invocations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			poller.Stop()
		}()
	}

	wg.Wait()

	// Ensure poller can be safely restarted after Stop
	ctx, cancel := context.WithCancel(t.Context())
	poller.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()

	// Ensure Stop after context cancellation does not panic
	poller.Stop()
}

func TestConfirmationPoller_RestartStress(t *testing.T) {
	t.Parallel()

	httpStub := mock.NewHTTPStub()
	mobileConf := guard.NewMobileConf(httpStub)

	poller := guard.NewPoller(mobileConf, guard.PollerConfig{
		DeviceID:       "android:00000000-0000-0000-0000-000000000000",
		SteamID:        id.ID(76561198000000001),
		IdentitySecret: "YWJjZGVmZ2hpamtsbW5vcA==",
		IdleInterval:   10 * time.Millisecond,
		BurstInterval:  5 * time.Millisecond,
		BurstDuration:  20 * time.Millisecond,
	})

	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		poller.Start(ctx)

		if i%2 == 0 {
			poller.Trigger()
		}

		time.Sleep(2 * time.Millisecond)
		poller.Stop()
		cancel()
	}
}

func TestConfirmationPoller_ConcurrentStartStopThrashing(t *testing.T) {
	t.Parallel()

	httpStub := mock.NewHTTPStub()
	mobileConf := guard.NewMobileConf(httpStub)

	poller := guard.NewPoller(mobileConf, guard.PollerConfig{
		DeviceID:       "android:00000000-0000-0000-0000-000000000000",
		SteamID:        id.ID(76561198000000001),
		IdentitySecret: "YWJjZGVmZ2hpamtsbW5vcA==",
		IdleInterval:   10 * time.Millisecond,
		BurstInterval:  5 * time.Millisecond,
		BurstDuration:  20 * time.Millisecond,
	})

	var wg sync.WaitGroup

	const goroutines = 8

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for ctx.Err() == nil {
				switch id % 3 {
				case 0:
					poller.Start(ctx)
					time.Sleep(1 * time.Millisecond)
				case 1:
					poller.Stop()
				case 2:
					poller.Trigger()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	poller.Stop()
}
