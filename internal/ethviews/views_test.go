package ethviews

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/abi"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc/ethrpctest"
	"github.com/allinbits/ibc-monitoring/internal/model"
)

var (
	adminAddr = common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	xferAddr  = common.HexToAddress("0x000000000000000000000000000000000000bbbb")
	lcAddr    = common.HexToAddress("0x000000000000000000000000000000000000cccc")
	tokenAddr = common.HexToAddress("0x000000000000000000000000000000000000dddd")
	guardian0 = common.HexToAddress("0x0000000000000000000000000000000000000001")
	guardian1 = common.HexToAddress("0x0000000000000000000000000000000000000002")
	watch0    = common.HexToAddress("0x0000000000000000000000000000000000000005")
)

const (
	blockNumber = uint64(9_000_000)
	blockTime   = uint64(1776700000)
)

func fixture(t *testing.T) (*Views, ethrpc.Block, *ethrpctest.Server) {
	t.Helper()
	srv := ethrpctest.New(31337, ethrpctest.Block{Number: blockNumber, Hash: common.HexToHash("0x11"), StateRoot: common.HexToHash("0x22"), Timestamp: blockTime})
	t.Cleanup(srv.Close)

	admin := abi.MustLoad(abi.Admin)
	xfer := abi.MustLoad(abi.Transfer)
	lc := abi.MustLoad(abi.LightClient)
	srv.Contract(adminAddr, admin, []byte{0x60, 0x80})
	srv.Contract(xferAddr, xfer, []byte{0x60, 0x80})
	srv.Contract(lcAddr, lc, []byte{0x60, 0x80})

	keys := [6]common.Address{guardian0, guardian1, {}, {}, watch0, {}}
	// IBCAdmin.
	srv.Returns(adminAddr, "epoch", uint32(1))
	srv.Returns(adminAddr, "lastSeq", uint64(2))
	srv.Returns(adminAddr, "govClient", "client-0")
	srv.Returns(adminAddr, "gnoChainId", "gno-eth-dev")
	srv.Returns(adminAddr, "lastRecordTime", uint64(1776500000))
	srv.Returns(adminAddr, "setupDone", true)
	srv.Returns(adminAddr, "keys", keys)
	srv.Returns(adminAddr, "flags", [6]flag{{}, {BlockNumber: 8_999_000, Time: 1776690000}, {}, {}, {}, {}})
	srv.Returns(adminAddr, "hold", flag{})
	srv.Returns(adminAddr, "holds", []hold{{Start: 1770000000, End: 1770100000}})
	srv.Returns(adminAddr, "conflictBlock", uint64(0))
	srv.Returns(adminAddr, "exec", common.HexToAddress("0xeeee"), [32]byte{0xab})
	srv.On(adminAddr, "decision", func(args []any) ([]any, error) {
		seq := args[1].(uint64)
		switch seq {
		case 1:
			return []any{decision{Hash: [32]byte{1}, Kind: 4, State: 3, RecordedTime: 1776400000, RecordedBlock: 8_900_000}}, nil
		case 2:
			return []any{decision{Hash: [32]byte{2}, Kind: 7, State: 2, RecordedTime: 1776500000, RecordedBlock: 8_950_000, Params: []byte{0xde, 0xad}}}, nil
		case 4:
			return []any{decision{Hash: [32]byte{4}, Kind: 3, State: 1, RecordedTime: 0}}, nil
		}
		return []any{decision{}}, nil
	})
	srv.On(adminAddr, "isTombstoned", func(args []any) ([]any, error) {
		return []any{args[1].(uint64) == 4}, nil
	})
	srv.Returns(adminAddr, "recoveryConfig", recoveryConfig{Delay: 30 * 86400, Threshold: 3, CancelThreshold: 3, Silence: 30 * 86400, HoldPeriod: 14 * 86400})
	srv.Returns(adminAddr, "recovery", [32]byte{0x77}, uint64(1776000000), uint8(1), [4][32]byte{{0x77}, {0x77}, {}, {}})

	// Transfer app.
	srv.Returns(xferAddr, "routeKey", "client-0")
	srv.Returns(xferAddr, "getEscrow", common.HexToAddress("0x5e5e"))
	srv.Returns(xferAddr, "routeClosed", false)
	srv.Returns(xferAddr, "routeReopening", false)
	srv.Returns(xferAddr, "routePaused", true)
	srv.Returns(xferAddr, "refundsHeld", false)
	srv.Returns(xferAddr, "recoveryBlocked", false)
	srv.Returns(xferAddr, "guardianKeys", keys)
	srv.Returns(xferAddr, "routeFlags", [6]flag{{BlockNumber: 8_998_000, Time: 1776680000}, {}, {}, {}, {BlockNumber: 8_998_500, Time: 1776681000}, {}})
	srv.Returns(xferAddr, "refundsFlags", [6]flag{})
	srv.Returns(xferAddr, "routeHold", flag{})
	srv.Returns(xferAddr, "refundsHold", flag{})
	srv.Returns(xferAddr, "quarantineSeconds", uint64(7*86400))
	srv.Returns(xferAddr, "quarantineEnd", big.NewInt(1777000000))
	lv := limitsView{Set: true, ReceiveLevel: big.NewInt(123), RefundLevel: big.NewInt(456)}
	lv.Limits.MaxPerMessage = big.NewInt(25_000_000_000)
	lv.Limits.DayCap = big.NewInt(100_000_000_000)
	lv.Limits.RefundCap = big.NewInt(100_000_000_000)
	lv.Limits.SendMax = big.NewInt(0)
	lv.Limits.SendMin = big.NewInt(0)
	srv.Returns(xferAddr, "limits", lv)
	srv.Returns(xferAddr, "reserved", big.NewInt(10))
	srv.Returns(xferAddr, "losses", big.NewInt(20))
	srv.Returns(xferAddr, "acceptedDeficit", big.NewInt(5))
	srv.Returns(xferAddr, "incidents", []incident{{Kind: 1, Status: 1, Client: [32]byte{0x9}, SuspectFrom: 8_000_000}}, uint64(0), uint64(1))
	srv.On(xferAddr, "clientRecon", func(args []any) ([]any, error) {
		if args[0].(string) == "client-old" {
			return []any{uint64(3), uint32(1), uint64(10), uint64(2), uint64(40)}, nil
		}
		return []any{uint64(1), uint32(0), uint64(0), uint64(0), uint64(7)}, nil
	})
	srv.On(xferAddr, "isRetired", func(args []any) ([]any, error) { return []any{args[0].(string) == "client-old"}, nil })
	srv.On(xferAddr, "sendRecord", func(args []any) ([]any, error) {
		return []any{SendRecord{Commitment: [32]byte{0xc0}, Sender: guardian0, Token: tokenAddr, Amount: big.NewInt(1000), Refunded: big.NewInt(0), Outcome: 1, Payment: 0, Open: false, SendBlock: 8_900_100, ProcessedBlock: 8_900_200, TimeoutTimestamp: 1776400000}}, nil
	})
	srv.Returns(xferAddr, "gnoBusy", false)

	// GnoLightClient.
	cs, err := EncodeClientState("gno-eth-dev", 0, 4200, 1, 3, 14*86400, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	srv.Returns(lcAddr, "getClientState", cs)
	srv.On(lcAddr, "getConsensusState", func(args []any) ([]any, error) {
		if args[0].(uint64) != 4200 {
			return nil, errConsensus
		}
		return []any{consensusState{Timestamp: 1776600000 * 1_000_000_000}}, nil
	})
	srv.Returns(lcAddr, "isFrozen", false)
	srv.Returns(lcAddr, "isExpired", false)
	srv.Returns(lcAddr, "lastTransition", uint64(3), uint64(1776000000))

	ethrpc.Now = func() time.Time { return time.Unix(int64(blockTime), 0) }
	c, err := ethrpc.Dial(context.Background(), srv.URL, "fixture", ethrpc.Finality{Mode: "tag"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	v, err := New(c, Addresses{Admin: adminAddr, Transfer: xferAddr, LightClient: lcAddr})
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.Pinned(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Number != blockNumber || b.Finality != "finalized" || b.Hash != srv.Head().HashOf() || b.StateRoot != common.HexToHash("0x22") {
		t.Fatalf("pinned %+v", b)
	}
	return v, b, srv
}

type consensusErr struct{}

func (consensusErr) Error() string { return "no consensus state" }

var errConsensus = consensusErr{}

func TestGovernance(t *testing.T) {
	v, b, _ := fixture(t)
	g, err := v.Governance(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if g.Epoch != 1 || g.LastSeq != 2 || g.GovClient != "client-0" || !g.SetupDone || g.GnoChainID != "gno-eth-dev" {
		t.Fatalf("admin: %+v", g)
	}
	if len(g.Keys) != 6 || g.Keys[0] != guardian0.Hex() {
		t.Fatalf("keys: %v", g.Keys)
	}
	if len(g.Flags) != 1 || g.Flags[0].Seat != 1 || g.Flags[0].Key != guardian1.Hex() || g.Flags[0].Time != 1776690000 {
		t.Fatalf("flags: %+v", g.Flags)
	}
	if g.Hold != nil || len(g.Holds) != 1 || g.Holds[0].End != 1770100000 {
		t.Fatalf("holds: %+v %+v", g.Hold, g.Holds)
	}
	if g.Exec != common.HexToAddress("0xeeee").Hex() || g.ExecCodeHash != common.Hash([32]byte{0xab}).Hex() {
		t.Fatalf("exec: %s %s", g.Exec, g.ExecCodeHash)
	}
	if len(g.Decisions) != 2 {
		t.Fatalf("decisions: %+v", g.Decisions)
	}
	d1, d2 := g.Decisions[0], g.Decisions[1]
	if d1.Kind != 4 || d1.StateName != "executed" || d1.MatureAt != 0 || d1.Tombstoned {
		t.Fatalf("d1: %+v", d1)
	}
	if d2.Kind != 7 || d2.StateName != "pending" || d2.MatureAt != 1776500000+7*86400 || d2.Params != "dead" {
		t.Fatalf("d2: %+v", d2)
	}
	if len(g.Buffered) != 1 || g.Buffered[0] != 4 {
		t.Fatalf("buffered: %v", g.Buffered)
	}
	r := g.Recovery
	if !r.Enabled || r.Delay != 30*86400 || r.Threshold != 3 || r.OpenedAt != 1776000000 || r.ExecutableAt != 1776000000+30*86400 || r.CancelVotes != 1 {
		t.Fatalf("recovery: %+v", r)
	}
	if len(r.Approved) != 4 || r.Approved[0] != common.Hash([32]byte{0x77}).Hex() || r.Approved[2] != (common.Hash{}).Hex() {
		t.Fatalf("approved: %v", r.Approved)
	}
	if g.Provenance.Chain != model.ChainEthereum || g.Provenance.Height != blockNumber || g.Provenance.Finality != "finalized" {
		t.Fatalf("provenance: %+v", g.Provenance)
	}
}

func TestRoute(t *testing.T) {
	v, b, _ := fixture(t)
	r, err := v.Route(context.Background(), "client-0", []string{"client-old"}, []common.Address{tokenAddr}, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.RouteKey != "client-0" || r.Escrow != common.HexToAddress("0x5e5e").Hex() || !r.Paused || r.Closed {
		t.Fatalf("route: %+v", r)
	}
	if len(r.RouteFlags) != 2 || r.RouteFlags[0].Seat != 0 || r.RouteFlags[1].Seat != 4 || r.RouteFlags[1].Key != watch0.Hex() {
		t.Fatalf("route flags: %+v", r.RouteFlags)
	}
	if len(r.RefundsFlags) != 0 || r.RouteHold != nil {
		t.Fatalf("refunds: %+v %+v", r.RefundsFlags, r.RouteHold)
	}
	if r.QuarantineSecs != 7*86400 || r.QuarantineEnd != 1777000000 {
		t.Fatalf("quarantine: %+v", r)
	}
	if len(r.Limits) != 1 || !r.Limits[0].Set || r.Limits[0].DayCap.String() != "100000000000" || r.Limits[0].ReceiveLevel.String() != "123" || r.Limits[0].Losses.String() != "20" || r.Limits[0].Deficit.String() != "5" {
		t.Fatalf("limits: %+v", r.Limits)
	}
	if len(r.Incidents) != 1 || r.Incidents[0].Kind != 1 || r.Incidents[0].SuspectFrom != 8_000_000 || r.PendingIncident != 1 {
		t.Fatalf("incidents: %+v", r.Incidents)
	}
	if len(r.Recon) != 2 || r.Recon[1].ClientID != "client-old" || !r.Recon[1].Retired || r.Recon[1].SuspectOpen != 2 || r.Recon[0].Retired {
		t.Fatalf("recon: %+v", r.Recon)
	}
	rec, err := v.SendRecord(context.Background(), "client-0", 1, b)
	if err != nil {
		t.Fatal(err)
	}
	amt, refunded := rec.Amounts()
	if rec.Outcome != 1 || amt.String() != "1000" || refunded.String() != "0" || rec.Token != tokenAddr {
		t.Fatalf("send record: %+v", rec)
	}
}

func TestLightClient(t *testing.T) {
	v, b, _ := fixture(t)
	c, err := v.LightClient(context.Background(), lcAddr, "client-0", "route", b)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != model.ClientActive || c.LatestHeight != 4200 || c.ChainID != "gno-eth-dev" || c.TrustingPeriod != 14*24*time.Hour {
		t.Fatalf("client: %+v", c)
	}
	head := time.Unix(1776600000, 0).UTC()
	if !c.HeadTime.Equal(head) || !c.Expiry.Equal(head.Add(14*24*time.Hour)) {
		t.Fatalf("expiry: %s %s", c.HeadTime, c.Expiry)
	}
	if c.Kind != model.ClientKindGnoLightClient || c.Address != lcAddr.Hex() || c.Role != "route" {
		t.Fatalf("client: %+v", c)
	}
	tr, err := v.LastTransition(context.Background(), lcAddr, b)
	if err != nil || tr.ID != 3 || tr.Time != 1776000000 {
		t.Fatalf("transition: %+v %v", tr, err)
	}
}

func TestRevertIsReported(t *testing.T) {
	v, b, srv := fixture(t)
	srv.On(xferAddr, "routeKey", func([]any) ([]any, error) { return nil, errConsensus })
	_, err := v.Route(context.Background(), "client-0", nil, nil, b)
	if err == nil {
		t.Fatal("expected an error")
	}
	var rev *ethrpc.ErrRevert
	if !asRevert(err, &rev) || rev.Reason != "no consensus state" {
		t.Fatalf("got %v", err)
	}
}
