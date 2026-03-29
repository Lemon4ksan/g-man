// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package offers manages real-time "live trade" invitations via the Steam
Connection Manager (CM).

Unlike the `trading` module which polls for asynchronous trade offers, this
module handles the immediate, pop-up style trade requests that occur when
two users are online and agree to trade live.

# Key Features:

  - Send live trade invitations to other users (`Invite`).
  - Listen for incoming trade proposals (`TradeProposedEvent`).
  - Programmatically accept or decline incoming requests.
  - Publishes events for each stage of the live trade lifecycle
    (`TradeProposedEvent`, `TradeResultEvent`, `TradeSessionStartedEvent`).

# Distinction from `trading`:
  - `offers`: For real-time, synchronous trades. Uses the CM socket.
  - `trading`: For asynchronous, persistent trade offers. Uses the WebAPI.
*/
package offers
