// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package status provides a subsystem for managing dynamic Steam presence, multi-game idling, and event-driven status flashes.
package status

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/trading/web"
)

var (
	ErrAppsModuleMissing = errors.New("status: required apps module dependency is missing")
	ErrIntervalTooShort  = errors.New("status: update interval must be at least 3 seconds")
)

const (
	ModuleName        = "status"
	MinUpdateInterval = 3 * time.Second
	MaxStatusLength   = 128
)

// WithModule registers the Status manager module in the Steam client orchestrator.
func WithModule(opts ...Option) steam.Option {
	return steam.WithModule(NewManager(opts...))
}

// From retrieves the Status manager module instance from the Steam client.
func From(c *steam.Client) *Manager {
	return steam.GetModule[*Manager](c)
}

// Config configures status update intervals, marquee parameters, and event triggers.
type Config struct {
	UpdateInterval    time.Duration
	Slides            []string
	MarqueeText       string
	MarqueeWidth      int
	IdleAppIDs        []uint32
	FlashDuration     time.Duration
	AutoFlashOnOffers bool
}

// Option configures a Manager instance.
type Option = generic.Option[*Manager]

// WithUpdateInterval configures status update frequency, enforcing a 3-second minimum to prevent Steam rate limits.
func WithUpdateInterval(d time.Duration) Option {
	return func(m *Manager) {
		if d >= MinUpdateInterval {
			m.config.UpdateInterval = d
		}
	}
}

// WithSlides sets a list of status strings rotated cyclically on each ticker tick.
func WithSlides(slides ...string) Option {
	return func(m *Manager) {
		m.config.Slides = slides
	}
}

// WithMarquee configures a scrolling text banner with a fixed window width.
func WithMarquee(text string, width int) Option {
	return func(m *Manager) {
		m.config.MarqueeText = text
		if width > 0 {
			m.config.MarqueeWidth = width
		}
	}
}

// WithIdleAppIDs configures real Steam AppIDs to play simultaneously with the custom status.
func WithIdleAppIDs(appIDs ...uint32) Option {
	return func(m *Manager) {
		m.config.IdleAppIDs = appIDs
	}
}

// WithAutoFlashOnOffers toggles temporary status flashes when new trade offers arrive.
func WithAutoFlashOnOffers(enabled bool) Option {
	return func(m *Manager) {
		m.config.AutoFlashOnOffers = enabled
	}
}

// Manager orchestrates dynamic Non-Steam status messages, game idling, and event-driven status overrides.
//
// Thread Safety:
//   - Fully thread-safe across all public methods.
type Manager struct {
	module.AuthBase

	config  Config
	apps    *apps.Apps
	service service.Doer

	stateMu          sync.RWMutex
	flashText        string
	activeFlashUntil time.Time
	dynamicProvider  func(ctx context.Context) string
}

// NewManager constructs a Manager instance with functional options applied.
func NewManager(opts ...Option) *Manager {
	m := &Manager{
		AuthBase: module.NewAuthBase(ModuleName),
		config: Config{
			UpdateInterval:    5 * time.Second,
			MarqueeWidth:      24,
			FlashDuration:     10 * time.Second,
			AutoFlashOnOffers: true,
		},
	}

	generic.ApplyOptions(m, opts...)

	return m
}

// Init resolves required client module dependencies.
func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.service = init.Service()

	appsMod, err := module.Get[*apps.Apps](init, apps.ModuleName)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAppsModuleMissing, err)
	}

	m.apps = appsMod

	return nil
}

// StartAuthed subscribes to system event channels and launches background status workers.
func (m *Manager) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	if err := m.AuthBase.StartAuthed(ctx, authCtx); err != nil {
		return err
	}

	if m.config.AutoFlashOnOffers {
		sub := m.Bus.Subscribe(&web.NewOfferEvent{})
		m.Go(func(workerCtx context.Context) {
			m.listenEvents(workerCtx, sub)
		})
	}

	m.Go(m.statusLoop)

	return nil
}

// SetDynamicProvider registers a closure evaluated on every update tick to compute dynamic status strings.
func (m *Manager) SetDynamicProvider(provider func(ctx context.Context) string) {
	m.stateMu.Lock()
	m.dynamicProvider = provider
	m.stateMu.Unlock()
}

// FlashStatus temporarily overrides the active status with a high-priority notification message.
func (m *Manager) FlashStatus(message string, duration time.Duration) {
	if message == "" {
		return
	}

	m.stateMu.Lock()
	m.flashText = message
	m.activeFlashUntil = time.Now().Add(duration)
	m.stateMu.Unlock()

	m.Logger.Debug("Status flash triggered", log.String("message", message))
}

// ForceUpdate triggers an immediate status refresh on Steam.
func (m *Manager) ForceUpdate(ctx context.Context) {
	frameIdx := 0
	marqueeOffset := 0

	statusText := m.nextStatusText(ctx, &frameIdx, &marqueeOffset)
	if statusText != "" || len(m.config.IdleAppIDs) > 0 {
		if len(statusText) > MaxStatusLength {
			statusText = statusText[:MaxStatusLength]
		}

		m.updateSteamStatus(ctx, statusText)
	}
}

