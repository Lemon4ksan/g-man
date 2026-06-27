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

// ID represents a unique 64-bit Steam identifier.
// Bit structure:
// [ 8 bits: Universe | 4 bits: Account Type | 20 bits: Instance | 32 bits: Account ID ]
//
// Create new instances of ID using [New], [Parse], [FromAccountID], or [Resolve].
// To verify if a parsed ID contains valid bits within a plausible range, use [ID.IsValid].
type ID uint64

const (
	// InvalidID is the default null value for a Steam [ID].
	InvalidID ID = 0

	// Base 64-bit ID for individual accounts in the public universe.
	individualBase ID = ID(
		(uint64(UniversePublic) << 56) | (uint64(AccountTypeIndividual) << 52) | (1 << 32),
	) // 76561197960265728
)

// Universe defines the Steam network universe.
// Retrieve an account's universe using the [ID.Universe] method.
type Universe uint8

const (
	// UniverseInvalid represents an uninitialized or invalid universe.
	UniverseInvalid Universe = 0
	// UniversePublic represents the standard public Steam network.
	UniversePublic Universe = 1
	// UniverseBeta represents the Steam beta network.
	UniverseBeta Universe = 2
	// UniverseInternal represents Valve's internal network.
	UniverseInternal Universe = 3
	// UniverseDev represents the Steam development network.
	UniverseDev Universe = 4
)

// String returns a human-readable representation of the [Universe].
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

// AccountType defines the classification of the account.
// Retrieve an account's classification using the [ID.Type] method.
type AccountType uint8

const (
	// AccountTypeInvalid represents an invalid or unknown account type.
	AccountTypeInvalid AccountType = 0
	// AccountTypeIndividual represents a standard user account.
	AccountTypeIndividual AccountType = 1
	// AccountTypeMultiseat represents a shared account (legacy).
	AccountTypeMultiseat AccountType = 2
	// AccountTypeGameServer represents an official game server.
	AccountTypeGameServer AccountType = 3
	// AccountTypeAnonGameServer represents an anonymous game server.
	AccountTypeAnonGameServer AccountType = 4
	// AccountTypePending represents a pending account.
	AccountTypePending AccountType = 5
	// AccountTypeContentServer represents a Steam content server.
	AccountTypeContentServer AccountType = 6
	// AccountTypeClan represents a Steam Group (Clan).
	AccountTypeClan AccountType = 7
	// AccountTypeChat represents a Steam chat room.
	AccountTypeChat AccountType = 8
	// AccountTypeConsoleUser represents a legacy console user.
	AccountTypeConsoleUser AccountType = 9
	// AccountTypeAnonUser represents an anonymous user account.
	AccountTypeAnonUser AccountType = 10
)

// String returns a human-readable representation of the [AccountType].
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

var (
	reSteam2 = regexp.MustCompile(`^STEAM_([0-5]):([0-1]):([0-9]+)$`)
	reSteam3 = regexp.MustCompile(`^\[([A-Z]):([0-5]):([0-9]+)(:[0-9]+)?\]$`)
	reURL    = regexp.MustCompile(`(?:https?://)?steamcommunity\.com/(?:profiles|id)/([a-zA-Z0-9_-]+)`)
)

// New constructs an [ID] from a raw 64-bit unsigned integer.
// It returns [InvalidID] if the input integer is 0.
func New(id uint64) ID { return ID(id) }

// FromAccountID creates a standard individual [ID] in the public universe from a 32-bit AccountID.
// If the accountID argument is 0, it still constructs an individual ID with Account ID set to 0.
func FromAccountID(accountID uint32) ID {
	return ID(accountID) + individualBase
}

// Parse parses a string representation of a Steam ID into an [ID].
// It supports parsing legacy Steam2 formats, modern Steam3 formats, and raw 64-bit string values.
// It returns [InvalidID] if the input string is empty, malformed, or invalid.
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

// AccountID returns the 32-bit portion of the [ID] representing the user's account number.
// It returns 0 if the original [ID] is [InvalidID].
func (id ID) AccountID() uint32 {
	return uint32(uint64(id) & 0xFFFFFFFF)
}

// Instance returns the 20-bit portion of the [ID] representing the account instance.
// It returns 0 if the original [ID] is [InvalidID].
func (id ID) Instance() uint32 {
	return uint32((uint64(id) >> 32) & 0xFFFFF)
}

