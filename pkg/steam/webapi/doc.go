// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate go run ../../../cmd/generator/webapi -in=../../../cmd/generator/api.steampowered.com.json -out=api.go

// Package webapi contains strictly typed, zero-allocation clients for the Steam Web API,
// compiled via aoni-gen from the Valve GetSupportedAPIList schema.
package webapi
