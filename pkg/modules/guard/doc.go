// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package guard implements the logic for handling Steam Guard Mobile Authenticator
confirmations.

This module automates the process of fetching, parsing, and acting upon mobile
confirmations, which are required for market listings and trade offers when
2FA is enabled.

# Architectural Role:

The `guard` module acts as an automated "finger" that can approve or deny
actions on behalf of the user. It polls the Steam Community website, simulates
the mobile app's cryptographic signing process, and can be configured for
automatic acceptance of specific confirmation types.

# Key Features:

  - Periodically polls for pending mobile confirmations.
  - Generates TOTP-based signatures to authorize actions (Accept/Cancel).
  - Implements configurable rate-limiting and exponential backoff to avoid API bans.
  - Provides `Accept()` and `Decline()` methods for manual control.
  - Can be configured to auto-accept specific confirmation types (e.g., market listings).
*/
package guard
