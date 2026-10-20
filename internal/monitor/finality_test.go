package monitor

import (
	"context"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/abi"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc/ethrpctest"
	"github.com/allinbits/ibc-monitoring/internal/ethviews"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// minimalAdmin serves just enough of IBCAdmin for a collection to succeed.
func minimalAdmin(t *testing.T, srv *ethrpctest.Server) common.Address {
	t.Helper()
	admin := common.HexToAddress("0xaaaa")
	srv.Contract(admin, abi.MustLoad(abi.Admin), []byte{1})
	srv.Returns(admin, "epoch", uint32(1))
	srv.Returns(admin, "lastSeq", uint64(0))
	srv.Returns(admin, "govClient", "client-0")
	srv.Returns(admin, "gnoChainId", "gno-eth-dev")
	srv.Returns(admin, "lastRecordTime", uint64(0))
	srv.Returns(admin, "setupDone", true)
	srv.Returns(admin, "keys", [6]common.Address{})
	srv.Returns(admin, "flags", [6]struct {
		BlockNumber uint64
		Time        uint64
	}{})
	srv.Returns(admin, "hold", struct {
		BlockNumber uint64
		Time        uint64
	}{})
	srv.Returns(admin, "holds", []struct {
		Start  uint64
		End    uint64
		Merged bool
	}{})
	srv.Returns(admin, "conflictBlock", uint64(0))
	srv.Returns(admin, "exec", common.Address{}, [32]byte{})
	srv.On(admin, "decision", func([]any) ([]any, error) {
		return []any{struct {
			Hash          [32]byte
			Kind          uint8
			State         uint8
			Bad           bool
			RecordedTime  uint64
			RecordedBlock uint64
			Params        []byte
		}{}}, nil
	})
	srv.Returns(admin, "recoveryConfig", struct {
		Delay           uint64
		Threshold       uint8
		CancelThreshold uint8
		Silence         uint64
		HoldPeriod      uint64
	}{})
	srv.Returns(admin, "recovery", [32]byte{}, uint64(0), uint8(0), [4][32]byte{})
	return admin
}

// Finality is established by the second provider's own finalized head
// (review finding 5): a block the primary calls finalized but the second
// provider has not finalized yet is unverified, however its header agrees.
func TestFinalityIsIndependent(t *testing.T) {
	blocks := []ethrpctest.Block{{Number: 90, Timestamp: 90}, {Number: 100, Timestamp: 100}}
	primary := ethrpctest.New(31337, blocks[0])
	t.Cleanup(primary.Close)
	primary.AddBlock(blocks[1])
	admin := minimalAdmin(t, primary)
	second := ethrpctest.New(31337, blocks[0])
	t.Cleanup(second.Close)
	second.AddBlock(blocks[1])
	second.SetFinalized(90)

	pc, err := ethrpc.Dial(context.Background(), primary.URL, "primary", ethrpc.Finality{Mode: "tag"})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := ethrpc.Dial(context.Background(), second.URL, "second", ethrpc.Finality{Mode: "tag"})
	if err != nil {
		t.Fatal(err)
	}
	views, err := ethviews.New(pc, ethviews.Addresses{Admin: admin, Transfer: common.HexToAddress("0xbbbb")})
	if err != nil {
		t.Fatal(err)
	}
	collect := func() Pins {
		st := store.NewMemory(10)
		return (&Collector{Eth: views, Eth2: sc, RequireCrossCheck: true, Store: st}).Collect(context.Background())
	}
	pins := collect()
	if pins.Eth.Number != 100 {
		t.Fatalf("primary pinned %d", pins.Eth.Number)
	}
	if pins.EthVerified != model.VerificationUnverified || !strings.Contains(pins.EthCrossCheck, "not finalized by the second provider") || pins.EthFinalityIndependent {
		t.Fatalf("lagging second provider: %s %q", pins.EthVerified, pins.EthCrossCheck)
	}
	// Once the second provider has finalized it, the block is cross-checked
	// and its finality independent.
	second.SetFinalized(100)
	pins = collect()
	if pins.EthVerified != model.VerificationCrossChecked || !pins.EthFinalityIndependent || !strings.Contains(pins.EthCrossCheck, "finalized by the second provider") {
		t.Fatalf("finalized: %s %q", pins.EthVerified, pins.EthCrossCheck)
	}
}
