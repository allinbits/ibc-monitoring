// Package ibcpacket holds the IBC v2 packet type and the commitments both
// chains store for it (gno-realms p/aib/ibc/types/v0 commitment.gno; the same
// sha256 scheme eureka's router keeps keyed by keccak256(path)). The monitor
// uses them to authenticate a send record's packet against its commitment
// (docs/design.md §7.4) before trusting anything the record says.
package ibcpacket

import (
	"crypto/sha256"
	"encoding/binary"
)

// Payload is one application payload of a packet.
type Payload struct {
	SourcePort      string
	DestinationPort string
	Version         string
	Encoding        string
	Value           []byte
}

// Packet is an IBC v2 packet.
type Packet struct {
	Sequence          uint64
	SourceClient      string
	DestinationClient string
	TimeoutTimestamp  uint64
	Payloads          []Payload
}

// Path kinds of ICS24 commitment keys: clientID ‖ kind ‖ sequence (big-endian).
const (
	KindPacketCommitment byte = 0x01
	KindPacketReceipt    byte = 0x02
	KindAcknowledgement  byte = 0x03
)

// Path returns the commitment-store key of a packet on a client.
func Path(clientID string, kind byte, sequence uint64) []byte {
	out := make([]byte, 0, len(clientID)+9)
	out = append(out, clientID...)
	out = append(out, kind)
	return binary.BigEndian.AppendUint64(out, sequence)
}

func hashPayload(p Payload) []byte {
	var buf []byte
	for _, s := range []string{p.SourcePort, p.DestinationPort, p.Version, p.Encoding} {
		h := sha256.Sum256([]byte(s))
		buf = append(buf, h[:]...)
	}
	h := sha256.Sum256(p.Value)
	buf = append(buf, h[:]...)
	sum := sha256.Sum256(buf)
	return sum[:]
}

// Commit returns the packet commitment the source stores:
// sha256(0x02 ‖ sha256(destClient) ‖ sha256(timeout_be) ‖ sha256(‖ hashPayload(p))).
func Commit(p Packet) [32]byte {
	var buf []byte
	destHash := sha256.Sum256([]byte(p.DestinationClient))
	buf = append(buf, destHash[:]...)
	timeoutHash := sha256.Sum256(binary.BigEndian.AppendUint64(nil, p.TimeoutTimestamp))
	buf = append(buf, timeoutHash[:]...)
	var appBytes []byte
	for _, pl := range p.Payloads {
		appBytes = append(appBytes, hashPayload(pl)...)
	}
	appHash := sha256.Sum256(appBytes)
	buf = append(buf, appHash[:]...)
	buf = append([]byte{0x02}, buf...)
	return sha256.Sum256(buf)
}

// CommitAcknowledgement returns the acknowledgement commitment:
// sha256(0x02 ‖ sha256(ack_1) ‖ … ‖ sha256(ack_n)).
func CommitAcknowledgement(acks [][]byte) [32]byte {
	buf := []byte{0x02}
	for _, a := range acks {
		h := sha256.Sum256(a)
		buf = append(buf, h[:]...)
	}
	return sha256.Sum256(buf)
}

// IsTransfer reports whether the packet is an ICS20 transfer packet: every
// payload on port "transfer" (docs/design.md §7.0, packet scope). The payload
// itself is decoded by the caller that needs its fields.
func IsTransfer(p Packet) bool {
	if len(p.Payloads) == 0 {
		return false
	}
	for _, pl := range p.Payloads {
		if pl.SourcePort != "transfer" || pl.DestinationPort != "transfer" {
			return false
		}
	}
	return true
}

// IsGovernance reports whether the packet is a governance decision (gov →
// ibcgov) or an epoch notice (ibcgov → gov).
func IsGovernance(p Packet) bool {
	if len(p.Payloads) != 1 {
		return false
	}
	pl := p.Payloads[0]
	return (pl.SourcePort == "gov" && pl.DestinationPort == "ibcgov") || (pl.SourcePort == "ibcgov" && pl.DestinationPort == "gov")
}
