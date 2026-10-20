package ibcpacket

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The core realm's packet encoding (the `encoded_packet_hex` event
// attribute; gno-eth-ibc relayer ibc.DecodePacket): protobuf with Packet
// {1 sequence varint, 2 source_client, 3 destination_client, 4
// timeout_timestamp varint, 5 payloads repeated} and Payload {1 source_port,
// 2 destination_port, 3 version, 4 encoding, 5 value}.

var errProto = errors.New("ibcpacket: malformed packet bytes")

// MaxProtoPayloads bounds the payloads a packet may carry (one on an
// Ethereum route, ADR §9).
const MaxProtoPayloads = 16

type protoField struct {
	num, wire, v uint64
	b            []byte
}

func protoFields(b []byte, max int) ([]protoField, error) {
	var out []protoField
	for len(b) > 0 {
		if len(out) >= max {
			return nil, fmt.Errorf("%w: more than %d fields", errProto, max)
		}
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errProto
		}
		b = b[n:]
		f := protoField{num: tag >> 3, wire: tag & 7}
		switch f.wire {
		case 0:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, errProto
			}
			f.v, b = v, b[n:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || l > uint64(len(b)-n) {
				return nil, errProto
			}
			f.b, b = b[n:n+int(l)], b[n+int(l):]
		default:
			return nil, fmt.Errorf("%w: wire type %d", errProto, f.wire)
		}
		out = append(out, f)
	}
	return out, nil
}

// DecodeProto decodes the core's protobuf packet.
func DecodeProto(b []byte) (Packet, error) {
	fs, err := protoFields(b, 4+MaxProtoPayloads)
	if err != nil {
		return Packet{}, err
	}
	var p Packet
	for _, f := range fs {
		switch {
		case f.num == 1 && f.wire == 0:
			p.Sequence = f.v
		case f.num == 2 && f.wire == 2:
			p.SourceClient = string(f.b)
		case f.num == 3 && f.wire == 2:
			p.DestinationClient = string(f.b)
		case f.num == 4 && f.wire == 0:
			p.TimeoutTimestamp = f.v
		case f.num == 5 && f.wire == 2:
			pfs, err := protoFields(f.b, 5)
			if err != nil {
				return Packet{}, err
			}
			var pl Payload
			for _, pf := range pfs {
				if pf.wire != 2 {
					continue
				}
				switch pf.num {
				case 1:
					pl.SourcePort = string(pf.b)
				case 2:
					pl.DestinationPort = string(pf.b)
				case 3:
					pl.Version = string(pf.b)
				case 4:
					pl.Encoding = string(pf.b)
				case 5:
					pl.Value = append([]byte(nil), pf.b...)
				}
			}
			p.Payloads = append(p.Payloads, pl)
		}
	}
	return p, nil
}

// EncodeProto encodes a packet as the core does (tests and fixtures).
func EncodeProto(p Packet) []byte {
	var out []byte
	out = binary.AppendUvarint(out, 1<<3)
	out = binary.AppendUvarint(out, p.Sequence)
	out = appendBytes(out, 2, []byte(p.SourceClient))
	out = appendBytes(out, 3, []byte(p.DestinationClient))
	out = binary.AppendUvarint(out, 4<<3)
	out = binary.AppendUvarint(out, p.TimeoutTimestamp)
	for _, pl := range p.Payloads {
		var inner []byte
		inner = appendBytes(inner, 1, []byte(pl.SourcePort))
		inner = appendBytes(inner, 2, []byte(pl.DestinationPort))
		inner = appendBytes(inner, 3, []byte(pl.Version))
		inner = appendBytes(inner, 4, []byte(pl.Encoding))
		inner = appendBytes(inner, 5, pl.Value)
		out = appendBytes(out, 5, inner)
	}
	return out
}

func appendBytes(out []byte, num uint64, b []byte) []byte {
	out = binary.AppendUvarint(out, num<<3|2)
	out = binary.AppendUvarint(out, uint64(len(b)))
	return append(out, b...)
}
