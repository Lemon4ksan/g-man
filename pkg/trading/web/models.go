// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"sync"

	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni/codec/values"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

type descKey = uint64

func packDescKey(classID, instanceID uint64) descKey {
	return descKey((classID << 32) | (instanceID & 0xFFFFFFFF))
}

func newDescKey(classID, instanceID string) descKey {
	cID, _ := strconv.ParseUint(classID, 10, 64)
	instID, _ := strconv.ParseUint(instanceID, 10, 64)
	return packDescKey(cID, instID)
}

type tradeOfferObj struct {
	NewVersion bool       `json:"newversion"`
	Version    int        `json:"version"`
	Me         sideObject `json:"me"`
	Them       sideObject `json:"them"`
}

type steamObject struct {
	AppID     uint32 `json:"appid"`
	ContextID string `json:"contextid"`
	Amount    int64  `json:"amount"`
	AssetID   string `json:"assetid"`
}

type sideObject struct {
	Assets   []steamObject `json:"assets"`
	Currency []any         `json:"currency"`
	Ready    bool          `json:"ready"`
}

type createParams struct {
	TradeOfferAccessToken string `json:"trade_offer_access_token"`
}

type sendNewReq struct {
	ServerID     int    `url:"serverid"`
	PartnerID    id.ID  `url:"partner"`
	Message      string `url:"tradeoffermessage"`
	JSON         string `url:"json_tradeoffer"`
	CreateParams string `url:"trade_offer_create_params,omitempty"`
	CounteredID  uint64 `url:"tradeofferid_countered,omitempty"`
}

var formBufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func (r sendNewReq) EncodeFormString() (string, error) {
	buf := formBufferPool.Get().(*bytes.Buffer)

	buf.Reset()
	defer formBufferPool.Put(buf)

	var intBuf [20]byte

	buf.WriteString("serverid=")
	buf.Write(strconv.AppendInt(intBuf[:0], int64(r.ServerID), 10))

	buf.WriteString("&partner=")
	buf.Write(strconv.AppendUint(intBuf[:0], uint64(r.PartnerID), 10))

	buf.WriteString("&tradeoffermessage=")
	bytesconv.AppendQueryEscaped(buf, bytesconv.S2B(r.Message))

	buf.WriteString("&json_tradeoffer=")
	bytesconv.AppendQueryEscaped(buf, bytesconv.S2B(r.JSON))

	if r.CreateParams != "" {
		buf.WriteString("&trade_offer_create_params=")
		bytesconv.AppendQueryEscaped(buf, bytesconv.S2B(r.CreateParams))
	}

	if r.CounteredID > 0 {
		buf.WriteString("&tradeofferid_countered=")
		buf.Write(strconv.AppendUint(intBuf[:0], r.CounteredID, 10))
	}

	return buf.String(), nil
}

type sendNewResponse struct {
	TradeOfferID string `json:"tradeofferid"`
	NeedsMobile  bool   `json:"needs_mobile_confirmation"`
	NeedsEmail   bool   `json:"needs_email_confirmation"`
}

type acceptResponse struct {
	TradeID                 string `json:"tradeid"`
	NeedsMobileConfirmation bool   `json:"needs_mobile_confirmation"`
	NeedsEmailConfirmation  bool   `json:"needs_email_confirmation"`
	EmailDomain             string `json:"email_domain"`
}

type tradeStatusReq struct {
	TradeID         uint64 `url:"tradeid"`
	GetDescriptions bool   `url:"get_descriptions"`
	Language        string `url:"language"`
}

type tradeStatusResp struct {
	Trades []struct {
		TradeID        uint64                  `json:"tradeid,string"`
		SteamIDOther   uint64                  `json:"steamid_other,string"`
		TimeInit       int64                   `json:"time_init"`
		Status         int                     `json:"status"`
		AssetsReceived []trading.ExchangeAsset `json:"assets_received"`
		AssetsGiven    []trading.ExchangeAsset `json:"assets_given"`
	} `json:"trades"`
}

type getOfferReq struct {
	TradeOfferID    uint64 `url:"tradeofferid"`
	GetDescriptions bool   `url:"get_descriptions"`
	Language        string `url:"language"`
}

type getOffersReq struct {
	GetReceivedOffers    int   `url:"get_received_offers"`
	GetSentOffers        int   `url:"get_sent_offers"`
	ActiveOnly           int   `url:"active_only"`
	GetDescriptions      int   `url:"get_descriptions"`
	TimeHistoricalCutoff int64 `url:"time_historical_cutoff"`
}

type getOffersResp struct {
	Sent         []*trading.TradeOffer `json:"trade_offers_sent"`
	Received     []*trading.TradeOffer `json:"trade_offers_received"`
	Descriptions []rawDescription      `json:"descriptions"`
}

type getAssetClassInfoResponse struct {
	Result map[string]json.RawMessage `json:"result"`
}

type rawDescription struct {
	AppID          uint32                `json:"appid"`
	ClassID        string                `json:"classid"`
	InstanceID     string                `json:"instanceid"`
	Name           string                `json:"name"`
	NameColor      string                `json:"name_color"`
	Type           string                `json:"type"`
	MarketName     string                `json:"market_name"`
	MarketHashName string                `json:"market_hash_name"`
	IconURL        string                `json:"icon_url"`
	Tradable       values.BoolInt        `json:"tradable"`
	Marketable     values.BoolInt        `json:"marketable"`
	Descriptions   []trading.Description `json:"descriptions"`
	Tags           []trading.Tag         `json:"tags"`
	Actions        []trading.Action      `json:"actions"`
}

