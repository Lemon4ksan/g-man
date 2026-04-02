package socket

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/network"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"google.golang.org/protobuf/proto"
)

func (s *Socket) processSingle(msg io.Reader) {
	packet, err := protocol.ParsePacket(msg)
	if err != nil {
		s.logger.Error("Failed to parse packet", log.Err(err))
		return
	}

	ctx := s.getContext()
	select {
	case s.msgCh <- packet:
	case <-ctx.Done():
	case <-s.done:
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
	if packet.EMsg == protocol.EMsg_DestJobFailed {
		err = &SteamError{
			EMsg:    packet.EMsg,
			Message: "destination job failed on Steam side",
		}
	}

	return s.jobManager.Resolve(targetID, packet, err)
}

// handleMulti is the built-in handler for EMsg_Multi, which contains multiple
// nested or compressed packets.
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

		s.routePacket(pkt)
	}
}

// unzip handles GZIP decompression for Steam Multi-messages.
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
