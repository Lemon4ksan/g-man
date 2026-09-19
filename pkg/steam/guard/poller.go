// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/silicon/clock"

	"github.com/lemon4ksan/g-man/internal/crypto"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// PollerConfig configures the event-driven adaptive Steam Guard confirmation poller.
type PollerConfig struct {
	DeviceID       string
	SteamID        id.ID
	IdentitySecret string
	IdleInterval   time.Duration
	BurstInterval  time.Duration
	BurstDuration  time.Duration
	AutoConfirmAll bool
	OnConfirmation func(conf *Confirmation) bool // Return true to accept, false to skip/ignore
}

// ConfirmationPoller orchestrates event-driven adaptive polling of mobile confirmations.
type ConfirmationPoller struct {
	mobileConf *MobileConf
	cfg        PollerConfig
	triggerCh  chan struct{}
	stopCh     chan struct{}
	mu         sync.Mutex
	running    bool
}

// NewPoller creates an adaptive 2FA confirmation poller.
func NewPoller(mobileConf *MobileConf, cfg PollerConfig) *ConfirmationPoller {
	if cfg.IdleInterval <= 0 {
		cfg.IdleInterval = 30 * time.Second
	}

	if cfg.BurstInterval <= 0 {
		cfg.BurstInterval = 2 * time.Second
	}

	if cfg.BurstDuration <= 0 {
		cfg.BurstDuration = 30 * time.Second
	}

	return &ConfirmationPoller{
		mobileConf: mobileConf,
		cfg:        cfg,
		triggerCh:  make(chan struct{}, 8),
		stopCh:     make(chan struct{}),
	}
}

// Trigger proactively wakes up the poller into high-frequency burst mode (e.g. after sending a trade offer).
func (p *ConfirmationPoller) Trigger() {
	select {
	case p.triggerCh <- struct{}{}:
	default:
	}
}

// PollOnce executes an immediate check and processes pending confirmations.
func (p *ConfirmationPoller) PollOnce(ctx context.Context) ([]*Confirmation, error) {
	now := clock.CoarseTime().Unix()
	confKeyArr := crypto.GenerateConfirmationKey([]byte(p.cfg.IdentitySecret), now, "conf")
	confKey := string(confKeyArr[:])

	list, err := p.mobileConf.GetConfirmations(ctx, p.cfg.DeviceID, p.cfg.SteamID, confKey, now)
	if err != nil {
		return nil, err
	}

	if list == nil || len(list.Confirmations) == 0 {
		return nil, nil
	}

	confs := list.Confirmations

	var toAccept []*Confirmation
	for _, c := range confs {
		shouldAccept := p.cfg.AutoConfirmAll
		if p.cfg.OnConfirmation != nil {
			shouldAccept = p.cfg.OnConfirmation(c)
		}

		if shouldAccept {
			toAccept = append(toAccept, c)
		}
	}

	if len(toAccept) > 0 {
		actTime := clock.CoarseTime().Unix()
		actKeyArr := crypto.GenerateConfirmationKey([]byte(p.cfg.IdentitySecret), actTime, "accept")
		actKey := string(actKeyArr[:])
		_ = p.mobileConf.RespondToMultiple(ctx, toAccept, true, p.cfg.DeviceID, p.cfg.SteamID, actKey, actTime)
	}

	return confs, nil
}

// Start launches the background polling loop in a separate goroutine.
func (p *ConfirmationPoller) Start(ctx context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}

	p.running = true
	p.mu.Unlock()

	go p.loop(ctx)
}

// Stop terminates the polling loop.
func (p *ConfirmationPoller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return
	}

	p.running = false
	close(p.stopCh)
}

func (p *ConfirmationPoller) loop(ctx context.Context) {
	idleTicker := time.NewTicker(p.cfg.IdleInterval)
	defer idleTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-idleTicker.C:
			_, _ = p.PollOnce(ctx)
		case <-p.triggerCh:
			p.runBurst(ctx)
		}
	}
}

func (p *ConfirmationPoller) runBurst(ctx context.Context) {
	burstDeadline := clock.CoarseTime().Add(p.cfg.BurstDuration)

	burstTicker := time.NewTicker(p.cfg.BurstInterval)
	defer burstTicker.Stop()

	// Initial immediate check
	_, _ = p.PollOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-burstTicker.C:
			if clock.CoarseTime().After(burstDeadline) {
				return
			}

			confs, _ := p.PollOnce(ctx)
			if len(confs) > 0 {
				// Successfully resolved confirmations during burst
				return
			}
		}
	}
}