type assetClassTag struct {
	Category              string `json:"category"`
	InternalName          string `json:"internal_name"`
	LocalizedCategoryName string `json:"localized_category_name"`
	LocalizedTagName      string `json:"localized_tag_name"`
	Name                  string `json:"name"`
}

// scanJSONObjectElements scans a JSON object {"0": val0, "1": val1} into raw byte slices without map allocations.
func scanJSONObjectElements(data []byte) ([][]byte, error) {
	i := 0
	n := len(data)

	for i < n && data[i] != '{' {
		i++
	}

	if i >= n {
		return nil, errors.New("invalid json object: missing opening brace")
	}

	i++

	var (
		stackBuf [32][]byte
		elements = stackBuf[:0]
		maxIdx   = -1
	)

	for i < n {
		for i < n && (data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n' || data[i] == ',') {
			i++
		}

		if i >= n {
			return nil, errors.New("invalid json object: unexpected EOF")
		}

		if data[i] == '}' {
			break
		}

		if data[i] != '"' {
			return nil, errors.New("invalid json object: expected key string")
		}

		i++

		keyStart := i
		for i < n && data[i] != '"' {
			i++
		}

		if i >= n {
			return nil, errors.New("invalid json object: unterminated key string")
		}

		keyBytes := data[keyStart:i]
		i++

		idx, ok := bytesconv.ParseUint64(keyBytes)
		if !ok {
			for i < n && data[i] != ':' {
				i++
			}

			if i < n {
				i++
			}

			i = skipJSONValue(data, i)

			continue
		}

		for i < n && data[i] != ':' {
			i++
		}

		if i >= n {
			return nil, errors.New("invalid json object: expected colon")
		}

		i++

		for i < n && (data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
			i++
		}

		valStart := i
		i = skipJSONValue(data, i)
		valEnd := i

		if valEnd > valStart {
			idxInt := int(idx)
			if idxInt > maxIdx {
				maxIdx = idxInt
			}

			if idxInt >= len(elements) {
				newLen := idxInt + 1
				if cap(elements) < newLen {
					newCap := cap(elements) * 2
					if newCap < newLen {
						newCap = newLen
					}

					newElems := make([][]byte, newLen, newCap)
					copy(newElems, elements)
					elements = newElems
				} else {
					elements = elements[:newLen]
				}
			}

			elements[idxInt] = data[valStart:valEnd]
		}
	}

	if maxIdx < 0 {
		return nil, errors.New("invalid json object: no valid numeric keys found")
	}

	return elements[:maxIdx+1], nil
}

func skipJSONValue(data []byte, i int) int {
	n := len(data)
	if i >= n {
		return i
	}

	depth := 0
	inString := false
	escaped := false

	for i < n {
		c := data[i]

		if escaped {
			escaped = false
			i++
			continue
		}

		if c == '\\' && inString {
			escaped = true
			i++
			continue
		}

		if c == '"' {
			inString = !inString

			i++
			if !inString && depth == 0 {
				return i
			}

			continue
		}

		if inString {
			i++
			continue
		}

		switch c {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		case ',', ' ', '\t', '\r', '\n':
			if depth == 0 {
				return i
			}
		}

		i++
	}

	return i
}

var flexBufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// unmarshalFlexibleArray stitches pseudo-array object elements into a single JSON array,
// performing a single json.Unmarshal call to eliminate multi-iteration decoder overhead.
func unmarshalFlexibleArray[T any](data []byte) ([]T, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}

	switch data[0] {
	case '"':
		return nil, nil

	case '[':
		var arr []T
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, err
		}

		return arr, nil

	case '{':
		rawElements, err := scanJSONObjectElements(data)
		if err != nil {
			return nil, err
		}

		if len(rawElements) == 0 {
			return nil, nil
		}

		buf := flexBufPool.Get().(*bytes.Buffer)

		buf.Reset()
		defer flexBufPool.Put(buf)

		buf.WriteByte('[')

		for i, raw := range rawElements {
			if i > 0 {
				buf.WriteByte(',')
			}

			if len(raw) > 0 {
				buf.Write(raw)
			} else {
				buf.WriteString("null")
			}
		}

		buf.WriteByte(']')

		var res []T
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			return nil, err
		}

		return res, nil

	default:
		return nil, fmt.Errorf("failed to unmarshal flexible array: %s", string(data))
	}
}

type flexibleDescriptions []trading.Description

func (fd *flexibleDescriptions) UnmarshalJSON(data []byte) error {
	res, err := unmarshalFlexibleArray[trading.Description](data)
	if err != nil {
		return err
	}

	*fd = res

	return nil
}

type flexibleTags []assetClassTag

func (ft *flexibleTags) UnmarshalJSON(data []byte) error {
	res, err := unmarshalFlexibleArray[assetClassTag](data)
	if err != nil {
		return err
	}

	*ft = res

	return nil
}

type rawAssetClassDescription struct {
	ClassID        string               `json:"classid"`
	InstanceID     string               `json:"instanceid"`
	Name           string               `json:"name"`
	MarketName     string               `json:"market_name"`
	Type           string               `json:"type"`
	MarketHashName string               `json:"market_hash_name"`
	IconURL        string               `json:"icon_url"`
	Descriptions   flexibleDescriptions `json:"descriptions"`
	Tags           flexibleTags         `json:"tags"`
	Tradable       values.BoolInt       `json:"tradable"`
	Marketable     values.BoolInt       `json:"marketable"`
}
