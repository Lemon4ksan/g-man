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
	_ = os.MkdirAll(*steamOut, 0o755)

	files, _ := filepath.Glob(filepath.Join(*steamSrc, "*.proto"))

	fileNames := make([]string, 0, len(files))
	mappings := make([]string, 0, len(files))

	for _, f := range files {
		base := filepath.Base(f)
		mappings = append(mappings, "--go_opt=M"+base+"="+*steamImport)
		fileNames = append(fileNames, base)
	}

	args := append([]string{
		"-I=" + *steamSrc,
		"--go_out=" + *steamOut,
		"--go_opt=paths=source_relative",
	}, append(mappings, fileNames...)...)

	execute(ctx, *steamSrc, "protoc", args)
}

func buildTF2(ctx context.Context) {
	fmt.Println("📦 Building TF2 Protobufs (with sanitization)...")

	tempDir, _ := os.MkdirTemp("", "tf2proto_build")
	defer os.RemoveAll(tempDir)

	tf2Sandbox := filepath.Join(tempDir, "tf2_gc")
	steamSandbox := filepath.Join(tempDir, "steam")

	_ = os.MkdirAll(tf2Sandbox, 0o755)
	_ = os.MkdirAll(steamSandbox, 0o755)

	// TF2 protobuffs import files like steammessages.proto.
	// The compiler needs to see them in the same file structure.
	steamFiles, _ := filepath.Glob(filepath.Join(*steamSrc, "*.proto"))
	for _, f := range steamFiles {
		copyFile(f, filepath.Join(steamSandbox, filepath.Base(f)))
	}

	tf2Files, _ := filepath.Glob(filepath.Join(*tf2Src, "*.proto"))
	for _, f := range tf2Files {
		dst := filepath.Join(tf2Sandbox, filepath.Base(f))
		// Valve doesn't always specify packages, or specifies them incorrectly.
		// We force 'package tf2_gc' for isolation.
		copySanitizeTF2(f, dst, "tf2_gc")
	}

	_ = os.MkdirAll(*tf2Out, 0o755)

	// If one .proto file imports another, protoc-gen-go doesn't know
	// which Go path (import path) contains the generated code for that file.
	// The Mfilename=import_path option tells the generator:
	// "If you see an import for file X, assume it's in package Y."
	var mappings []string
	for _, f := range steamFiles {
		mappings = append(mappings, "--go_opt=M"+filepath.Base(f)+"="+*steamImport)
	}

	for _, f := range tf2Files {
		base := filepath.Base(f)
		// We map both the short name and the name with the directory prefix
		mappings = append(mappings, "--go_opt=Mtf2_gc/"+base+"="+*tf2Import)
		mappings = append(mappings, "--go_opt=M"+base+"="+*tf2Import)
	}

	// Some files in the Valve repositories duplicate core Steam messages.
	// Compiling them within the TF2 package will result in a conflict
	// of duplicate data types in Go. We'll skip these.
	blacklist := map[string]bool{
		"steammessages.proto":              true,
		"steammessages_base.proto":         true,
		"steammessages_unified_base.proto": true,
		"enums_clientserver.proto":         true,
	}

	for _, f := range tf2Files {
		base := filepath.Base(f)
		if blacklist[base] {
			continue
		}

		fmt.Printf("  > Compiling: %s\n", base)
		// Run protoc from the root of the temporary folder so that the -I (include) paths
		// match the import structure in the .proto files.
		execute(ctx, tempDir, "protoc", append([]string{
			"-I=.",      // Sandbox root
			"-I=tf2_gc", // For TF2 imports
			"-I=steam",  // For Steam imports
			"--go_out=" + *tf2Out,
			"--go_opt=paths=source_relative",
		}, append(mappings, filepath.Join("tf2_gc", base))...))
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

func copySanitizeTF2(src, dst, newPkg string) {
	data, _ := os.ReadFile(src)
	content := string(data)

	// Valve often forgets 'package' or uses different names.
	// We're unifying them so all TF2 GC messages end up in a single Go package.
	rePkg := regexp.MustCompile(`(?m)^\s*package\s+[^;]+;`)
	if rePkg.MatchString(content) {
		content = rePkg.ReplaceAllString(content, "package "+newPkg+";")
	} else {
		content = "package " + newPkg + ";\n\n" + content
	}

	// Steam .proto files often write the type as '.CMsgBase'.
	// Modern protoc-gen-go treats leading dots as "absolute paths
	// in the root namespace," which breaks Go generation (it looks for the '.' package).
	// We convert '.CMsgBase' to 'CMsgBase' so that the search occurs within the package.
	// Exception: google.protobuf types (e.g., .google.protobuf.DescriptorProto).
	reLeadingDot := regexp.MustCompile(`([\s\(\<])\.([a-zA-Z])`)
	content = reLeadingDot.ReplaceAllStringFunc(content, func(m string) string {
		if strings.Contains(m, ".google") {
			return m
		}

		return string(m[0]) + string(m[2:])
	})

	_ = os.WriteFile(dst, []byte(content), 0o644)
}
