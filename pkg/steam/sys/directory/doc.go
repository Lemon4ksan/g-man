// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package directory provides a client for the Steam ISteamDirectory WebAPI.
// It discovers and balances the load of Steam Connection Manager (CM) servers,
// filtering endpoints to establish optimal socket connections.
//
// The primary orchestrator is [Service], which is initialized using [New] with a [service.Doer] client.
// It queries the WebAPI using configurations defined in [CMCfg] to return lists of [CMServer] endpoints.
//
// Basic usage example:
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//
//		"github.com/lemon4ksan/g-man/pkg/steam"
//		"github.com/lemon4ksan/g-man/pkg/steam/directory"
//		"github.com/lemon4ksan/g-man/pkg/steam/service"
//	)
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Build standard client config
//		clientCfg := steam.DefaultConfig()
//		client, err := steam.NewClient(clientCfg, steam.WithLogger(logger))
//		if err != nil {
//			fmt.Println("Failed to create client:", err)
//			return
//		}
//		defer client.Close()
//
//		ds := directory.New(client)
//
//		optimal, err := ds.GetOptimalCMServer(context.Background())
//		if err != nil {
//			panic(err)
//		}
//		fmt.Println("Optimal CM Server Endpoint:", optimal.Endpoint)
//	}
package directory
