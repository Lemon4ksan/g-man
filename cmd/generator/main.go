// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	moduleRoot = "github.com/lemon4ksan/g-man"

	protoRawDir     = "./protobufs/"
	webApiJsonInput = "./api.steampowered.com.json"

	steamLangOutput = pkgRoot + "/steam/protocol/enums.go"

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

	if strings.Contains(args, "webapi") {
		buildWebApi()
	}

	if strings.Contains(args, "proto") {
		buildProto()
	}

	if strings.Contains(args, "steamlang") {
		buildSteamLang()
	}

	if strings.Contains(args, "format") {
		format()
	}
}

func buildWebApi() {
	fmt.Println("🚀 Building WebAPI interfaces...")
	ensureDir(filepath.Dir(webApiOutput))
	execute("go", []string{"run", "./webapi/main.go", webApiJsonInput, webApiOutput})
}

func buildProto() {
	fmt.Println("📦 Building Protobufs...")

	absSteamOut, _ := filepath.Abs(steamOut)
	absTf2Out, _ := filepath.Abs(tf2Out)
	absSteamSrc, _ := filepath.Abs(filepath.Join(protoRawDir, "steam"))
	absTf2Src, _ := filepath.Abs(filepath.Join(protoRawDir, "tf2"))

	execute("go", []string{
		"run", "./proto/main.go",
		"-steam_src", absSteamSrc,
		"-tf2_src", absTf2Src,
		"-steam_out", absSteamOut,
		"-tf2_out", absTf2Out,
		"-steam_import", steamImport,
		"-tf2_import", tf2Import,
	})
}

func buildSteamLang() {
	fmt.Println("📜 Building SteamLanguage enums...")
	absOutput, _ := filepath.Abs(steamLangOutput)

	execute("go", []string{
		"run", "./steamlang/main.go",
		"-out", absOutput,
		"-pkg", "protocol",
	})
}

func format() {
	fmt.Println("🧹 Formatting...")
	execute("go", []string{"fmt", pkgRoot + "/..."})
}

func ensureDir(path string) {
	if !fileExists(path) {
		if err := os.MkdirAll(path, 0755); err != nil {
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

func execute(name string, args []string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Command failed: %v\n", err)
		os.Exit(1)
	}
}
