// Package ethviews reads the Ethereum contracts' views into typed models
// (docs/design.md §4.2), every call pinned to one block.
package ethviews

import (
	"context"
	"fmt"
	"math/big"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/abi"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/model"
)

// Addresses are the deployment's contracts (contracts/deployments/<chain>.json
// of gno-eth-ibc).
type Addresses struct {
	Router       common.Address
	Transfer     common.Address
	Admin        common.Address
	Exec         common.Address
	LightClient  common.Address
	UgnotVoucher common.Address
}

// Views reads from one node.
type Views struct {
	// HistoryFrom is the first block the route history (GnoRouteMigrated)
	// is read from; the deployment block (eth.history_from_block).
	HistoryFrom uint64
	rpc         *ethrpc.Client
	addrs       Addresses
	admin       *gethabi.ABI
	xfer        *gethabi.ABI
	lc          *gethabi.ABI
	erc20       *gethabi.ABI
	exec        *gethabi.ABI
	router      *gethabi.ABI
}

// New returns views over rpc.
func New(rpc *ethrpc.Client, addrs Addresses) (*Views, error) {
	v := &Views{rpc: rpc, addrs: addrs}
	var err error
	if v.admin, err = abi.Load(abi.Admin); err != nil {
		return nil, err
	}
	if v.xfer, err = abi.Load(abi.Transfer); err != nil {
		return nil, err
	}
	if v.lc, err = abi.Load(abi.LightClient); err != nil {
		return nil, err
	}
	if v.erc20, err = abi.Load(abi.Voucher); err != nil {
		return nil, err
	}
	if v.exec, err = abi.Load(abi.AdminExec); err != nil {
		return nil, err
	}
	if v.router, err = abi.Load(abi.Router); err != nil {
		return nil, err
	}
	return v, nil
}

// Addresses returns the configured addresses.
func (v *Views) Addresses() Addresses { return v.addrs }

// Pinned resolves the block of a snapshot.
func (v *Views) Pinned(ctx context.Context) (ethrpc.Block, error) { return v.rpc.Pinned(ctx) }

func (v *Views) provenance(b ethrpc.Block) model.Provenance {
	return model.Provenance{
		Chain:        model.ChainEthereum,
		Height:       b.Number,
		BlockHash:    b.Hash.Hex(),
		Node:         v.rpc.Name(),
		ReadAt:       ethrpc.Now(),
		Verification: model.VerificationProvider,
		Finality:     b.Finality,
	}
}

func (v *Views) call(ctx context.Context, to common.Address, a *gethabi.ABI, method string, b ethrpc.Block, args ...any) ([]any, error) {
	return v.rpc.Call(ctx, to, a, method, b.Number, args...)
}

// one calls a single-output view and converts it.
func one[T any](ctx context.Context, v *Views, to common.Address, a *gethabi.ABI, method string, b ethrpc.Block, args ...any) (T, error) {
	var zero T
	out, err := v.call(ctx, to, a, method, b, args...)
	if err != nil {
		return zero, err
	}
	if len(out) != 1 {
		return zero, fmt.Errorf("%s: expected 1 output, got %d", method, len(out))
	}
	t, ok := out[0].(T)
	if !ok {
		return zero, fmt.Errorf("%s: output is %T, want %T", method, out[0], zero)
	}
	return t, nil
}

// flag is IGuardians.Flag as go-ethereum unpacks it.
type flag struct {
	BlockNumber uint64
	Time        uint64
}

func toFlags(raw any, keys [6]common.Address) ([]model.EthFlag, error) {
	fl, ok := gethabi.ConvertType(raw, new([6]flag)).(*[6]flag)
	if !ok {
		return nil, fmt.Errorf("flags: unexpected type %T", raw)
	}
	out := []model.EthFlag{}
	for i, f := range fl {
		if f.BlockNumber == 0 && f.Time == 0 {
			continue
		}
		out = append(out, model.EthFlag{Seat: i, Key: keys[i].Hex(), BlockNumber: f.BlockNumber, Time: int64(f.Time)})
	}
	return out, nil
}

func toHold(raw any) (*model.EthHold, error) {
	f, ok := gethabi.ConvertType(raw, new(flag)).(*flag)
	if !ok {
		return nil, fmt.Errorf("hold: unexpected type %T", raw)
	}
	if f.BlockNumber == 0 && f.Time == 0 {
		return nil, nil
	}
	return &model.EthHold{Start: int64(f.Time)}, nil
}

func amountOf(x any) model.Amount {
	if b, ok := x.(*big.Int); ok {
		return model.NewAmount(b)
	}
	return model.Amount{}
}
