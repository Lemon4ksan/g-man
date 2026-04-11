// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	moduleRoot = "github.com/lemon4ksan/g-man"

	protoRawDir     = "./protobufs/"
	webApiJsonInput = "./api.steampowered.com.json"

	steamLangOutput = pkgRoot + "/steam/protocol/enums/enums.go"

	steamImport = moduleRoot + "/pkg/protobuf/steam"
	tf2Import   = moduleRoot + "/pkg/protobuf/tf2"

	pkgRoot      = "../../pkg"
	webApiOutput = pkgRoot + "/steam/webapi/generated.go"
	steamOut     = pkgRoot + "/protobuf/steam"
	tf2Out       = pkgRoot + "/protobuf/tf2"
)

// NOTE: Updated files should be reviewed manually because of unfixable steam junk.

func main() {
	args := strings.Join(os.Args[1:], " ")

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go [webapi] [proto] [steamlang] [format]")
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if strings.Contains(args, "webapi") {
		buildWebApi(ctx)
	}

	if strings.Contains(args, "proto") {
		buildProto(ctx)
	}

	if strings.Contains(args, "steamlang") {
		buildSteamLang(ctx)
	}

	if strings.Contains(args, "format") {
		format(ctx)
	}
}

func buildWebApi(ctx context.Context) {
	fmt.Println("🚀 Building WebAPI interfaces...")
	ensureDir(filepath.Dir(webApiOutput))
	execute(ctx, "go", []string{"run", "./webapi/main.go", webApiJsonInput, webApiOutput})
}

func buildProto(ctx context.Context) {
	fmt.Println("📦 Building Protobufs...")

	absSteamOut, _ := filepath.Abs(steamOut)
	absTf2Out, _ := filepath.Abs(tf2Out)
	absSteamSrc, _ := filepath.Abs(filepath.Join(protoRawDir, "steam"))
	absTf2Src, _ := filepath.Abs(filepath.Join(protoRawDir, "tf2"))

	execute(ctx, "go", []string{
		"run", "./proto/main.go",
		"-steam_src", absSteamSrc,
		"-tf2_src", absTf2Src,
		"-steam_out", absSteamOut,
		"-tf2_out", absTf2Out,
		"-steam_import", steamImport,
		"-tf2_import", tf2Import,
	})
}

func buildSteamLang(ctx context.Context) {
	fmt.Println("📜 Building SteamLanguage enums...")

	absOutput, _ := filepath.Abs(steamLangOutput)

	execute(ctx, "go", []string{
		"run", "./steamlang/main.go",
		"-out", absOutput,
		"-pkg", "protocol",
	})
}

func format(ctx context.Context) {
	fmt.Println("🧹 Formatting...")
	execute(ctx, "go", []string{"fmt", pkgRoot + "/..."})
}

func ensureDir(path string) {
	if !fileExists(path) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			fmt.Printf("Failed to create directory %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}

	if errors.Is(err, os.ErrNotExist) {
		return false
	}

	return false
}

func execute(ctx context.Context, name string, args []string) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Command failed: %v\n", err)
		os.Exit(1)
	}
}
