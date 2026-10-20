package gnoviews

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/allinbits/ibc-monitoring/internal/ethrelaywire"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnotx"
	"github.com/allinbits/ibc-monitoring/internal/ibcpacket"
)

// EthRelayPath is the relayer's realm (gno-eth-ibc relayer: EthRelayRealm).
const EthRelayPath = "gno.land/r/aib/ibc/ethrelay"

// PacketEvent is a packet event of a block with the proof height its
// transaction carried, when the attribution is certain (docs/design.md §4.1,
// proof heights).
type PacketEvent struct {
	Height   int64
	TxIndex  int
	Type     string // recv_packet | acknowledge_packet | timeout_packet
	Source   string
	Dest     string
	Sequence uint64
	// Packet is the event's own packet (the core's encoded_packet_hex).
	Packet ibcpacket.Packet
	// Proof is the proof height of the one invocation that produced the
	// event; MsgIndex its message index. Resolved is false when the
	// attribution is uncertain, in which case the comparison is deferred and
	// the client's head never substituted; Reason says why.
	Proof    ethrelaywire.Height
	MsgIndex int
	Resolved bool
	Reason   string
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

// invocation is one decoded ethrelay call of a transaction.
type invocation struct {
	msgIndex int
	kind     ethrelaywire.Kind
	msg      ethrelaywire.Message
	commit   [32]byte
}

// PacketEvents reads a block's packet events and attributes each to the
// invocation that produced it, certain only when: the event's packet bytes
// and the invocation's packet have the same commitment, exactly one
// invocation of the event's kind in the transaction names that packet, and
// the transaction holds no message the monitor cannot decode. A second
// invocation for the same packet (which the core answers with a receipt
// no-op and no event) or an unknown message could have been the emitter,
// so such a transaction is deferred whole.
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
	perTx := map[int][]invocation{}
	deferred := map[int]string{}
	for i, raw := range txs {
		tx, err := gnotx.Decode(raw)
		if err != nil {
			deferred[i] = "transaction undecodable: " + err.Error()
			continue
		}
		if len(tx.Other) > 0 {
			deferred[i] = fmt.Sprintf("%d message(s) of a kind the monitor does not decode (indexes %v) could have emitted the event", len(tx.Other), tx.Other)
		}
		for _, c := range tx.Calls {
			kind, ok := funcKinds[c.Func]
			if c.PkgPath != EthRelayPath || !ok {
				continue
			}
			if len(c.Args) != 1 {
				deferred[i] = fmt.Sprintf("ethrelay call %d with %d arguments", c.Index, len(c.Args))
				continue
			}
			b, err := base64.StdEncoding.DecodeString(c.Args[0])
			if err != nil {
				deferred[i] = fmt.Sprintf("ethrelay call %d: argument is not base64", c.Index)
				continue
			}
			m, err := ethrelaywire.Decode(kind, b)
			if err != nil {
				deferred[i] = fmt.Sprintf("ethrelay call %d: %v", c.Index, err)
				continue
			}
			perTx[i] = append(perTx[i], invocation{msgIndex: c.Index, kind: kind, msg: m, commit: ibcpacket.Commit(m.Packet)})
		}
	}
	out := make([]PacketEvent, 0, len(wanted))
	for _, e := range wanted {
		pe := PacketEvent{Height: height, TxIndex: e.TxIndex, Type: e.Type, Source: e.Attrs["packet_source_client"], Dest: e.Attrs["packet_dest_client"], MsgIndex: -1}
		if _, err := fmt.Sscanf(e.Attrs["packet_sequence"], "%d", &pe.Sequence); err != nil {
			pe.Reason = "event without a sequence"
			out = append(out, pe)
			continue
		}
		raw, err := hex.DecodeString(e.Attrs["encoded_packet_hex"])
		if err != nil {
			pe.Reason = "event without packet bytes"
			out = append(out, pe)
			continue
		}
		if pe.Packet, err = ibcpacket.DecodeProto(raw); err != nil {
			pe.Reason = "event packet undecodable: " + err.Error()
			out = append(out, pe)
			continue
		}
		if r, ok := deferred[e.TxIndex]; ok {
			pe.Reason = "deferred: " + r
			out = append(out, pe)
			continue
		}
		want := ibcpacket.Commit(pe.Packet)
		var matches []invocation
		for _, inv := range perTx[e.TxIndex] {
			if inv.kind == eventKinds[e.Type] && inv.commit == want && inv.msg.Packet.Sequence == pe.Sequence && inv.msg.Packet.SourceClient == pe.Source {
				matches = append(matches, inv)
			}
		}
		switch len(matches) {
		case 1:
			pe.Proof, pe.MsgIndex, pe.Resolved = matches[0].msg.ProofHeight, matches[0].msgIndex, true
		case 0:
			pe.Reason = "no ethrelay invocation of this packet in its transaction"
		default:
			pe.Reason = fmt.Sprintf("deferred: %d invocations name this packet (a later one is a receipt no-op); the emitter is uncertain", len(matches))
		}
		out = append(out, pe)
	}
	return out, nil
}
