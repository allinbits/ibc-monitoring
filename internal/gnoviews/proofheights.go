package gnoviews

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/allinbits/ibc-monitoring/internal/ethrelaywire"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnotx"
	"github.com/allinbits/ibc-monitoring/internal/ibcpacket"
)

// EthRelayPath is the relayer's realm (gno-eth-ibc relayer: EthRelayRealm).
const EthRelayPath = "gno.land/r/aib/ibc/ethrelay"

// PacketEvent is a packet event of a block with the proof height its
// transaction carried, when the monitor could decode it (docs/design.md
// §4.1, proof heights).
type PacketEvent struct {
	Height   int64
	TxIndex  int
	Type     string // recv_packet | acknowledge_packet | timeout_packet
	Source   string
	Dest     string
	Sequence uint64
	// Proof is the proof height of the transaction's matching message;
	// Resolved is false when none could be decoded or matched, in which case
	// the comparison is deferred and the client's head never substituted.
	Proof    ethrelaywire.Height
	Resolved bool
	Reason   string
	Packet   ibcpacket.Packet
}

var eventKinds = map[string]ethrelaywire.Kind{
	"recv_packet":        ethrelaywire.KindRecvPacket,
	"acknowledge_packet": ethrelaywire.KindAcknowledgement,
	"timeout_packet":     ethrelaywire.KindTimeout,
}

var funcKinds = map[string]ethrelaywire.Kind{
	"RecvPacket":      ethrelaywire.KindRecvPacket,
	"Acknowledgement": ethrelaywire.KindAcknowledgement,
	"Timeout":         ethrelaywire.KindTimeout,
}

// PacketEvents reads a block's packet events and associates each with the
// proof height of the ethrelay message in the same transaction that names
// the same packet (source client and sequence). A transaction the decoders
// cannot read leaves its events unresolved.
func (v *Views) PacketEvents(ctx context.Context, height int64) ([]PacketEvent, error) {
	events, err := v.rpc.BlockResults(ctx, height)
	if err != nil {
		return nil, err
	}
	var wanted []gnorpc.Event
	for _, e := range events {
		if _, ok := eventKinds[e.Type]; ok && e.PkgPath == v.paths.Core {
			wanted = append(wanted, e)
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	txs, err := v.rpc.BlockTxs(ctx, height)
	if err != nil {
		return nil, err
	}
	// Decode every ethrelay message per transaction once.
	type decoded struct {
		kind ethrelaywire.Kind
		msg  ethrelaywire.Message
	}
	perTx := map[int][]decoded{}
	txErr := map[int]string{}
	for i, raw := range txs {
		tx, err := gnotx.Decode(raw)
		if err != nil {
			txErr[i] = "transaction undecodable: " + err.Error()
			continue
		}
		if tx.Other > 0 {
			txErr[i] = fmt.Sprintf("%d message(s) of a kind the monitor does not decode", tx.Other)
		}
		for _, c := range tx.Calls {
			kind, ok := funcKinds[c.Func]
			if c.PkgPath != EthRelayPath || !ok || len(c.Args) != 1 {
				continue
			}
			b, err := base64.StdEncoding.DecodeString(c.Args[0])
			if err != nil {
				txErr[i] = "ethrelay argument is not base64"
				continue
			}
			m, err := ethrelaywire.Decode(kind, b)
			if err != nil {
				txErr[i] = "ethrelay message undecodable: " + err.Error()
				continue
			}
			perTx[i] = append(perTx[i], decoded{kind: kind, msg: m})
		}
	}
	out := make([]PacketEvent, 0, len(wanted))
	for _, e := range wanted {
		pe := PacketEvent{Height: height, TxIndex: e.TxIndex, Type: e.Type, Source: e.Attrs["packet_source_client"], Dest: e.Attrs["packet_dest_client"]}
		if _, err := fmt.Sscanf(e.Attrs["packet_sequence"], "%d", &pe.Sequence); err != nil {
			pe.Reason = "event without a sequence"
			out = append(out, pe)
			continue
		}
		kind := eventKinds[e.Type]
		for _, d := range perTx[e.TxIndex] {
			if d.kind == kind && d.msg.Packet.SourceClient == pe.Source && d.msg.Packet.DestinationClient == pe.Dest && d.msg.Packet.Sequence == pe.Sequence {
				pe.Proof, pe.Packet, pe.Resolved = d.msg.ProofHeight, d.msg.Packet, true
				break
			}
		}
		if !pe.Resolved {
			pe.Reason = "no ethrelay message of this packet in its transaction"
			if r, ok := txErr[e.TxIndex]; ok {
				pe.Reason = r
			}
		}
		out = append(out, pe)
	}
	return out, nil
}
