package gnoviews

import (
	"context"
	"testing"

	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc/gnorpctest"
)

// The amino transaction fixture of internal/gnotx: two ethrelay calls
// (RecvPacket at proof slot 10000000, Acknowledgement at 10000001) for packet
// 7 client-0 → ethereum-1.
const relayTxHex = "0a81020a0a2f766d2e6d5f63616c6c12f2010a2867316a67386d74757475396b6868667763346e786d756863706674663070616a6468667673716635221b676e6f2e6c616e642f722f6169622f6962632f65746872656c61792a0a526563765061636b6574329c01414141414141414141416349593278705a5735304c54414b5a58526f5a584a6c645730744d5141414141427035694e5141516830636d467563325a6c63676830636d467563325a6c6367647059334d794d433078476d46776347787059324630615739754c336774633239736157527064486b7459574a704174367441414141414141414141414141414141414a69576741634241514542416749440aa2020a0a2f766d2e6d5f63616c6c1293020a2867316a67386d74757475396b6868667763346e786d756863706674663070616a6468667673716635221b676e6f2e6c616e642f722f6169622f6962632f65746872656c61792a0f41636b6e6f776c656467656d656e7432b801414141414141414141416349593278705a5735304c54414b5a58526f5a584a6c645730744d5141414141427035694e5141516830636d467563325a6c63676830636d467563325a6c6367647059334d794d433078476d46776347787059324630615739754c336774633239736157527064486b7459574a704174367441414141414141414141414141414141414a69576751455265794a795a584e31624851694f694a4255513039496e3048415145424151494341773d3d1214088084af5f120d313030303030303075676e6f74220766697874757265"

func TestPacketEventsProofHeights(t *testing.T) {
	f, err := gnorpctest.Load("testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	attrs := func(seq string) map[string]string {
		return map[string]string{"packet_source_client": "client-0", "packet_dest_client": "ethereum-1", "packet_sequence": seq}
	}
	f.Blocks = map[string]gnorpctest.BlockFixture{
		"4100": {
			Txs: []string{relayTxHex, "ff"},
			Events: [][]gnorpctest.EventFixture{
				{ // tx 0: the relayer's receive and acknowledgement of packet 7
					{Type: "recv_packet", PkgPath: "gno.land/r/aib/ibc/core", Attrs: attrs("7")},
					{Type: "acknowledge_packet", PkgPath: "gno.land/r/aib/ibc/core", Attrs: attrs("7")},
					{Type: "fungible_token_packet", PkgPath: "gno.land/r/aib/ibc/apps/transfer", Attrs: map[string]string{}},
				},
				{ // tx 1: an undecodable transaction that emitted a receive of packet 8
					{Type: "recv_packet", PkgPath: "gno.land/r/aib/ibc/core", Attrs: attrs("8")},
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
	if len(evs) != 3 {
		t.Fatalf("events %+v", evs)
	}
	recv, ack, orphan := evs[0], evs[1], evs[2]
	if !recv.Resolved || recv.Proof.Height != 10000000 || recv.Proof.Revision != 0 || recv.Sequence != 7 || recv.TxIndex != 0 || recv.Packet.Sequence != 7 {
		t.Fatalf("recv %+v", recv)
	}
	if !ack.Resolved || ack.Proof.Height != 10000001 || ack.Type != "acknowledge_packet" {
		t.Fatalf("ack %+v", ack)
	}
	// The event of an undecodable transaction stays unresolved with a reason;
	// nothing substitutes a height for it.
	if orphan.Resolved || orphan.Sequence != 8 || orphan.TxIndex != 1 || orphan.Reason == "" || orphan.Proof.Height != 0 {
		t.Fatalf("orphan %+v", orphan)
	}
	// A block with no packet event answers nothing without reading its txs.
	none, err := v.PacketEvents(context.Background(), 4200)
	if err != nil || len(none) != 0 {
		t.Fatalf("empty block: %v %v", none, err)
	}
}
