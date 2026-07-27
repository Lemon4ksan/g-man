// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trading provides a game-agnostic framework for managing, evaluating,
// and executing Steam trade offers across any Steam Economy application (CS2, Dota 2, TF2, Rust, etc.).
//
// # 1. Architecture & Offer Lifecycle
//
// The trading package models the complete transactional lifecycle of Steam trade offers (IEconService):
//
//	Incoming Offer -> Event Bus -> Queue -> Asset Lock (KeyMutex) -> Engine Pipeline -> Verdict -> Execution
//
// Offer States (OfferState):
//   - OfferStateActive (2): Offer is pending action from recipient or partner.
//   - OfferStateAccepted (3): Offer has been successfully accepted and finalized.
//   - OfferStateCountered (4): Offer was responded to with a counter-offer.
//   - OfferStateExpired (5): Offer passed its expiration deadline.
//   - OfferStateCanceled (6): Offer was canceled by the sender.
//   - OfferStateDeclined (7): Offer was explicitly declined.
//   - OfferStateInEscrow (11): Offer accepted but held in Steam trade hold (escrow).
//
// # 2. Concurrency & Race Condition Prevention
//
// To prevent "Double-Spending" vulnerabilities (where the same inventory asset is accepted or offered
// in two parallel racing trade offers), the processor enforces atomic asset locking:
//   - Before evaluating an offer, all Item AssetIDs (ItemsToGive and ItemsToReceive) are sorted and locked via KeyMutex.
//   - If any asset in an offer is currently locked by an active pipeline worker, the offer is skipped until released.
//   - Upon verdict execution (or failure), locks are automatically released using defer routines.
//
// # 3. Evaluation Engine Pipeline (Middleware Pattern)
//
// Trade offers are evaluated by passing a TradeContext through a chain of composable Middleware handlers:
//
//	type Middleware func(next Handler) Handler
//
// Middlewares inspect c.Offer and set a definitive Verdict:
//   - ActionAccept: Execute AcceptOffer and trigger mobile 2FA confirmation if required.
//   - ActionDecline: Execute DeclineOffer and send a decline notification to the partner.
//   - ActionCounter: Dispatch a counter-offer with modified Item payloads.
//   - ActionReview: Flag the offer for manual administrator intervention.
//   - ActionSkip / ActionIgnore: Leave the offer untouched for subsequent polling cycles.
//
// # 4. Game-Agnostic Asset Representation
//
// Inventory items are represented as generic Steam Economy assets (Item):
//   - Identity: Bound by AppID, ContextID, and AssetID (unique instance in inventory).
//   - Classification: ClassID (base item definition) and InstanceID (variation/condition modifier).
//   - Metadata: Descriptions, Tags, Actions, and custom Attributes are stored generically,
//     enabling seamless integration with any game economy on Steam without core SDK modifications.
package trading
