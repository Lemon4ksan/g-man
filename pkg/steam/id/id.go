// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package id

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// ID represents a 64-bit Steam identifier.
// Bit structure: [ 8 bits: Universe | 4 bits: Account Type | 20 bits: Instance | 32 bits: Account ID ].
type ID uint64

const (
	InvalidID ID = 0

	IndividualBase ID = ID(
		(uint64(UniversePublic) << 56) | (uint64(AccountTypeIndividual) << 52) | (1 << 32),
	)
)

var (
	// ErrInvalidInputFormat indicates an input string could not be parsed as a Steam ID or profile URL.
	ErrInvalidInputFormat = errors.New("steamid: invalid input format")
	// ErrEmptyTradeURL indicates an empty trade offer URL string was passed.
	ErrEmptyTradeURL = errors.New("steamid: trade url is empty")
	// ErrMissingPartnerParam indicates a trade URL lacked the 'partner' query parameter.
	ErrMissingPartnerParam = errors.New("steamid: missing partner parameter in trade URL")
)

type Universe uint8

const (
	UniverseInvalid  Universe = 0
	UniversePublic   Universe = 1
	UniverseBeta     Universe = 2
	UniverseInternal Universe = 3
	UniverseDev      Universe = 4
)

func (u Universe) String() string {
	switch u {
	case UniverseInvalid:
		return "Invalid"
	case UniversePublic:
		return "Public"
	case UniverseBeta:
		return "Beta"
	case UniverseInternal:
		return "Internal"
	case UniverseDev:
		return "Dev"
	default:
		return fmt.Sprintf("Universe(%d)", u)
	}
}

type AccountType uint8

const (
	AccountTypeInvalid        AccountType = 0
	AccountTypeIndividual     AccountType = 1
	AccountTypeMultiseat      AccountType = 2
	AccountTypeGameServer     AccountType = 3
	AccountTypeAnonGameServer AccountType = 4
	AccountTypePending        AccountType = 5
	AccountTypeContentServer  AccountType = 6
	AccountTypeClan           AccountType = 7
	AccountTypeChat           AccountType = 8
	AccountTypeConsoleUser    AccountType = 9
	AccountTypeAnonUser       AccountType = 10
)

func (a AccountType) String() string {
	switch a {
	case AccountTypeInvalid:
		return "Invalid"
	case AccountTypeIndividual:
		return "Individual"
	case AccountTypeMultiseat:
		return "Multiseat"
	case AccountTypeGameServer:
		return "GameServer"
	case AccountTypeAnonGameServer:
		return "AnonGameServer"
	case AccountTypePending:
		return "Pending"
	case AccountTypeContentServer:
		return "ContentServer"
	case AccountTypeClan:
		return "Clan"
	case AccountTypeChat:
		return "Chat"
	case AccountTypeConsoleUser:
		return "ConsoleUser"
	case AccountTypeAnonUser:
		return "AnonUser"
	default:
		return fmt.Sprintf("AccountType(%d)", a)
	}
}

var rxURL = regexp.MustCompile(`(?:https?://)?steamcommunity\.com/(?:profiles|id)/([a-zA-Z0-9_-]+)`)

// New constructs an ID from a raw 64-bit unsigned integer.
func New(id uint64) ID { return ID(id) }

// FromAccountID converts a 32-bit account ID into a 64-bit individual public universe Steam ID.
func FromAccountID(accountID uint32) ID {
	return ID(accountID) + IndividualBase
}

// Parse parses string representations of 64-bit raw IDs, legacy Steam2 (STEAM_0:1:...), or Steam3 ([U:1:...]) formats.
func Parse(s string) ID {
	if len(s) == 0 {
		return InvalidID
	}

	if s[0] >= '0' && s[0] <= '9' {
		if idVal, err := strconv.ParseUint(s, 10, 64); err == nil {
			return ID(idVal)
		}

		return InvalidID
	}

	if strings.HasPrefix(s, "STEAM_") {
		rest := s[6:]

		idx1 := strings.IndexByte(rest, ':')
		if idx1 == -1 {
			return InvalidID
		}

		idx2 := strings.IndexByte(rest[idx1+1:], ':')
		if idx2 == -1 {
			return InvalidID
		}

		idx2 += idx1 + 1

		authServer, err1 := strconv.ParseUint(rest[idx1+1:idx2], 10, 64)
		accountID, err2 := strconv.ParseUint(rest[idx2+1:], 10, 64)

		if err1 == nil && err2 == nil {
			return ID(IndividualBase.Uint64() + (accountID * 2) + authServer)
		}

		return InvalidID
	}

	if strings.HasPrefix(s, "[U:1:") && strings.HasSuffix(s, "]") {
		inner := s[5 : len(s)-1]
		if idx := strings.IndexByte(inner, ':'); idx != -1 {
			inner = inner[:idx]
		}

		if accountID, err := strconv.ParseUint(inner, 10, 32); err == nil {
			return FromAccountID(uint32(accountID))
		}

		return InvalidID
	}

	return InvalidID
}

