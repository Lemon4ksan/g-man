// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package crypto implements Steam-specific cryptographic algorithms, session key exchanges,
// custom AES-256-CBC with derived HMAC initialization vectors, and Steam Guard TOTP key generation.
//
// # 1. RSA-OAEP Session Handshake
//
// During channel encryption setup, the client generates a 32-byte cryptographically secure random AES key
// and appends a 16-byte server nonce. This 48-byte buffer is encrypted using Steam's public RSA key (system.pem)
// with OAEP padding (SHA-1 digest).
//
// # 2. Custom AES-256-CBC with Derived HMAC IV
//
// Standard Steam symmetric transport encryption uses AES-256-CBC, but derives its Initialization Vector (IV)
// dynamically from the plaintext payload to guarantee integrity:
//
//  1. Generate 3 random bytes: prefix = Random(3)
//  2. Calculate HMAC-SHA1 signature: MAC = HMAC-SHA1(Key[:16], prefix + Plaintext)
//  3. Construct IV (16 bytes): IV = MAC[:13] + prefix
//  4. Encrypt Plaintext using AES-256-CBC with IV.
//  5. Encrypt IV using AES-256-ECB with Key.
//  6. Prepend encrypted IV to ciphertext.
//
// # 3. Steam Guard TOTP Algorithm (Custom RFC 6238 Variant)
//
// Steam Guard 2FA codes diverge from standard RFC 6238 TOTP in two key aspects:
//
//  1. Time Slice: Calculated over 30-second epochs (t = timestamp / 30).
//
//  2. Custom Alphabet: Uses a truncated 26-character character set to prevent visual ambiguity:
//
//     Alphabet: "23456789BCDFGHJKMNPQRTVWXY"
//     (Excluded ambiguous characters: 0, 1, A, E, I, O, U).
//
// The 5-character code is extracted by taking modulo operations of fullCode % 26.
package crypto
