// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"

	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

// BotHandler connects an Engine to the automated trade processor.
type BotHandler struct {
	engine *Engine
	logger log.Logger
}

// NewBotHandler creates a new BotHandler for the given engine.
func NewBotHandler(e *Engine, l log.Logger) *BotHandler {
	return &BotHandler{
		engine: e,
		logger: l,
	}
}

// ProcessOffer fulfills the processor.OfferHandler interface.
func (h *BotHandler) ProcessOffer(ctx context.Context, offer *trading.TradeOffer) (trading.ActionDecision, error) {
	verdict, err := h.engine.Process(ctx, offer)
	if err != nil {
		return trading.ActionDecision{Action: trading.ActionSkip}, err
	}

	return verdict.Decision(), nil
}

// OnActionFailed fulfills the processor.OfferHandler interface.
func (h *BotHandler) OnActionFailed(
	ctx context.Context,
	offer *trading.TradeOffer,
	action trading.ActionType,
	reason string,
	err error,
) {
	h.logger.Error("Trade action failed",
		log.Uint64("offer_id", offer.ID),
		log.String("action", string(action)),
		log.String("reason", reason),
		log.Err(err),
	)
}
