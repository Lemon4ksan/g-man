// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package processor acts as the central orchestrator for the trade management
subsystem. It glues together the engine, notifications, and review packages
to provide a seamless trade processing pipeline.

# Lifecycle of a Trade Offer

The Processor manages the complete flow of an incoming 'trading.TradeOffer':
 1. Sequential Queueing: Offers are processed one-by-one to prevent inventory
    desynchronization and race conditions.
 2. Asset Locking: Prevents "double-spending" by tracking asset IDs currently
    occupied in active processing cycles.
 3. Decision Making: Invokes the 'engine' to determine the correct action.
 4. Execution: Calls the Steam API to accept or decline the offer.
 5. Feedback Loop: Sends chat notifications to the partner and alerts the
    admin if intervention is required.

# Stability and Performance

By utilizing internal channels and background workers, the Processor ensures
that the bot remains responsive even under heavy trade volume. It also includes
concurrency primitives to safely manage shared state across different stages
of the pipeline.
*/
package processor
