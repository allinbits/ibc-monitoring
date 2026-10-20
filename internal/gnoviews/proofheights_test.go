package gnoviews

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc/gnorpctest"
	"github.com/allinbits/ibc-monitoring/internal/ibcpacket"
)

// pbField encodes a length-delimited protobuf field; anyMsg an amino Any.
func pbField(num uint64, payload []byte) []byte {
	out := binary.AppendUvarint(nil, num<<3|2)
	out = binary.AppendUvarint(out, uint64(len(payload)))
	return append(out, payload...)
}

func anyMsg(typeURL string, value []byte) []byte {
	return append(pbField(1, []byte(typeURL)), pbField(2, value)...)
}

// The amino transaction fixture of internal/gnotx: two ethrelay calls
// (RecvPacket at proof slot 10000000, Acknowledgement at 10000001) for packet
// 7 client-0 → ethereum-1.
const relayTxHex = "0a81020a0a2f766d2e6d5f63616c6c12f2010a2867316a67386d74757475396b6868667763346e786d756863706674663070616a6468667673716635221b676e6f2e6c616e642f722f6169622f6962632f65746872656c61792a0a526563765061636b6574329c01414141414141414141416349593278705a5735304c54414b5a58526f5a584a6c645730744d5141414141427035694e5141516830636d467563325a6c63676830636d467563325a6c6367647059334d794d433078476d46776347787059324630615739754c336774633239736157527064486b7459574a704174367441414141414141414141414141414141414a69576741634241514542416749440aa2020a0a2f766d2e6d5f63616c6c1293020a2867316a67386d74757475396b6868667763346e786d756863706674663070616a6468667673716635221b676e6f2e6c616e642f722f6169622f6962632f65746872656c61792a0f41636b6e6f776c656467656d656e7432b801414141414141414141416349593278705a5735304c54414b5a58526f5a584a6c645730744d5141414141427035694e5141516830636d467563325a6c63676830636d467563325a6c6367647059334d794d433078476d46776347787059324630615739754c336774633239736157527064486b7459574a704174367441414141414141414141414141414141414a69576751455265794a795a584e31624851694f694a4255513039496e3048415145424151494341773d3d1214088084af5f120d313030303030303075676e6f74220766697874757265"