// AccountID extracts the 32-bit account ID portion.
func (id ID) AccountID() uint32 {
	return uint32(uint64(id) & 0xFFFFFFFF)
}

// Instance extracts the 20-bit instance portion.
func (id ID) Instance() uint32 {
	return uint32((uint64(id) >> 32) & 0xFFFFF)
}

// Type extracts the 4-bit account type descriptor.
func (id ID) Type() AccountType {
	return AccountType((uint64(id) >> 52) & 0xF)
}

// Universe extracts the 8-bit universe descriptor.
func (id ID) Universe() Universe {
	return Universe((uint64(id) >> 56) & 0xFF)
}

// IsValid reports whether universe and account type bits fall within standard valid ranges.
func (id ID) IsValid() bool {
	t := id.Type()
	u := id.Universe()

	return u > UniverseInvalid && u <= UniverseDev && t > AccountTypeInvalid && t <= AccountTypeAnonUser
}

// Steam2 formats the ID as legacy "STEAM_0:X:YYYY".
func (id ID) Steam2() string {
	accID := uint64(id.AccountID())

	return fmt.Sprintf("STEAM_0:%d:%d", accID%2, accID/2)
}

// Steam3 formats the ID as modern "[U:1:YYYY]".
func (id ID) Steam3() string {
	return fmt.Sprintf("[U:1:%d]", id.AccountID())
}

func (id ID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

func (id ID) Uint64() uint64 {
	return uint64(id)
}

func (id ID) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, `"%d"`, id), nil
}

func (id *ID) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		*id = InvalidID

		return nil
	}

	s := strings.Trim(string(data), `"`)
	if s == "null" {
		*id = InvalidID

		return nil
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("steamid: invalid json value: %w", err)
	}

	*id = ID(val)

	return nil
}

// Resolve parses raw IDs, profile URLs, or vanity custom URL slugs using ISteamUser/ResolveVanityURL.
func Resolve(ctx context.Context, d service.Doer, input string) (ID, error) {
	input = strings.TrimSpace(input)
	if id := Parse(input); id.IsValid() {
		return id, nil
	}

	matches := rxURL.FindStringSubmatch(input)
	if len(matches) < 2 {
		return InvalidID, ErrInvalidInputFormat
	}

	slug := matches[1]
	if id := Parse(slug); id.IsValid() {
		return id, nil
	}

	return ResolveVanityURL(ctx, d, slug)
}

// ResolveVanityURL resolves a custom vanity URL slug via WebAPI.
func ResolveVanityURL(ctx context.Context, d service.Doer, vanityURL string) (ID, error) {
	type response struct {
		SteamID string `json:"steamid"`
		Success int    `json:"success"`
		Message string `json:"message"`
	}

	req := struct {
		VanityURL string `url:"vanityurl"`
	}{VanityURL: vanityURL}

	res, err := service.WebAPI[response](ctx, d, "GET", "ISteamUser", "ResolveVanityURL", 1, req)
	if err != nil {
		return InvalidID, err
	}

	if res.Success != 1 {
		return InvalidID, fmt.Errorf(
			"steamid: could not resolve vanity URL (success=%d, msg=%s)",
			res.Success,
			res.Message,
		)
	}

	return Parse(res.SteamID), nil
}

// ParseTradeURL extracts partner Steam ID and trade token from a trade offer link.
func ParseTradeURL(tradeURL string) (ID, string, error) {
	if tradeURL == "" {
		return 0, "", ErrEmptyTradeURL
	}

	u, err := url.Parse(tradeURL)
	if err != nil {
		return 0, "", err
	}

	params := u.Query()
	partnerStr := params.Get("partner")
	token := params.Get("token")

	if partnerStr == "" {
		return 0, "", ErrMissingPartnerParam
	}

	accountID, err := strconv.ParseUint(partnerStr, 10, 32)
	if err != nil {
		return 0, "", err
	}

	return FromAccountID(uint32(accountID)), token, nil
}
