// Package ethrelaywire decodes the messages the relayer submits to
// gno.land/r/aib/ibc/ethrelay (RecvPacket, Acknowledgement, Timeout): a
// p/aib/encoding/wire/v0 message carrying the packet, the proof height and
// the proof (docs/design.md §4.1, proof heights). The monitor needs the
// packet and the proof height; the proof bytes are kept opaque.
//
// Layout (relayer ethgno.EncodePacketMsg): Uint(sequence) Str(sourceClient)
// Str(destinationClient) Uint(timeout) Uvarint(n) [Str(sourcePort)
// Str(destinationPort) Str(version) Str(encoding) Blob(value)]×n
// Uint(revision) Uint(height) [Uvarint(m) Blob(ack)×m: acknowledgements only]
// Blob(proof). Uint is 8 bytes big-endian; Blob and Str are uvarint-length
// prefixed.
package ethrelaywire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/allinbits/ibc-monitoring/internal/ibcpacket"
)

// Height is an IBC height: for an Ethereum client on gno.land the revision
// is 0 and the height a beacon slot.
type Height struct {
	Revision uint64
	Height   uint64
}

// Message is a decoded relay message.
type Message struct {
	Packet      ibcpacket.Packet
	ProofHeight Height
	// Acks is set for an Acknowledgement message.
	Acks [][]byte
	// Proof is the opaque MembershipProof blob.
	Proof []byte
}

// Kind names the ethrelay function a message was sent to.
type Kind string

const (
	KindRecvPacket      Kind = "RecvPacket"
	KindAcknowledgement Kind = "Acknowledgement"
	KindTimeout         Kind = "Timeout"
)

var (
	errShort    = errors.New("ethrelaywire: message too short")
	errLength   = errors.New("ethrelaywire: length past the end of the message")
	errTrailing = errors.New("ethrelaywire: trailing bytes")
)

type reader struct {
	buf []byte
	off int
	err error
}

func (r *reader) remaining() int { return len(r.buf) - r.off }

func (r *reader) uint() uint64 {
	if r.err != nil {
		return 0
	}
	if r.remaining() < 8 {
		r.err = errShort
		return 0
	}
	v := binary.BigEndian.Uint64(r.buf[r.off:])
	r.off += 8
	return v
}

func (r *reader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.buf[r.off:])
	if n <= 0 {
		r.err = errShort
		return 0
	}
	r.off += n
	return v
}

func (r *reader) blob() []byte {
	n := r.uvarint()
	if r.err != nil {
		return nil
	}
	if n > uint64(r.remaining()) {
		r.err = errLength
		return nil
	}
	out := r.buf[r.off : r.off+int(n)]
	r.off += int(n)
	return out
}

func (r *reader) str() string { return string(r.blob()) }

// count reads a list length no larger than the remaining bytes (each
// element takes at least one), as the realm does.
func (r *reader) count() int {
	n := r.uvarint()
	if r.err != nil {
		return 0
	}
	if n > uint64(r.remaining()) {
		r.err = errLength
		return 0
	}
	return int(n)
}

// Decode decodes a message of the given kind.
func Decode(kind Kind, msg []byte) (Message, error) {
	r := &reader{buf: msg}
	var m Message
	m.Packet.Sequence = r.uint()
	m.Packet.SourceClient = r.str()
	m.Packet.DestinationClient = r.str()
	m.Packet.TimeoutTimestamp = r.uint()
	n := r.count()
	for i := 0; i < n && r.err == nil; i++ {
		m.Packet.Payloads = append(m.Packet.Payloads, ibcpacket.Payload{
			SourcePort:      r.str(),
			DestinationPort: r.str(),
			Version:         r.str(),
			Encoding:        r.str(),
			Value:           append([]byte(nil), r.blob()...),
		})
	}
	m.ProofHeight.Revision = r.uint()
	m.ProofHeight.Height = r.uint()
	if kind == KindAcknowledgement {
		k := r.count()
		for i := 0; i < k && r.err == nil; i++ {
			m.Acks = append(m.Acks, append([]byte(nil), r.blob()...))
		}
	}
	m.Proof = append([]byte(nil), r.blob()...)
	if r.err != nil {
		return Message{}, r.err
	}
	if r.remaining() != 0 {
		return Message{}, errTrailing
	}
	if kind != KindRecvPacket && kind != KindAcknowledgement && kind != KindTimeout {
		return Message{}, fmt.Errorf("ethrelaywire: unknown kind %q", kind)
	}
	return m, nil
}
