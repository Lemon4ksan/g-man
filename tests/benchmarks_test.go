// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// tests/benchmarks_test.go

package tests_test

import (
	"bytes"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/lemon4ksan/aoni/cookie"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
	"github.com/lemon4ksan/g-man/internal/crypto"
	"github.com/lemon4ksan/g-man/internal/socket/connector"
	"github.com/lemon4ksan/g-man/pkg/command"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding/bvdf"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

func BenchmarkSteamID_Parse_Uint64(b *testing.B) {
	s := "76561198000000001"

	b.ReportAllocs()

	for b.Loop() {
		_ = id.Parse(s)
	}
}

func BenchmarkSteamID_Parse_Steam2(b *testing.B) {
	s := "STEAM_0:1:19867136"

	b.ReportAllocs()

	for b.Loop() {
		_ = id.Parse(s)
	}
}

func BenchmarkSteamID_Parse_Steam3(b *testing.B) {
	s := "[U:1:39734273]"

	b.ReportAllocs()

	for b.Loop() {
		_ = id.Parse(s)
	}
}

func BenchmarkSteamFramer_ReadFrame(b *testing.B) {
	frameData := []byte{0x05, 0x00, 0x00, 0x00, 'V', 'T', '0', '1', 'H', 'e', 'l', 'l', 'o'}
	framer := connector.SteamFramer{}
	r := bytes.NewReader(frameData)

	b.ReportAllocs()

	for b.Loop() {
		r.Reset(frameData)
		_, _ = framer.ReadFrame(r)
	}
}

func BenchmarkCrypto_GenerateAuthCode(b *testing.B) {
	secret := "1234567890123456789012345678901234567890"
	timestamp := int64(1700000000)

	b.ReportAllocs()

	for b.Loop() {
		_, _ = crypto.GenerateAuthCode(secret, timestamp)
	}
}

func BenchmarkProtocol_ParseGCPacket_Proto(b *testing.B) {
	payload := []byte("example_gc_protobuf_payload_data_bytes_for_benchmarks")
	p := protocol.NewGCPacket(440, 1001, payload)
	p.IsProto = true
	p.SourceJobID = 111
	p.TargetJobID = 222

	serialized, err := p.Serialize()
	if err != nil {
		b.Fatalf("failed to serialize GCPacket: %v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		pkt, err := protocol.ParseGCPacket(440, 1001|protocol.ProtoMask, serialized)
		if err != nil {
			b.Fatal(err)
		}

		_ = pkt
	}
}

var formBufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

type mockSendReq struct {
	ServerID     int
	PartnerID    id.ID
	Message      string
	JSON         string
	CreateParams string
}

func (r mockSendReq) EncodeFormString() (string, error) {
	buf := formBufferPool.Get().(*bytes.Buffer)

	buf.Reset()
	defer formBufferPool.Put(buf)

	buf.WriteString("serverid=")
	buf.WriteString(strconv.Itoa(r.ServerID))
	buf.WriteString("&partner=")

	var numBuf [20]byte
	buf.Write(strconv.AppendUint(numBuf[:0], uint64(r.PartnerID), 10))

	buf.WriteString("&tradeoffermessage=")
	bytesconv.AppendQueryEscaped(buf, []byte(r.Message))

	buf.WriteString("&json_tradeoffer=")
	bytesconv.AppendQueryEscaped(buf, []byte(r.JSON))

	return buf.String(), nil
}

func BenchmarkTrading_FastFormEncoder(b *testing.B) {
	req := mockSendReq{
		ServerID:  1,
		PartnerID: id.ID(76561198000000001),
		Message:   "Automated Trade Offer",
		JSON:      `{"newversion":true,"version":2,"me":{"assets":[]},"them":{"assets":[]}}`,
	}

	b.ReportAllocs()

	for b.Loop() {
		_, _ = req.EncodeFormString()
	}
}

func BenchmarkEncoding_RapidValidate_JSON(b *testing.B) {
	data := []byte(`{"response":{"trade_offers_sent":[],"trade_offers_received":[]}}`)

	b.ReportAllocs()

	for b.Loop() {
		_ = encoding.RapidValidateSteamResponse(data)
	}
}

func BenchmarkCrypto_GenerateConfirmationKey(b *testing.B) {
	identitySecret := "1234567890123456789012345678901234567890"
	timestamp := int64(1700000000)
	tag := "conf"

	b.ReportAllocs()

	for b.Loop() {
		_, _ = crypto.GenerateConfirmationKey(identitySecret, timestamp, tag)
	}
}

func BenchmarkCommand_ParseCommandLine(b *testing.B) {
	line := `!accept 123456789 "trade offer note"`

	b.ReportAllocs()

	for b.Loop() {
		_ = command.ParseCommandLine(line)
	}
}

func BenchmarkCookie_BuildCookieHeader(b *testing.B) {
	cookies := []*http.Cookie{
		{Name: "sessionid", Value: "1234567890abcdef", Path: "/"},
		{Name: "steamLoginSecure", Value: "76561198000000001||token", Path: "/"},
		{Name: "browserid", Value: "9876543210", Path: "/"},
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = cookie.BuildCookieHeader(cookies)
	}
}

func BenchmarkTrading_ItemLockingPrep(b *testing.B) {
	offer := &trading.TradeOffer{
		ID: 123456,
		ItemsToGive: []*trading.Item{
			{AssetID: 10001}, {AssetID: 10002}, {AssetID: 10003}, {AssetID: 10004}, {AssetID: 10005},
		},
		ItemsToReceive: []*trading.Item{
			{AssetID: 20001}, {AssetID: 20002}, {AssetID: 20003}, {AssetID: 20004}, {AssetID: 20005},
		},
	}

	b.ReportAllocs()

	for b.Loop() {
		totalLen := len(offer.ItemsToGive) + len(offer.ItemsToReceive)

		var (
			stackIDs [32]uint64
			ids      []uint64
		)

		if totalLen <= len(stackIDs) {
			ids = stackIDs[:0]
		} else {
			ids = make([]uint64, 0, totalLen)
		}

		for _, it := range offer.ItemsToGive {
			ids = append(ids, it.AssetID)
		}

		for _, it := range offer.ItemsToReceive {
			ids = append(ids, it.AssetID)
		}

		_ = ids
	}
}

func BenchmarkBVDF_ParseAppInfo(b *testing.B) {
	data := append([]byte{
		0x28, 0x44, 0x56, 0x07, // AppInfoMagic40
		0x01, 0x00, 0x00, 0x00, // Universe 1
		0xb8, 0x01, 0x00, 0x00, // AppID 440
		0x3c, 0x00, 0x00, 0x00, // Size 60
	}, make([]byte, 80)...)

	b.ReportAllocs()

	for b.Loop() {
		_, _ = bvdf.ParseAppInfo(data)
	}
}
