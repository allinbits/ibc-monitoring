package ethviews

import (
	"context"
	"fmt"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/model"
)

// Window is IBCAdmin's buffering window (W = 64, ADR §9).
const Window = 64

// Delays per kind (ADR §7.6): 24 h for an unpause, 7 days otherwise.
const (
	DelayUnpause = 24 * 3600
	DelayDefault = 7 * 24 * 3600
)

type decision struct {
	Hash          [32]byte
	Kind          uint8
	State         uint8
	Bad           bool
	RecordedTime  uint64
	RecordedBlock uint64
	Params        []byte
}

type hold struct {
	Start  uint64
	End    uint64
	Merged bool
}

type recoveryConfig struct {
	Delay           uint64
	Threshold       uint8
	CancelThreshold uint8
	Silence         uint64
	HoldPeriod      uint64
}

// Governance reads IBCAdmin's ledger and guardian state at the pinned block.
func (v *Views) Governance(ctx context.Context, b ethrpc.Block) (model.EthGovernance, error) {
	g := model.EthGovernance{Provenance: v.provenance(b), Decisions: []model.EthDecision{}, Buffered: []uint64{}, Flags: []model.EthFlag{}, Holds: []model.EthHold{}}
	addr := v.addrs.Admin
	var err error
	if g.Epoch, err = one[uint32](ctx, v, addr, v.admin, "epoch", b); err != nil {
		return g, err
	}
	if g.LastSeq, err = one[uint64](ctx, v, addr, v.admin, "lastSeq", b); err != nil {
		return g, err
	}
	if g.GovClient, err = one[string](ctx, v, addr, v.admin, "govClient", b); err != nil {
		return g, err
	}
	if g.GnoChainID, err = one[string](ctx, v, addr, v.admin, "gnoChainId", b); err != nil {
		return g, err
	}
	lrt, err := one[uint64](ctx, v, addr, v.admin, "lastRecordTime", b)
	if err != nil {
		return g, err
	}
	g.LastRecordTime = int64(lrt)
	if g.SetupDone, err = one[bool](ctx, v, addr, v.admin, "setupDone", b); err != nil {
		return g, err
	}
	keys, err := one[[6]common.Address](ctx, v, addr, v.admin, "keys", b)
	if err != nil {
		return g, err
	}
	for _, k := range keys {
		g.Keys = append(g.Keys, k.Hex())
	}
	out, err := v.call(ctx, addr, v.admin, "flags", b)
	if err != nil {
		return g, err
	}
	if g.Flags, err = toFlags(out[0], keys); err != nil {
		return g, err
	}
	out, err = v.call(ctx, addr, v.admin, "hold", b)
	if err != nil {
		return g, err
	}
	if g.Hold, err = toHold(out[0]); err != nil {
		return g, err
	}
	out, err = v.call(ctx, addr, v.admin, "holds", b)
	if err != nil {
		return g, err
	}
	hs, ok := gethabi.ConvertType(out[0], new([]hold)).(*[]hold)
	if !ok {
		return g, fmt.Errorf("holds: unexpected type %T", out[0])
	}
	for _, h := range *hs {
		g.Holds = append(g.Holds, model.EthHold{Start: int64(h.Start), End: int64(h.End), Merged: h.Merged})
	}
	if g.ConflictBlock, err = one[uint64](ctx, v, addr, v.admin, "conflictBlock", b); err != nil {
		return g, err
	}
	out, err = v.call(ctx, addr, v.admin, "exec", b)
	if err != nil {
		return g, err
	}
	if len(out) == 2 {
		if a, ok := out[0].(common.Address); ok {
			g.Exec = a.Hex()
		}
		if h, ok := out[1].([32]byte); ok {
			g.ExecCodeHash = common.Hash(h).Hex()
		}
	}
	for seq := uint64(1); seq <= g.LastSeq+Window; seq++ {
		d, err := v.Decision(ctx, g.Epoch, seq, b)
		if err != nil {
			return g, err
		}
		if seq > g.LastSeq {
			if d.State == 1 {
				g.Buffered = append(g.Buffered, seq)
			}
			continue
		}
		g.Decisions = append(g.Decisions, d)
	}
	if g.Recovery, err = v.Recovery(ctx, b); err != nil {
		return g, err
	}
	return g, nil
}