func TestPacketEventsProofHeights(t *testing.T) {
	f, err := gnorpctest.Load("testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's packet 7, as the core encodes it in its events.
	packet7 := ibcpacket.Packet{Sequence: 7, SourceClient: "client-0", DestinationClient: "ethereum-1", TimeoutTimestamp: 1776690000,
		Payloads: []ibcpacket.Payload{{SourcePort: "transfer", DestinationPort: "transfer", Version: "ics20-1", Encoding: "application/x-solidity-abi", Value: []byte{0xde, 0xad}}}}
	packet8 := packet7
	packet8.Sequence = 8
	attrs := func(p ibcpacket.Packet) map[string]string {
		return map[string]string{"packet_source_client": p.SourceClient, "packet_dest_client": p.DestinationClient, "packet_sequence": strconv.FormatUint(p.Sequence, 10), "encoded_packet_hex": hex.EncodeToString(ibcpacket.EncodeProto(p))}
	}
	// tx 2: a MsgRun (which the monitor does not decode) that received packet
	// 7, followed by an ethrelay.RecvPacket of the same packet, which the
	// core answered with a receipt no-op: the event's emitter is uncertain.
	relayTx, _ := hex.DecodeString(relayTxHex)
	runThenRelay := append(pbField(1, anyMsg("/vm.m_run", pbField(1, []byte("g1caller")))), relayTx...)
	// tx 3: two ethrelay.RecvPacket invocations of packet 7 (the second a
	// no-op): ambiguous as well.
	twice := append(append([]byte(nil), relayTx...), relayTx...)
	f.Blocks = map[string]gnorpctest.BlockFixture{
		"4100": {
			Txs: []string{relayTxHex, "ff", hex.EncodeToString(runThenRelay), hex.EncodeToString(twice)},
			Events: [][]gnorpctest.EventFixture{
				{ // tx 0: the relayer's receive and acknowledgement of packet 7
					{Type: "recv_packet", PkgPath: "gno.land/r/aib/ibc/core", Attrs: attrs(packet7)},
					{Type: "acknowledge_packet", PkgPath: "gno.land/r/aib/ibc/core", Attrs: attrs(packet7)},
					{Type: "fungible_token_packet", PkgPath: "gno.land/r/aib/ibc/apps/transfer", Attrs: map[string]string{}},
				},
				{ // tx 1: an undecodable transaction that emitted a receive of packet 8
					{Type: "recv_packet", PkgPath: "gno.land/r/aib/ibc/core", Attrs: attrs(packet8)},
				},
				{ // tx 2: the MsgRun emitted the event; the ethrelay call was a no-op
					{Type: "recv_packet", PkgPath: "gno.land/r/aib/ibc/core", Attrs: attrs(packet7)},
				},
				{ // tx 3: the first invocation emitted; the second was a no-op
					{Type: "recv_packet", PkgPath: "gno.land/r/aib/ibc/core", Attrs: attrs(packet7)},
				},
			},
		},
	}
	srv := gnorpctest.New(f)
	t.Cleanup(srv.Close)
	v := New(gnorpc.New(srv.URL), DefaultPaths)
	evs, err := v.PacketEvents(context.Background(), 4100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 5 {
		t.Fatalf("events %+v", evs)
	}
	recv, ack, orphan, mixed, double := evs[0], evs[1], evs[2], evs[3], evs[4]
	if !recv.Resolved || recv.Proof.Height != 10000000 || recv.Proof.Revision != 0 || recv.Sequence != 7 || recv.TxIndex != 0 || recv.MsgIndex != 0 || recv.Packet.Sequence != 7 {
		t.Fatalf("recv %+v", recv)
	}
	if !ack.Resolved || ack.Proof.Height != 10000001 || ack.Type != "acknowledge_packet" || ack.MsgIndex != 1 {
		t.Fatalf("ack %+v", ack)
	}
	// The event of an undecodable transaction stays unresolved with a reason;
	// nothing substitutes a height for it.
	if orphan.Resolved || orphan.Sequence != 8 || orphan.TxIndex != 1 || orphan.Reason == "" || orphan.Proof.Height != 0 {
		t.Fatalf("orphan %+v", orphan)
	}
	// A MsgRun receive followed by an ethrelay no-op: deferred, the no-op's
	// height is never taken (review finding 5).
	if mixed.Resolved || mixed.TxIndex != 2 || !strings.Contains(mixed.Reason, "deferred") || !strings.Contains(mixed.Reason, "does not decode") {
		t.Fatalf("mixed %+v", mixed)
	}
	// Two invocations of the same packet: ambiguous, deferred.
	if double.Resolved || double.TxIndex != 3 || !strings.Contains(double.Reason, "2 invocations") {
		t.Fatalf("double %+v", double)
	}
	// An event whose packet bytes differ from every invocation's (another
	// payload) is not attributed by its key alone.
	altered := packet7
	altered.Payloads[0].Value = []byte{0xbe, 0xef}
	f.Blocks["4101"] = gnorpctest.BlockFixture{Txs: []string{relayTxHex}, Events: [][]gnorpctest.EventFixture{{{Type: "recv_packet", PkgPath: "gno.land/r/aib/ibc/core", Attrs: attrs(altered)}}}}
	srv.Set("", "")
	evs, err = v.PacketEvents(context.Background(), 4101)
	if err != nil || len(evs) != 1 || evs[0].Resolved || !strings.Contains(evs[0].Reason, "no ethrelay invocation") {
		t.Fatalf("altered %+v %v", evs, err)
	}
	// A block with no packet event answers nothing without reading its txs.
	none, err := v.PacketEvents(context.Background(), 4200)
	if err != nil || len(none) != 0 {
		t.Fatalf("empty block: %v %v", none, err)
	}
}
