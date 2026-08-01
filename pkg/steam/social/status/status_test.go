// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package status_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/social/status"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/test/mock"
)

func TestStatusManager_ReconnectRefresh(t *testing.T) {
	initCtx := mock.NewInitContext()
	eb := initCtx.Bus()
	initCtx.SetModule(apps.ModuleName, apps.New())

	mgr := status.NewManager(
		status.WithUpdateInterval(10*time.Second),
		status.WithSlides("Playing G-man Bot"),
		status.WithIdleAppIDs(730),
	)

	err := mgr.Init(initCtx)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	subStatus := eb.Subscribe(&status.StatusUpdatedEvent{})
	defer subStatus.Unsubscribe()

	err = mgr.StartAuthed(ctx, nil)
	require.NoError(t, err)

	// Verify initial status pushed on start
	select {
	case ev := <-subStatus.C():
		sev, ok := ev.(*status.StatusUpdatedEvent)
		require.True(t, ok)
		assert.Equal(t, "Playing G-man Bot", sev.StatusText)
		assert.Equal(t, []uint32{730}, sev.IdleAppIDs)
	case <-time.After(2 * time.Second):
		t.Fatal("Expected initial status update on start")
	}

	// Simulate reconnect via LoggedOnEvent
	eb.Publish(&auth.LoggedOnEvent{})

	select {
	case ev := <-subStatus.C():
		sev, ok := ev.(*status.StatusUpdatedEvent)
		require.True(t, ok)
		assert.Equal(t, "Playing G-man Bot", sev.StatusText)
	case <-time.After(2 * time.Second):
		t.Fatal("Expected immediate status update on LoggedOnEvent reconnect")
	}

	// Simulate partial reconnect / state transition via StateEvent
	eb.Publish(&auth.StateEvent{New: auth.StateLoggedOn})

	select {
	case ev := <-subStatus.C():
		sev, ok := ev.(*status.StatusUpdatedEvent)
		require.True(t, ok)
		assert.Equal(t, "Playing G-man Bot", sev.StatusText)
	case <-time.After(2 * time.Second):
		t.Fatal("Expected immediate status update on StateEvent reconnect")
	}

	// Test manual ForceUpdate
	mgr.ForceUpdate(ctx)

	select {
	case ev := <-subStatus.C():
		sev, ok := ev.(*status.StatusUpdatedEvent)
		require.True(t, ok)
		assert.Equal(t, "Playing G-man Bot", sev.StatusText)
	case <-time.After(2 * time.Second):
		t.Fatal("Expected immediate status update on ForceUpdate")
	}
}