// Decision reads decision(epoch, seq) with its derived maturity and tombstone.
func (v *Views) Decision(ctx context.Context, epoch uint32, seq uint64, b ethrpc.Block) (model.EthDecision, error) {
	out, err := v.call(ctx, v.addrs.Admin, v.admin, "decision", b, epoch, seq)
	if err != nil {
		return model.EthDecision{}, err
	}
	d, ok := gethabi.ConvertType(out[0], new(decision)).(*decision)
	if !ok {
		return model.EthDecision{}, fmt.Errorf("decision: unexpected type %T", out[0])
	}
	md := model.EthDecision{
		Epoch:         epoch,
		Seq:           seq,
		Hash:          common.Hash(d.Hash).Hex(),
		Kind:          d.Kind,
		State:         d.State,
		StateName:     model.DecisionStateName(d.State),
		Bad:           d.Bad,
		RecordedTime:  int64(d.RecordedTime),
		RecordedBlock: d.RecordedBlock,
	}
	if len(d.Params) > 0 {
		md.Params = common.Bytes2Hex(d.Params)
	}
	if d.State == 2 { // pending: matureAt is not stored
		delay := int64(DelayDefault)
		if d.Kind == 2 {
			delay = DelayUnpause
		}
		md.MatureAt = md.RecordedTime + delay
	}
	if d.State != 0 {
		ts, err := one[bool](ctx, v, v.addrs.Admin, v.admin, "isTombstoned", b, epoch, seq)
		if err != nil {
			return md, err
		}
		md.Tombstoned = ts
	}
	return md, nil
}

// Recovery reads the emergency recovery configuration and state.
func (v *Views) Recovery(ctx context.Context, b ethrpc.Block) (model.EthRecovery, error) {
	var r model.EthRecovery
	out, err := v.call(ctx, v.addrs.Admin, v.admin, "recoveryConfig", b)
	if err != nil {
		return r, err
	}
	cfg, ok := gethabi.ConvertType(out[0], new(recoveryConfig)).(*recoveryConfig)
	if !ok {
		return r, fmt.Errorf("recoveryConfig: unexpected type %T", out[0])
	}
	r.Delay, r.Threshold, r.CancelThreshold, r.Silence, r.HoldPeriod = cfg.Delay, cfg.Threshold, cfg.CancelThreshold, cfg.Silence, cfg.HoldPeriod
	r.Enabled = cfg.Delay > 0
	out, err = v.call(ctx, v.addrs.Admin, v.admin, "recovery", b)
	if err != nil {
		return r, err
	}
	if len(out) != 4 {
		return r, fmt.Errorf("recovery: %d outputs", len(out))
	}
	if h, ok := out[0].([32]byte); ok {
		r.Proposal = common.Hash(h).Hex()
	}
	r.OpenedAt, _ = out[1].(uint64)
	r.CancelVotes, _ = out[2].(uint8)
	if ap, ok := out[3].([4][32]byte); ok {
		for _, h := range ap {
			r.Approved = append(r.Approved, common.Hash(h).Hex())
		}
	}
	if r.OpenedAt > 0 {
		r.ExecutableAt = r.OpenedAt + r.Delay
	}
	return r, nil
}

// Bindings reads the executor's bindings() (router, access manager, transfer).
func (v *Views) Bindings(ctx context.Context, exec common.Address, b ethrpc.Block) ([3]common.Address, error) {
	out, err := v.call(ctx, exec, v.exec, "bindings", b)
	if err != nil {
		return [3]common.Address{}, err
	}
	var r [3]common.Address
	for i := 0; i < 3 && i < len(out); i++ {
		r[i], _ = out[i].(common.Address)
	}
	return r, nil
}
