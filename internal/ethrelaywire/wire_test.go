package ethrelaywire

import (
	"encoding/hex"
	"testing"
)

// Fixtures generated with the relayer's own encoder (gno-eth-ibc
// relayer/ethgno EncodePacketMsg at 72a41de): packet 7 client-0 → ethereum-1,
// timeout 1776690000, one transfer payload with value 0xdead, proof slot
// 10000000 (receive) and 10000001 with one ack (acknowledgement), and a
// StorageProof{AccountProof: [01], StorageProof: [0203], Value: 04}.
const (
	recvHex = "000000000000000708636c69656e742d300a657468657265756d2d310000000069e6235001087472616e73666572087472616e736665720769637332302d311a6170706c69636174696f6e2f782d736f6c69646974792d61626902dead000000000000000000000000009896800701010101020203"
	ackHex  = "000000000000000708636c69656e742d300a657468657265756d2d310000000069e6235001087472616e73666572087472616e736665720769637332302d311a6170706c69636174696f6e2f782d736f6c69646974792d61626902dead0000000000000000000000000098968101117b22726573756c74223a2241513d3d227d0701010101020203"
)

func TestDecodeRelayerFixtures(t *testing.T) {
	recv, _ := hex.DecodeString(recvHex)
	m, err := Decode(KindRecvPacket, recv)
	if err != nil {
		t.Fatal(err)
	}
	p := m.Packet
	if p.Sequence != 7 || p.SourceClient != "client-0" || p.DestinationClient != "ethereum-1" || p.TimeoutTimestamp != 1776690000 {
		t.Fatalf("packet %+v", p)
	}
	if len(p.Payloads) != 1 || p.Payloads[0].SourcePort != "transfer" || p.Payloads[0].Version != "ics20-1" || p.Payloads[0].Encoding != "application/x-solidity-abi" || hex.EncodeToString(p.Payloads[0].Value) != "dead" {
		t.Fatalf("payload %+v", p.Payloads)
	}
	if m.ProofHeight.Revision != 0 || m.ProofHeight.Height != 10000000 {
		t.Fatalf("proof height %+v", m.ProofHeight)
	}
	if m.Acks != nil || len(m.Proof) != 7 {
		t.Fatalf("acks %v proof %x", m.Acks, m.Proof)
	}

	ack, _ := hex.DecodeString(ackHex)
	m, err = Decode(KindAcknowledgement, ack)
	if err != nil {
		t.Fatal(err)
	}
	if m.ProofHeight.Height != 10000001 || len(m.Acks) != 1 || string(m.Acks[0]) != `{"result":"AQ=="}` {
		t.Fatalf("ack %+v", m)
	}
	// Reading an acknowledgement as a receive mis-parses and fails closed.
	if _, err := Decode(KindRecvPacket, ack); err == nil {
		t.Fatal("ack decoded as a receive")
	}
	// Truncated and trailing inputs fail.
	if _, err := Decode(KindRecvPacket, recv[:len(recv)-1]); err == nil {
		t.Fatal("truncated accepted")
	}
	if _, err := Decode(KindRecvPacket, append(append([]byte(nil), recv...), 0)); err == nil {
		t.Fatal("trailing accepted")
	}
	// A hostile list length never allocates past the message.
	hostile := append([]byte(nil), recv[:8+1+8+1+10+8]...)
	hostile = append(hostile, 0xff, 0xff, 0xff, 0xff, 0x0f)
	if _, err := Decode(KindRecvPacket, hostile); err == nil {
		t.Fatal("hostile length accepted")
	}
}
