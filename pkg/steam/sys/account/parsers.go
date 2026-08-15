// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package account

import (
	"encoding/binary"
	"errors"

	"github.com/lemon4ksan/g-man/pkg/steam/encoding/bvdf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

func parseVACBans(payload []byte) (*VACBansEvent, error) {
	if len(payload) < 4 {
		return nil, errors.New("vac ban status payload too short")
	}

	numBans := binary.LittleEndian.Uint32(payload[0:4])
	offset := 4
	appIDs := make([]uint32, 0)
	ranges := make([][2]uint32, 0)

	for range numBans {
		if offset+12 > len(payload) {
			break
		}

		rangeStart := binary.LittleEndian.Uint32(payload[offset : offset+4])
		rangeEnd := binary.LittleEndian.Uint32(payload[offset+4 : offset+8])
		offset += 12

		if rangeEnd < rangeStart {
			rangeStart, rangeEnd = rangeEnd, rangeStart
		}

		ranges = append(ranges, [2]uint32{rangeStart, rangeEnd})

		for j := rangeStart; j <= rangeEnd; j++ {
			appIDs = append(appIDs, j)
		}
	}

	return &VACBansEvent{
		NumBans: numBans,
		AppIDs:  appIDs,
		Ranges:  ranges,
	}, nil
}

func parseGuestPasses(payload []byte) ([]map[string]any, error) {
	if len(payload) < 12 {
		return nil, errors.New("update guest passes list payload too short")
	}

	eresult := binary.LittleEndian.Uint32(payload[0:4])
	if enums.EResult(eresult) != enums.EResult_OK {
		return nil, nil
	}

	countToGive := binary.LittleEndian.Uint32(payload[4:8])
	countToRedeem := binary.LittleEndian.Uint32(payload[8:12])

	offset := 12
	for range countToGive {
		var discard map[string]any
		if err := bvdf.UnmarshalOffset(payload, &offset, &discard); err != nil {
			return nil, err
		}
	}

	gifts := make([]map[string]any, 0, countToRedeem)
	for range countToRedeem {
		var gift map[string]any
		if err := bvdf.UnmarshalOffset(payload, &offset, &gift); err != nil {
			return nil, err
		}

		if msgObj, ok := gift["MessageObject"].(map[string]any); ok {
			gift = msgObj
		}

		gifts = append(gifts, gift)
	}

	return gifts, nil
}
