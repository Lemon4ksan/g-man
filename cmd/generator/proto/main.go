// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

var (
	steamSrc    = flag.String("steam_src", "", "Path to raw Steam proto files")
	tf2Src      = flag.String("tf2_src", "", "Path to raw TF2 proto files")
	steamOut    = flag.String("steam_out", "", "Output path for Steam Go files")
	tf2Out      = flag.String("tf2_out", "", "Output path for TF2 Go files")
	steamImport = flag.String("steam_import", "", "Go import path for Steam package")
	tf2Import   = flag.String("tf2_import", "", "Go import path for TF2 package")
)

func main() {
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if *steamSrc != "" && *steamOut != "" {
		buildSteam(ctx)
	}

	if *tf2Src != "" && *tf2Out != "" {
		buildTF2(ctx)
	}
}

func buildSteam(ctx context.Context) {
	fmt.Println("📦 Building Steam Protobufs (+vtprotobuf BLAZING speed)...")

	tempDir, _ := os.MkdirTemp("", "steamproto_build")
	defer os.RemoveAll(tempDir)

	steamSandbox := filepath.Join(tempDir, "steam")
	_ = os.MkdirAll(steamSandbox, 0o755)

	files, _ := filepath.Glob(filepath.Join(*steamSrc, "*.proto"))

	for _, f := range files {
		dst := filepath.Join(steamSandbox, filepath.Base(f))
		copySanitizeProto(f, dst, "steam", *steamImport)
	}

	findAndCopyGoogleProto(*steamSrc, tempDir)

	_ = os.MkdirAll(*steamOut, 0o755)
	absSteamOut, err := filepath.Abs(*steamOut)
	if err != nil {
		absSteamOut = *steamOut
	}
	absSteamOut = filepath.ToSlash(absSteamOut)

	fileNames := make([]string, 0, len(files))
	var mappings []string

	for _, f := range files {
		base := filepath.Base(f)
		fileNames = append(fileNames, base)

		mappings = append(mappings, "--go_opt=M"+base+"="+*steamImport)
		mappings = append(mappings, "--go_opt=Msteam/"+base+"="+*steamImport)
		mappings = append(mappings, "--go-vtproto_opt=M"+base+"="+*steamImport)
		mappings = append(mappings, "--go-vtproto_opt=Msteam/"+base+"="+*steamImport)
	}

	baseArgs := []string{
		"-I=.",
		"-I=..",
		"--go_out=" + absSteamOut,
		"--go_opt=paths=source_relative",
		"--go-vtproto_out=" + absSteamOut,
		"--go-vtproto_opt=paths=source_relative",
		"--go-vtproto_opt=features=marshal+unmarshal+size+pool",
	}

	allArgs := append(append(baseArgs, mappings...), fileNames...)
	executeWithResponseFile(ctx, steamSandbox, allArgs)
}

func buildTF2(ctx context.Context) {
	fmt.Println("📦 Building TF2 Protobufs (Standard Stable Go)...")

	tempDir, _ := os.MkdirTemp("", "tf2proto_build")
	defer os.RemoveAll(tempDir)

	tf2Sandbox := filepath.Join(tempDir, "tf2_gc")
	steamSandbox := filepath.Join(tempDir, "steam")

	_ = os.MkdirAll(tf2Sandbox, 0o755)
	_ = os.MkdirAll(steamSandbox, 0o755)

	steamFiles, _ := filepath.Glob(filepath.Join(*steamSrc, "*.proto"))
	for _, f := range steamFiles {
		copySanitizeProto(f, filepath.Join(steamSandbox, filepath.Base(f)), "steam", *steamImport)
	}

	findAndCopyGoogleProto(*steamSrc, tempDir)

	tf2Files, _ := filepath.Glob(filepath.Join(*tf2Src, "*.proto"))
	for _, f := range tf2Files {
		dst := filepath.Join(tf2Sandbox, filepath.Base(f))
		copySanitizeProto(f, dst, "tf2_gc", *tf2Import)
	}

	_ = os.MkdirAll(*tf2Out, 0o755)
	absTF2Out, err := filepath.Abs(*tf2Out)
	if err != nil {
		absTF2Out = *tf2Out
	}
	absTF2Out = filepath.ToSlash(absTF2Out)

	blacklist := map[string]bool{
		"steammessages.proto":              true,
		"steammessages_base.proto":         true,
		"steammessages_unified_base.proto": true,
		"enums_clientserver.proto":         true,
	}

	var tf2ToCompile []string
	for _, f := range tf2Files {
		base := filepath.Base(f)
		if blacklist[base] {
			continue
		}
		tf2ToCompile = append(tf2ToCompile, base)
	}

	var mappings []string
	for _, f := range steamFiles {
		base := filepath.Base(f)
		mappings = append(mappings, "--go_opt=M"+base+"="+*steamImport)
		mappings = append(mappings, "--go_opt=Msteam/"+base+"="+*steamImport)
	}

	for _, f := range tf2Files {
		base := filepath.Base(f)
		mappings = append(mappings, "--go_opt=Mtf2_gc/"+base+"="+*tf2Import)
		mappings = append(mappings, "--go_opt=M"+base+"="+*tf2Import)
	}

	baseArgs := []string{
		"-I=.",
		"-I=../steam",
		"-I=..",
		"--go_out=" + absTF2Out,
		"--go_opt=paths=source_relative",
	}

	allArgs := append(append(baseArgs, mappings...), tf2ToCompile...)
	executeWithResponseFile(ctx, tf2Sandbox, allArgs)
}

