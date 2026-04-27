// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/network"
)

func (s *Socket) processSingle(msg io.Reader) {
	packet, err := protocol.ParsePacket(msg)
	if err != nil {
		s.logger.Error("Failed to parse packet", log.Err(err))
		return
	}

	select {
	case s.msgCh <- packet:
	case <-s.done:
	default:
		s.logger.Warn("Packet dropped: msgCh saturated",
			log.EMsg(packet.EMsg),
			log.Int("chan_cap", cap(s.msgCh)),
			log.Int("chan_len", len(s.msgCh)))

		if packet.GetTargetJobID() != protocol.NoJob {
			select {
			case s.msgCh <- packet:
			case <-time.After(100 * time.Millisecond):
				s.logger.Error("Job response dropped due to congestion", log.JobID(packet.GetTargetJobID()))
			}
		}
	}
}

func (s *Socket) handleService(packet *protocol.Packet) {
	header, ok := packet.Header.(*protocol.MsgHdrProtoBuf)
	if !ok {
		s.logger.Warn("Received ServiceMethod with non-proto header")
		return
	}

	methodName := header.Proto.GetTargetJobName()

	s.serviceHandlersMu.RLock()
	handler, ok := s.serviceHandlers[methodName]
	s.serviceHandlersMu.RUnlock()

	if ok {
		handler(packet)
	} else {
		s.logger.Debug("Unhandled ServiceMethod", log.String("method", methodName))
	}
}

func (s *Socket) handleJobResponse(packet *protocol.Packet) bool {
	targetID := packet.GetTargetJobID()
	if targetID == protocol.NoJob {
		return false
	}

	var err error
	if packet.EMsg == enums.EMsg_DestJobFailed {
		err = ErrDestJobFailed
	}

	return s.jobManager.Resolve(targetID, packet, err)
}

// handleMulti is the built-in handler for EMsg_Multi, which contains
// multiple nested or compressed packets. This handler automatically
// flattens nested packets and re-injects them into the processing queue.
// The order of packets within a Multi-message is preserved during re-injection.
func (s *Socket) handleMulti(packet *protocol.Packet) {
	msg := &pb.CMsgMulti{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		s.logger.Error("Failed to unmarshal CMsgMulti",
			log.Err(err),
			log.Int("payload_len", len(packet.Payload)),
		)

		return
	}

	payload := msg.GetMessageBody()
	if size := msg.GetSizeUnzipped(); size > 0 {
		var err error
		if payload, err = s.decompressPayload(payload, int64(size)); err != nil {
			s.logger.Error("Decompression failed",
				log.Int("compressed_size", len(msg.GetMessageBody())),
				log.Uint32("unzipped_size", size),
			)

			return
		}
	}

	var dispatched int

	ctx := s.getContext()

	reader := bytes.NewReader(payload)
	for reader.Len() > 0 {
		var subSize uint32
		if err := binary.Read(reader, binary.LittleEndian, &subSize); err != nil {
			break
		}

		pkt, err := protocol.ParsePacket(io.LimitReader(reader, int64(subSize)))
		if err != nil {
			continue
		}

		select {
		case s.msgCh <- pkt:
			dispatched++
		case <-ctx.Done():
			return
		default:
			s.logger.Warn("Multi sub-packet dropped: msgCh full",
				log.Int("dispatched", dispatched))
			return
		}
	}
}

// decompressPayload handles GZIP decompression for Steam Multi-messages.
// It enforces a maximum unzipped size to prevent memory exhaustion (Zip Bombs).
func (s *Socket) decompressPayload(data network.NetMessage, unzippedSize int64) ([]byte, error) {
	if unzippedSize > 100*1024*1024 { // 100MB limit to prevent OOM attacks
		return nil, ErrDecompressionLimit
	}

	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	out := make([]byte, unzippedSize)
	if _, err := io.ReadFull(gr, out); err != nil {
		return nil, fmt.Errorf("read full decompressed payload: %w", err)
	}

	return out, nil
}
