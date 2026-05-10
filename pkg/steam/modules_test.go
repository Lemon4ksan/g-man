// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

func TestModuleManager_Register(t *testing.T) {
	state := &atomic.Int32{}
	state.Store(int32(StateRunning))

	mm := &ModuleManager{
		modules: make(map[string]module.Module),
		state:   state,
	}

	m := new(mockModule)
	m.On("Name").Return("test_mod")
	m.On("Init", mock.Anything).Return(nil).Once()
	m.On("Start", mock.Anything).Return(nil).Once()

	err := mm.Register(context.Background(), m)
	assert.NoError(t, err)
	m.AssertExpectations(t)

	// Check retrieval
	retrieved := mm.Get("test_mod")
	assert.Equal(t, m, retrieved)
}

func TestModuleManager_AddAndGet(t *testing.T) {
	mm := &ModuleManager{modules: make(map[string]module.Module)}

	mod := new(mockModule)
	mod.On("Name").Return("test")

	mm.Add(mod)
	assert.Equal(t, mod, mm.Get("test"))
	assert.Nil(t, mm.Get("unknown"))
}

func TestModuleManager_Register_Errors(t *testing.T) {
	state := &atomic.Int32{}
	mm := &ModuleManager{
		modules: make(map[string]module.Module),
		state:   state,
	}

	t.Run("Duplicate", func(t *testing.T) {
		mod := new(mockModule)
		mod.On("Name").Return("dup")
		mm.Add(mod)
		err := mm.Register(context.Background(), mod)
		assert.ErrorContains(t, err, "already registered")
	})

	t.Run("Init Fail", func(t *testing.T) {
		state.Store(int32(StateRunning))

		mod := new(mockModule)
		mod.On("Name").Return("init_fail")
		mod.On("Init", mock.Anything).Return(errors.New("err init")).Once()
		err := mm.Register(context.Background(), mod)
		assert.ErrorContains(t, err, "err init")
	})

	t.Run("Start Fail", func(t *testing.T) {
		mod := new(mockModule)
		mod.On("Name").Return("start_fail")
		mod.On("Init", mock.Anything).Return(nil).Once()
		mod.On("Start", mock.Anything).Return(errors.New("err start")).Once()
		err := mm.Register(context.Background(), mod)
		assert.ErrorContains(t, err, "err start")
	})

	t.Run("StartAuthed Fail", func(t *testing.T) {
		state.Store(int32(StateAuthorized))

		mod := new(mockAuthModule)
		mod.On("Name").Return("auth_fail")
		mod.On("Init", mock.Anything).Return(nil).Once()
		mod.On("Start", mock.Anything).Return(nil).Once()
		mod.On("StartAuthed", mock.Anything, mock.Anything).Return(errors.New("err authed")).Once()

		err := mm.Register(context.Background(), mod)
		assert.ErrorContains(t, err, "err authed")
	})
}

func TestModuleManager_AllOps(t *testing.T) {
	mm := &ModuleManager{modules: make(map[string]module.Module)}

	mod1 := new(mockModule)
	mod1.On("Name").Return("m1")
	mod1.On("Init", mock.Anything).Return(errors.New("init err")).Once()
	mod1.On("Start", mock.Anything).Return(errors.New("start err")).Once()

	mod2 := new(mockAuthModule)
	mod2.On("Name").Return("m2")
	mod2.On("Init", mock.Anything).Return(errors.New("init err")).Once()
	mod2.On("Start", mock.Anything).Return(errors.New("start err")).Once()
	mod2.On("StartAuthed", mock.Anything, mock.Anything).Return(errors.New("auth err")).Once()

	mm.Add(mod1)
	mm.Add(mod2)

	err := mm.InitAll(nil)
	assert.ErrorContains(t, err, "init err")

	err = mm.StartAll(context.Background())
	assert.ErrorContains(t, err, "start err")

	err = mm.StartAuthedAll(context.Background(), nil)
	assert.ErrorContains(t, err, "auth err")
}
