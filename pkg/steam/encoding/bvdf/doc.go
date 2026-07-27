// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bvdf implements a high-performance parser for Valve Proprietary Binary KeyValues (BVDF) format.
//
// # 1. Binary KeyValues vs Text VDF
//
// While human-readable configuration files use text VDF, Steam stores cached local metadata
// (e.g. appinfo.vdf and packageinfo.vdf) in a compact binary format to optimize disk I/O and parsing performance.
//
// # 2. Magic Signatures & Header Formats
//
// The parser automatically detects file header signatures:
//
//   - AppInfo Format:
//
//   - Magic 0x07564428 (AppInfo V40): Fixed header + raw app binary blocks.
//
//   - Magic 0x07564429 (AppInfo V41): Includes string table offset for key deduplication.
//
//   - PackageInfo Format:
//
//   - Magic 0x06565527 (PackageInfo V39): Standard package header with SHA1 hashes.
//
//   - Magic 0x06565528 (PackageInfo V40): Includes token metadata fields.
//
// # 3. Type Marker Byte Reference
//
// Values in BVDF streams are prefixed by a single type marker byte:
//   - 0x00 (Type Map): Nested child object dictionary.
//   - 0x01 (Type String): Null-terminated UTF-8 string.
//   - 0x02 (Type Int32): 32-bit signed little-endian integer.
//   - 0x03 (Type Float32): 32-bit IEEE 754 floating point number.
//   - 0x07 (Type UInt64): 64-bit unsigned little-endian integer.
//   - 0x08 (Type End): End marker for child dictionary blocks.
package bvdf
