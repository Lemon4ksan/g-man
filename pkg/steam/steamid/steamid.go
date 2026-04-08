// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package steamid provides utilities for parsing, validating, and converting SteamIDs
// between various formats (Steam2, Steam3, AccountID, and SteamID64).
package steamid

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// ID represents a unique 64-bit Steam identifier.
// Bit structure:
// [ 8 bits: Universe | 4 bits: Account Type | 20 bits: Instance | 32 bits: Account ID ]
type ID uint64

const (
	// InvalidID is the default null value for a SteamID.
	InvalidID ID = 0

	// Base 64-bit ID for individual accounts in the public universe.
	individualBase ID = ID((uint64(UniversePublic) << 56) | (uint64(AccountTypeIndividual) << 52) | (1 << 32)) // 76561197960265728
)

// Universe defines steam account universe.
type Universe uint8

const (
	UniverseInvalid  Universe = 0
	UniversePublic   Universe = 1
	UniverseBeta     Universe = 2
	UniverseInternal Universe = 3
	UniverseDev      Universe = 4
)

// AccountType defines object type, which this ID belongs to.
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

var (
	reSteam2 = regexp.MustCompile(`^STEAM_([0-5]):([0-1]):([0-9]+)$`)
	reSteam3 = regexp.MustCompile(`^\[([A-Z]):([0-5]):([0-9]+)(:[0-9]+)?\]$`)
	reURL    = regexp.MustCompile(`(?:https?://)?steamcommunity\.com/(?:profiles|id)/([a-zA-Z0-9_-]+)`)
)

// New constructs a SteamID from a uint64.
func New(id uint64) ID { return ID(id) }

// FromAccountID creates an individual SteamID from a 32-bit AccountID.
func FromAccountID(accountID uint32) ID {
	return ID(accountID) + individualBase
}

// Parse parses a string representation of a SteamID (Steam2, Steam3, or 64-bit string).
func Parse(s string) ID {
	if s == "" {
		return InvalidID
	}

	// Try 64-bit uint64 string
	if id, err := strconv.ParseUint(s, 10, 64); err == nil {
		return ID(id)
	}

	// Try Steam2 (STEAM_0:0:12345)
	if m := reSteam2.FindStringSubmatch(s); m != nil {
		authServer, _ := strconv.ParseUint(m[2], 10, 64)
		accountID, _ := strconv.ParseUint(m[3], 10, 64)
		return ID(individualBase.Uint64() + (accountID * 2) + authServer)
	}

	// Try Steam3 ([U:1:12345])
	if m := reSteam3.FindStringSubmatch(s); m != nil {
		accountID, _ := strconv.ParseUint(m[3], 10, 64)
		return FromAccountID(uint32(accountID))
	}

	return InvalidID
}

// AccountID returns the 32-bit part of the SteamID.
func (id ID) AccountID() uint32 {
	return uint32(uint64(id) & 0xFFFFFFFF)
}

// Instance returns the 20-bit portion of the identifier (account instance).
func (id ID) Instance() uint32 {
	return uint32((uint64(id) >> 32) & 0xFFFFF)
}

// Type returns account type.
func (id ID) Type() AccountType {
	return AccountType((uint64(id) >> 52) & 0xF)
}

// Universe returns the account's universe.
func (id ID) Universe() Universe {
	return Universe((uint64(id) >> 56) & 0xFF)
}

// Valid checks if the ID is within a plausible range.
func (id ID) Valid() bool {
	t := id.Type()
	u := id.Universe()
	return u > UniverseInvalid && u <= UniverseDev && t > AccountTypeInvalid && t <= AccountTypeAnonUser
}

// Steam2 returns the legacy format: STEAM_0:0:42063864
func (id ID) Steam2() string {
	accID := uint64(id.AccountID())
	return fmt.Sprintf("STEAM_0:%d:%d", accID%2, accID/2)
}

// Steam3 returns the modern format: [U:1:84127728]
func (id ID) Steam3() string {
	return fmt.Sprintf("[U:1:%d]", id.AccountID())
}

// String returns the SteamID64 as a string.
func (id ID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// Uint64 returns the raw 64-bit value.
func (id ID) Uint64() uint64 {
	return uint64(id)
}

// MarshalJSON implements the json.Marshaler interface.
// SteamID is always marshaled to a string, as JavaScript does not support 64-bit integers without loss of precision.
func (id ID) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, `"%d"`, id), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// Supports parsing from both numbers and strings.
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

// Resolve attempts to get a SteamID from a string that could be an ID or a URL.
// If it's a Vanity URL (e.g. /id/lemon4ksan), it uses the service.Doer to resolve it via WebAPI.
func Resolve(ctx context.Context, d service.Doer, input string) (ID, error) {
	input = strings.TrimSpace(input)
	if id := Parse(input); id.Valid() {
		return id, nil
	}

	// Check if it's a URL
	matches := reURL.FindStringSubmatch(input)
	if len(matches) < 2 {
		return InvalidID, fmt.Errorf("steamid: invalid input format")
	}

	slug := matches[1]
	// If the slug is already a 64-bit ID, return it
	if id := Parse(slug); id.Valid() {
		return id, nil
	}

	// Otherwise, it's a Vanity URL, resolve via ISteamUser
	return ResolveVanityURL(ctx, d, slug)
}

// ResolveVanityURL calls ISteamUser/ResolveVanityURL WebAPI.
func ResolveVanityURL(ctx context.Context, d service.Doer, vanityURL string) (ID, error) {
	type response struct {
		SteamID string `json:"steamid"`
		Success int    `json:"success"`
		Message string `json:"message"`
	}

	req := struct {
		VanityURL string `url:"vanityurl"`
	}{VanityURL: vanityURL}

	// Using the WebAPI helper from the service package
	res, err := service.WebAPI[response](ctx, d, "GET", "ISteamUser", "ResolveVanityURL", 1, req)
	if err != nil {
		return InvalidID, err
	}

	if res.Success != 1 {
		return InvalidID, fmt.Errorf("steamid: could not resolve vanity URL (success=%d, msg=%s)", res.Success, res.Message)
	}

	return Parse(res.SteamID), nil
}
