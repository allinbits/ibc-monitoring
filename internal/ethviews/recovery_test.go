package ethviews

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc/ethrpctest"
	"github.com/allinbits/ibc-monitoring/internal/model"
)

// The fixture's deployment has the permanent disable and has not used it.
func TestRecoverySupportedEnabled(t *testing.T) {
	v, b, _ := fixture(t)
	r, err := v.Recovery(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Supported || r.Disabled || !r.Enabled || r.Status != model.RecoveryEnabled || r.DisabledBy != nil {
		t.Fatalf("recovery: %+v", r)
	}
	if r.Delay != 30*86400 || r.Threshold != 3 || r.CancelThreshold != 3 || r.Silence != 30*86400 || r.HoldPeriod != 14*86400 {
		t.Fatalf("config: %+v", r)
	}
}

// A kind 14 applied: the latch reads true, the recovery is no longer
// enabled although its delay is set, and the event names the decision.
func TestRecoverySupportedDisabled(t *testing.T) {
	v, b, srv := fixture(t)
	srv.Returns(adminAddr, "recoveryDisabled", true)
	srv.Returns(adminAddr, "recovery", [32]byte{}, uint64(0), uint8(0), [4][32]byte{})
	ev := v.admin.Events["EmergencyRecoveryDisabled"]
	srv.AddLog(types.Log{
		Address:     adminAddr,
		Topics:      []common.Hash{ev.ID, common.BigToHash(common.Big1), common.BigToHash(common.Big3)},
		BlockNumber: blockNumber - 10,
		TxHash:      common.HexToHash("0x14"),
	})
	g, err := v.Governance(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	r := g.Recovery
	if !r.Supported || !r.Disabled || r.Enabled || r.Status != model.RecoveryDisabled || r.Delay != 30*86400 {
		t.Fatalf("recovery: %+v", r)
	}
	want := model.EthRecoveryDisable{Epoch: 1, Seq: 3, Block: blockNumber - 10, Tx: common.HexToHash("0x14").Hex()}
	if r.DisabledBy == nil || *r.DisabledBy != want {
		t.Fatalf("disabled by: %+v", r.DisabledBy)
	}
}

// A deployment with delay 0 starts latched, with no event.
func TestRecoveryNeverEnabled(t *testing.T) {
	v, b, srv := fixture(t)
	srv.Returns(adminAddr, "recoveryConfig", recoveryConfig{})
	srv.Returns(adminAddr, "recoveryDisabled", true)
	r, err := v.Recovery(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Supported || !r.Disabled || r.Enabled || r.Status != model.RecoveryNeverEnabled || r.DisabledBy != nil {
		t.Fatalf("recovery: %+v", r)
	}
}

// A deployment that predates kind 14 (the live testnet's): the call reverts
// without data and the code lacks the selector, so the view is unsupported,
// not an error, and the rest of the recovery is still read.
func TestRecoveryUnsupported(t *testing.T) {
	v, b, srv := fixture(t)
	srv.On(adminAddr, "recoveryDisabled", func([]any) ([]any, error) { return nil, ethrpctest.EmptyRevert })
	g, err := v.Governance(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	r := g.Recovery
	if r.Supported || r.Disabled || !r.Enabled || r.Status != model.RecoveryEnabled || r.Delay != 30*86400 || r.OpenedAt != 1776000000 {
		t.Fatalf("recovery: %+v", r)
	}
}

// An empty revert from code that has the function is a failure, not an old
// deployment.
func TestRecoveryEmptyRevertWithSelector(t *testing.T) {
	v, b, srv := fixture(t)
	srv.Contract(adminAddr, v.admin, append([]byte{0x60, 0x80, 0x63}, v.admin.Methods["recoveryDisabled"].ID...))
	srv.On(adminAddr, "recoveryDisabled", func([]any) ([]any, error) { return nil, ethrpctest.EmptyRevert })
	if _, err := v.Recovery(context.Background(), b); err == nil {
		t.Fatal("expected an error")
	}
}

// A transient node error is never read as an old deployment: the
// collection fails as before.
func TestRecoveryRPCError(t *testing.T) {
	v, b, srv := fixture(t)
	srv.On(adminAddr, "recoveryDisabled", func([]any) ([]any, error) {
		return nil, &ethrpctest.RPCError{Code: -32005, Message: "rate limit exceeded"}
	})
	_, err := v.Governance(context.Background(), b)
	if err == nil {
		t.Fatal("expected an error")
	}
	var rev *ethrpc.ErrRevert
	if errors.As(err, &rev) {
		t.Fatalf("a node error read as a revert: %v", err)
	}
}

// An open recovery suspends a pending kind 14.
func TestDisableRecoverySuspended(t *testing.T) {
	v, b, srv := fixture(t)
	srv.On(adminAddr, "decision", func(args []any) ([]any, error) {
		if args[1].(uint64) == 1 {
			return []any{decision{Hash: [32]byte{1}, Kind: KindDisableRecovery, State: 2, RecordedTime: 1776500000}}, nil
		}
		return []any{decision{}}, nil
	})
	g, err := v.Governance(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	d := g.Decisions[0]
	if d.Kind != KindDisableRecovery || !d.Suspended || d.MatureAt != 1776500000+DelayDefault || model.KindName(d.Kind) != "disable-recovery" {
		t.Fatalf("kind 14: %+v", d)
	}
}