// Type returns the account classification of the [ID] as an [AccountType].
// It returns [AccountTypeInvalid] if the original [ID] is [InvalidID].
func (id ID) Type() AccountType {
	return AccountType((uint64(id) >> 52) & 0xF)
}

// Universe returns the Steam network universe of the [ID] as a [Universe].
// It returns [UniverseInvalid] if the original [ID] is [InvalidID].
func (id ID) Universe() Universe {
	return Universe((uint64(id) >> 56) & 0xFF)
}

// IsValid checks if the [ID] bits are within a plausible range of universes and account types.
func (id ID) IsValid() bool {
	t := id.Type()
	u := id.Universe()

	return u > UniverseInvalid && u <= UniverseDev && t > AccountTypeInvalid && t <= AccountTypeAnonUser
}

// Steam2 returns the legacy string representation of the [ID].
// Calling this on an [InvalidID] or non-individual accounts yields a mathematically formatted legacy string.
func (id ID) Steam2() string {
	accID := uint64(id.AccountID())
	return fmt.Sprintf("STEAM_0:%d:%d", accID%2, accID/2)
}

// Steam3 returns the modern string representation of the [ID].
// Calling this on an [InvalidID] yields a formatted string with account ID set to 0.
func (id ID) Steam3() string {
	return fmt.Sprintf("[U:1:%d]", id.AccountID())
}

// String returns the raw 64-bit [ID] formatted as a decimal string.
func (id ID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// Uint64 returns the raw 64-bit value of the [ID] as an unsigned integer.
func (id ID) Uint64() uint64 {
	return uint64(id)
}

// MarshalJSON encodes the [ID] as a JSON decimal string to prevent precision loss.
// It conforms to the [encoding/json.Marshaler] interface.
func (id ID) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, `"%d"`, id), nil
}

// UnmarshalJSON decodes a JSON decimal string or numeric value into the [ID].
// It conforms to the [encoding/json.Unmarshaler] interface.
// If the JSON data is empty or represents a null value, the target is set to [InvalidID].
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

// Resolve extracts a Steam [ID] from a raw string, profile URL, or custom vanity URL.
// If the input is a vanity custom URL slug, Resolve queries the Steam WebAPI using the [service.Doer] client.
// It returns [InvalidID] and an error if the input format is invalid, if vanity resolution fails, or if the context is cancelled.
// Calling Resolve with a nil [service.Doer] client will result in a panic.
func Resolve(ctx context.Context, d service.Doer, input string) (ID, error) {
	input = strings.TrimSpace(input)
	if id := Parse(input); id.IsValid() {
		return id, nil
	}

	// Check if it's a URL
	matches := reURL.FindStringSubmatch(input)
	if len(matches) < 2 {
		return InvalidID, errors.New("steamid: invalid input format")
	}

	slug := matches[1]
	// If the slug is already a 64-bit ID, return it
	if id := Parse(slug); id.IsValid() {
		return id, nil
	}

	// Otherwise, it's a Vanity URL, resolve via ISteamUser
	return ResolveVanityURL(ctx, d, slug)
}

// ResolveVanityURL resolves a custom Steam vanity URL slug into a 64-bit [ID] using the Steam WebAPI.
// It returns [InvalidID] and an error if the WebAPI request fails, if the context is cancelled, or if vanityURL is empty.
// Calling ResolveVanityURL with a nil [service.Doer] client will result in a panic.
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
		return InvalidID, fmt.Errorf(
			"steamid: could not resolve vanity URL (success=%d, msg=%s)",
			res.Success,
			res.Message,
		)
	}

	return Parse(res.SteamID), nil
}

// ParseTradeURL parses a Steam trade link, extracting the partner's 64-bit [ID] and trade token.
// It returns [InvalidID] and an error if tradeURL is empty, malformed, or missing query parameters.
func ParseTradeURL(tradeURL string) (ID, string, error) {
	if tradeURL == "" {
		return 0, "", errors.New("trade url is empty")
	}

	u, err := url.Parse(tradeURL)
	if err != nil {
		return 0, "", err
	}

	queryParams := u.Query()
	partnerStr := queryParams.Get("partner")
	token := queryParams.Get("token")

	if partnerStr == "" {
		return 0, "", errors.New("missing partner parameter in trade URL")
	}

	accountID, err := strconv.ParseUint(partnerStr, 10, 32)
	if err != nil {
		return 0, "", err
	}

	return FromAccountID(uint32(accountID)), token, nil
}
