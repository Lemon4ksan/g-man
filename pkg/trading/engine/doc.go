// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package engine implements a high-performance, middleware-based reasoning system
for Steam trade offers. It follows the "Chain of Responsibility" (Onion) pattern,
similar to modern web frameworks.

# The reasoning process

A trade offer is wrapped in a 'TradeContext' and passed through a series of
independent 'Middleware' functions. Each middleware can:
  - Enrich the context with metadata (e.g., fetch item prices or user reputation).
  - Modify the final 'Verdict' (e.g., set to DECLINE because of a blacklist).
  - Break the chain early (short-circuit) if a definitive decision is reached.
  - Execute logic after the rest of the chain has finished (e.g., for logging).

# Modularity and Isolation

The Engine allows developers to separate complex business rules into small,
testable units. For example, a 'PricingMiddleware' doesn't need to know about
a 'BlacklistMiddleware'. They only interact through the shared metadata in
the TradeContext.

# Verdicts and Actions

The end result of an engine run is a 'Verdict', which consists of an 'Action'
(ACCEPT, DECLINE, COUNTER, REVIEW, or IGNORE) and a string 'Reason' indicating
why that decision was made.
*/
package engine
