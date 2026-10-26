package monitor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/abi"
	"github.com/allinbits/ibc-monitoring/internal/alert"
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
	srv.Returns(admin, "recoveryDisabled", true) // delay 0: latched from the start
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

// A primary that keeps answering an old, genuine finalized block passes the
// header cross-check but fails the freshness bounds (ninth round, finding
// 1): the pin is stale, not fresh, the second provider's own head is kept,
// and head_stale/ethereum is raised after the deferred threshold — never an
// auto-flag.
func TestStalePrimaryHeadIsNotFresh(t *testing.T) {
	old := ethrpctest.Block{Number: 90, Timestamp: uint64(time.Now().Add(-3 * time.Hour).Unix())}
	head := ethrpctest.Block{Number: 300, Timestamp: uint64(time.Now().Unix())}
	primary := ethrpctest.New(31337, old)
	t.Cleanup(primary.Close)
	primary.AddBlock(head)
	primary.SetFinalized(90) // withholding: it keeps finalizing 90
	admin := minimalAdmin(t, primary)
	second := ethrpctest.New(31337, old)
	t.Cleanup(second.Close)
	second.AddBlock(head)
	second.SetFinalized(300)
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
	st := store.NewMemory(10)
	c := &Collector{Eth: views, Eth2: sc, RequireCrossCheck: true, Store: st, Freshness: Freshness{MaxEthProviderLag: 64}}
	pins := c.Collect(context.Background())
	if pins.EthVerified != model.VerificationCrossChecked || !pins.EthState.OK || !pins.EthState.Verified {
		t.Fatalf("the old block should still cross-check: %+v %q", pins.EthState, pins.EthCrossCheck)
	}
	if !pins.EthState.Stale || !strings.Contains(pins.EthState.StaleReason, "withholding") || pins.EthHeads.Primary != 90 || pins.EthHeads.Second != 300 {
		t.Fatalf("stale pin: %+v heads %+v", pins.EthState, pins.EthHeads)
	}
	gov := st.Snapshots(store.KindEthGovernance)
	if len(gov) != 1 || pins.Fresh(model.ChainEthereum, gov[0].Provenance) {
		t.Fatal("a stale pin's snapshots read as fresh")
	}
	// The head-age bound alone catches it too.
	c2 := &Collector{Eth: views, Eth2: sc, RequireCrossCheck: true, Store: store.NewMemory(10), Freshness: Freshness{MaxEthHeadAge: 45 * time.Minute}}
	if p := c2.Collect(context.Background()); !p.EthState.Stale || !strings.Contains(p.EthState.StaleReason, "old") {
		t.Fatalf("head age: %+v", p.EthState)
	}
	// Within the bounds the same pin is fresh.
	c3 := &Collector{Eth: views, Eth2: sc, RequireCrossCheck: true, Store: store.NewMemory(10), Freshness: Freshness{MaxEthProviderLag: 500, MaxEthHeadAge: 4 * time.Hour}}
	if p := c3.Collect(context.Background()); p.EthState.Stale {
		t.Fatalf("within bounds: %+v", p.EthState)
	}
	// Persistently stale: head_stale/ethereum after the deferred threshold,
	// a liveness alarm with no auto-flag eligibility.
	sink := &recordingSink{}
	eng := &Engine{Collector: c, Checks: nil, Alerts: alert.NewManager(st, nil, sink), Store: st, DeferredAlarmFailures: 2}
	eng.RunOnce(context.Background())
	a, ok := st.Alarm("head_stale/ethereum")
	if !ok || a.State != model.AlarmRaised || a.Severity != model.SeverityCritical || a.AutoFlag {
		t.Fatalf("head_stale alarm %+v (ok %v)", a, ok)
	}
}