func (m *Manager) statusLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.UpdateInterval)
	defer ticker.Stop()

	frameIdx := 0
	marqueeOffset := 0

	subLoggedOn := m.Bus.Subscribe(&auth.LoggedOnEvent{})
	defer subLoggedOn.Unsubscribe()

	subState := m.Bus.Subscribe(&auth.StateEvent{})
	defer subState.Unsubscribe()

	// Perform initial immediate status push on loop start / reconnect
	statusText := m.nextStatusText(ctx, &frameIdx, &marqueeOffset)
	if statusText != "" || len(m.config.IdleAppIDs) > 0 {
		if len(statusText) > MaxStatusLength {
			statusText = statusText[:MaxStatusLength]
		}

		m.updateSteamStatus(ctx, statusText)
	}

	for {
		select {
		case <-ctx.Done():
			return

		case <-subLoggedOn.C():
			statusText := m.nextStatusText(ctx, &frameIdx, &marqueeOffset)
			if statusText != "" || len(m.config.IdleAppIDs) > 0 {
				if len(statusText) > MaxStatusLength {
					statusText = statusText[:MaxStatusLength]
				}

				m.updateSteamStatus(ctx, statusText)
			}

		case ev := <-subState.C():
			if sev, ok := ev.(*auth.StateEvent); ok && sev.New == auth.StateLoggedOn {
				statusText := m.nextStatusText(ctx, &frameIdx, &marqueeOffset)
				if statusText != "" || len(m.config.IdleAppIDs) > 0 {
					if len(statusText) > MaxStatusLength {
						statusText = statusText[:MaxStatusLength]
					}

					m.updateSteamStatus(ctx, statusText)
				}
			}

		case <-ticker.C:
			statusText := m.nextStatusText(ctx, &frameIdx, &marqueeOffset)
			if statusText == "" && len(m.config.IdleAppIDs) == 0 {
				continue
			}

			if len(statusText) > MaxStatusLength {
				statusText = statusText[:MaxStatusLength]
			}

			m.updateSteamStatus(ctx, statusText)
		}
	}
}

func (m *Manager) nextStatusText(ctx context.Context, frameIdx, marqueeOffset *int) string {
	m.stateMu.RLock()
	flashMsg := m.flashText
	isFlashActive := time.Now().Before(m.activeFlashUntil)
	provider := m.dynamicProvider
	m.stateMu.RUnlock()

	if isFlashActive && flashMsg != "" {
		return flashMsg
	}

	if provider != nil {
		if custom := provider(ctx); custom != "" {
			return custom
		}
	}

	if len(m.config.Slides) > 0 {
		text := m.config.Slides[*frameIdx]
		*frameIdx = (*frameIdx + 1) % len(m.config.Slides)

		return text
	}

	if m.config.MarqueeText != "" {
		text := RenderMarquee(m.config.MarqueeText, *marqueeOffset, m.config.MarqueeWidth)
		*marqueeOffset++

		return text
	}

	return ""
}

func (m *Manager) updateSteamStatus(ctx context.Context, statusText string) {
	if len(m.config.IdleAppIDs) == 0 && statusText == "" {
		return
	}

	if err := m.playCombined(ctx, m.config.IdleAppIDs, statusText); err != nil {
		m.Logger.Warn("Failed to update status", log.Err(err))
	} else {
		m.Bus.Publish(&StatusUpdatedEvent{
			StatusText: statusText,
			IdleAppIDs: m.config.IdleAppIDs,
			Timestamp:  time.Now(),
		})
	}
}

func (m *Manager) playCombined(ctx context.Context, appIDs []uint32, customText string) error {
	games := make([]*pb.CMsgClientGamesPlayed_GamePlayed, 0, len(appIDs)+1)

	if customText != "" {
		games = append(games, &pb.CMsgClientGamesPlayed_GamePlayed{
			GameId:        proto.Uint64(apps.NonSteamGameID),
			GameExtraInfo: proto.String(customText),
		})
	}

	for _, appID := range appIDs {
		games = append(games, &pb.CMsgClientGamesPlayed_GamePlayed{
			GameId: proto.Uint64(uint64(appID)),
		})
	}

	req := &pb.CMsgClientGamesPlayed{
		GamesPlayed: games,
	}

	_, err := service.LegacyProto[service.NoResponse](
		ctx, m.service, enums.EMsg_ClientGamesPlayedWithDataBlob, req,
	)

	return err
}

func (m *Manager) listenEvents(ctx context.Context, sub *bus.Subscription) {
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-sub.C():
			if !ok {
				return
			}

			offerEv, isOffer := ev.(*web.NewOfferEvent)
			if !isOffer || offerEv.Offer == nil {
				continue
			}

			flashMsg := fmt.Sprintf(
				"🚨 NEW OFFER #%d FROM %d",
				offerEv.Offer.ID,
				offerEv.Offer.OtherSteamID.AccountID(),
			)

			m.FlashStatus(flashMsg, m.config.FlashDuration)
		}
	}
}
