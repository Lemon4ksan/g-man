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

	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni/cookie"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
	"github.com/lemon4ksan/g-man/internal/crypto"
	"github.com/lemon4ksan/g-man/internal/socket/connector"
	"github.com/lemon4ksan/g-man/pkg/command"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/community/inventory"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding/bvdf"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// ============================================================================
// 1. BYTESCONV & NUMBER PARSING BENCHMARKS
// ============================================================================

func BenchmarkBytesconv_ParseUint64(b *testing.B) {
	data := []byte("76561198000000001")

	b.ReportAllocs()

	for b.Loop() {
		_, _ = bytesconv.ParseUint64(data)
	}
}

func BenchmarkBytesconv_ParseInt64(b *testing.B) {
	data := []byte("-1234567890")

	b.ReportAllocs()

	for b.Loop() {
		_, _ = bytesconv.ParseInt64(data)
	}
}

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

// ============================================================================
// 2. SOCKET & PROTOCOL PACKET PARSING BENCHMARKS
// ============================================================================

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

func BenchmarkProtocol_ParsePacket_FastBytesReader(b *testing.B) {
	// Construct a sample Protobuf CM header packet
	hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientHeartBeat, 76561198000000001, 101)
	hdrBuf := new(bytes.Buffer)
	_ = hdr.SerializeTo(hdrBuf)
	hdrBuf.WriteString("payload_data_bytes")
	packetBytes := hdrBuf.Bytes()

	r := bytes.NewReader(packetBytes)

	b.ReportAllocs()

	for b.Loop() {
		r.Reset(packetBytes)

		pkt, err := protocol.ParsePacket(r)
		if err != nil {
			b.Fatal(err)
		}

		protocol.ReleasePacket(pkt)
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

		protocol.ReleaseGCPacket(pkt)
	}
}

func BenchmarkProtocol_CMsgMulti_Unmarshal(b *testing.B) {
	multiMsg := &pb.CMsgMulti{
		MessageBody: []byte("sample_compressed_or_uncompressed_inner_packet_data"),
	}
	data, _ := protocol.MarshalProto(multiMsg)

	b.ReportAllocs()

	msg := &pb.CMsgMulti{}
	for b.Loop() {
		msg.Reset()
		_ = protocol.UnmarshalProto(data, msg)
	}
}

// ============================================================================
// 3. INVENTORY & FLEXIBLE ARRAY UNMARSHALING BENCHMARKS
// ============================================================================

func BenchmarkInventory_UnmarshalFlexibleArray_Array(b *testing.B) {
	data := []byte(`[{"value":"Line 1","color":"7a7a7a"},{"value":"Line 2","color":"ffffff"}]`)

	b.ReportAllocs()

	for b.Loop() {
		var descs []trading.Description
		if len(data) > 0 && data[0] == '[' {
			_ = json.Unmarshal(data, &descs)
		}
	}
}

func BenchmarkInventory_UnmarshalFlexibleArray_Object(b *testing.B) {
	// Steam's edge-case format: object instead of array
	data := []byte(`{"0":{"value":"Line 1","color":"7a7a7a"},"1":{"value":"Line 2","color":"ffffff"}}`)

	b.ReportAllocs()

	for b.Loop() {
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(data, &rawMap); err == nil {
			res := make([]trading.Description, len(rawMap))
			for k, raw := range rawMap {
				idx, ok := bytesconv.ParseUint64(bytesconv.S2B(k))
				if ok && idx < uint64(len(res)) {
					_ = json.Unmarshal(raw, &res[idx])
				}
			}
		}
	}
}

func BenchmarkInventory_ProcessAssets_Opt(b *testing.B) {
	assets := make([]inventory.Asset, 500)
	for i := range assets {
		assets[i] = inventory.Asset{
			AssetID:    strconv.Itoa(10000 + i),
			ClassID:    strconv.Itoa(20000 + (i % 10)),
			InstanceID: "0",
			Amount:     "1",
		}
	}

	descriptions := make([]inventory.Description, 10)
	for i := range descriptions {
		descriptions[i] = inventory.Description{
			ClassID:        strconv.Itoa(20000 + i),
			InstanceID:     "0",
			Name:           "Item Name",
			MarketHashName: "Item Market Hash Name",
			Tradable:       1,
		}
	}

	b.ReportAllocs()

	for b.Loop() {
		descMap := make(map[struct{ ClassID, InstanceID uint64 }]*inventory.Description, len(descriptions))
		for i := range descriptions {
			d := &descriptions[i]
			cID, _ := bytesconv.ParseUint64(bytesconv.S2B(d.ClassID))
			instID, _ := bytesconv.ParseUint64(bytesconv.S2B(d.InstanceID))
			descMap[struct{ ClassID, InstanceID uint64 }{ClassID: cID, InstanceID: instID}] = d
		}

		pos := 1
		for i := range assets {
			asset := &assets[i]
			cID, _ := bytesconv.ParseUint64(bytesconv.S2B(asset.ClassID))
			instID, _ := bytesconv.ParseUint64(bytesconv.S2B(asset.InstanceID))
			key := struct{ ClassID, InstanceID uint64 }{ClassID: cID, InstanceID: instID}

			if desc, ok := descMap[key]; ok && desc.Tradable == 1 {
				asset.Pos = pos
				pos++
			}
		}
	}
}

// ============================================================================
// 4. TRADING & FORM ENCODING BENCHMARKS
// ============================================================================

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

// ============================================================================
// 5. CRYPTO & SECURITY BENCHMARKS
// ============================================================================

func BenchmarkCrypto_GenerateAuthCode(b *testing.B) {
	secret := "1234567890123456789012345678901234567890"
	timestamp := int64(1700000000)

	b.ReportAllocs()

	for b.Loop() {
		_, _ = crypto.GenerateAuthCode(secret, timestamp)
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

// ============================================================================
// 6. UTILITY & ENCODING BENCHMARKS
// ============================================================================

func BenchmarkEncoding_RapidValidate_JSON(b *testing.B) {
	data := []byte(`{"response":{"trade_offers_sent":[],"trade_offers_received":[]}}`)

	b.ReportAllocs()

	for b.Loop() {
		_ = encoding.RapidValidateSteamResponse(data)
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