func executeWithResponseFile(ctx context.Context, dir string, args []string) {
	respFile, err := os.CreateTemp("", "protoc_args_*.txt")
	if err != nil {
		fmt.Printf("Failed to create response file: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(respFile.Name())

	var sb strings.Builder
	for _, arg := range args {
		formattedArg := filepath.ToSlash(arg)
		if strings.Contains(formattedArg, " ") {
			sb.WriteString(`"` + strings.ReplaceAll(formattedArg, `\`, `\\`) + `"`)
		} else {
			sb.WriteString(formattedArg)
		}
		sb.WriteString("\n")
	}

	if err := os.WriteFile(respFile.Name(), []byte(sb.String()), 0o644); err != nil {
		fmt.Printf("Failed to write response file: %v\n", err)
		os.Exit(1)
	}
	_ = respFile.Close()

	execute(ctx, dir, "protoc", []string{"@" + respFile.Name()})
}

func findAndCopyGoogleProto(steamSrc string, tempDir string) {
	candidates := []string{
		filepath.Join(steamSrc, "google"),
		filepath.Join(filepath.Dir(steamSrc), "google"),
		filepath.Dir(filepath.Dir(steamSrc)),
	}

	for _, cand := range candidates {
		gDir := cand
		if !strings.HasSuffix(cand, "google") {
			gDir = filepath.Join(cand, "google")
		}
		if info, err := os.Stat(gDir); err == nil && info.IsDir() {
			copyDir(gDir, filepath.Join(tempDir, "google"))
			return
		}
	}
}

func execute(ctx context.Context, dir, command string, args []string) {
	cmd := exec.CommandContext(ctx, command, args...)

	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Error in %s: %v\n%s\n", dir, err, string(out))
		os.Exit(1)
	}
}

func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		fmt.Printf("Failed to read %s: %v\n", src, err)
		os.Exit(1)
	}

	err = os.WriteFile(dst, data, 0o644)
	if err != nil {
		fmt.Printf("Failed to write %s: %v\n", dst, err)
		os.Exit(1)
	}
}

func copyDir(src, dst string) {
	_ = os.MkdirAll(dst, 0o755)
	entries, _ := os.ReadDir(src)

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			copyDir(srcPath, dstPath)
		} else {
			copyFile(srcPath, dstPath)
		}
	}
}

func copySanitizeProto(src, dst, overridePackage, goPackageImport string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}

	content := string(data)

	if overridePackage != "" {
		rePkg := regexp.MustCompile(`(?m)^\s*package\s+[^;]+;`)
		if rePkg.MatchString(content) {
			content = rePkg.ReplaceAllString(content, "package "+overridePackage+";")
		} else {
			content = "package " + overridePackage + ";\n\n" + content
		}
	}

	if goPackageImport != "" {
		reGoPkg := regexp.MustCompile(`(?m)^\s*option\s+go_package\s*=\s*[^;]+;`)
		goPkgOption := fmt.Sprintf(`option go_package = "%s";`, goPackageImport)
		if reGoPkg.MatchString(content) {
			content = reGoPkg.ReplaceAllString(content, goPkgOption)
		} else {
			content = goPkgOption + "\n" + content
		}
	}

	reLeadingDot := regexp.MustCompile(`([\s\(\<])\.([a-zA-Z])`)
	content = reLeadingDot.ReplaceAllStringFunc(content, func(m string) string {
		if strings.Contains(m, ".google") {
			return m
		}

		return string(m[0]) + string(m[2:])
	})

	_ = os.WriteFile(dst, []byte(content), 0o644)
}
