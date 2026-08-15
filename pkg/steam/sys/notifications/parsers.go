// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"encoding/binary"
	"strconv"
)

func parseMarketingMessages(payload []byte) (*MarketingMessagesEvent, error) {
	if len(payload) < 8 {
		return &MarketingMessagesEvent{}, nil
	}

	timestamp := binary.LittleEndian.Uint32(payload[0:4])
	count := binary.LittleEndian.Uint32(payload[4:8])

	offset := 8
	messages := make([]MarketingMessage, 0, count)

	for range count {
		if offset+4 > len(payload) {
			break
		}

		subLen := binary.LittleEndian.Uint32(payload[offset : offset+4])
		offset += 4

		if offset+int(subLen) > len(payload) {
			break
		}

		subPayload := payload[offset : offset+int(subLen)]

		msg := parseMarketingMessage(subPayload)
		if msg != nil {
			messages = append(messages, *msg)
		}

		offset += int(subLen)
	}

	return &MarketingMessagesEvent{
		Timestamp: int64(timestamp),
		Messages:  messages,
	}, nil
}

func parseMarketingMessage(payload []byte) *MarketingMessage {
	if len(payload) < 12 {
		return nil
	}

	msgID := strconv.FormatUint(uint64(payload[0])|uint64(payload[1])<<8|
		uint64(payload[2])<<16|uint64(payload[3])<<24|
		uint64(payload[4])<<32|uint64(payload[5])<<40|
		uint64(payload[6])<<48|uint64(payload[7])<<56, 10)

	offset := 8
	urlEnd := -1

	for i := offset; i < len(payload); i++ {
		if payload[i] == 0 {
			urlEnd = i
			break
		}
	}

	if urlEnd == -1 {
		return nil
	}

	url := string(payload[offset:urlEnd])
	offset = urlEnd + 1

	if offset+4 > len(payload) {
		return nil
	}

	flags := binary.LittleEndian.Uint32(payload[offset : offset+4])

	return &MarketingMessage{
		ID:    msgID,
		URL:   url,
		Flags: flags,
	}
}
