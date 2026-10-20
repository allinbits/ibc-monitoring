package monitor

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/abi"
	"github.com/allinbits/ibc-monitoring/internal/alert"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc/ethrpctest"
	"github.com/allinbits/ibc-monitoring/internal/ethviews"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc/gnorpctest"
	"github.com/allinbits/ibc-monitoring/internal/gnoviews"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

type recordingSink struct {
	mu     sync.Mutex
	events []model.AlarmEvent
}

func (*recordingSink) Name() string { return "test" }
func (r *recordingSink) Send(_ context.Context, e model.AlarmEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

// count waits (sinks deliver asynchronously) until n events arrived or a
// deadline passes, and returns how many there are.
func (r *recordingSink) count(n int) int {
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		got := len(r.events)
		r.mu.Unlock()
		if got >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (r *recordingSink) first() model.AlarmEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[0]
}

// ethFixture is a minimal Ethereum deployment: IBCAdmin with no decisions, a
// transfer app with one route and a light client whose head is `head`.
func ethFixture(t *testing.T, head time.Time) (*ethviews.Views, *ethrpc.Client, *ethrpctest.Server) {
	t.Helper()
	admin := common.HexToAddress("0xaaaa")
	xfer := common.HexToAddress("0xbbbb")
	lc := common.HexToAddress("0xcccc")
	srv := ethrpctest.New(31337, ethrpctest.Block{Number: 100, Hash: common.HexToHash("0x100"), Timestamp: uint64(time.Now().Unix())})
	t.Cleanup(srv.Close)
	srv.Contract(admin, abi.MustLoad(abi.Admin), []byte{1})
	srv.Contract(xfer, abi.MustLoad(abi.Transfer), []byte{1})
	srv.Contract(lc, abi.MustLoad(abi.LightClient), []byte{1})
	keys := [6]common.Address{}
	srv.Returns(admin, "epoch", uint32(1))
	srv.Returns(admin, "lastSeq", uint64(0))
	srv.Returns(admin, "govClient", "client-0")
	srv.Returns(admin, "gnoChainId", "gno-eth-dev")
	srv.Returns(admin, "lastRecordTime", uint64(0))
	srv.Returns(admin, "setupDone", true)
	srv.Returns(admin, "keys", keys)
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
	srv.Returns(xfer, "routeKey", "client-0")
	srv.Returns(xfer, "getEscrow", common.HexToAddress("0x5e5e"))
	for _, m := range []string{"routeClosed", "routeReopening", "routePaused", "refundsHeld", "recoveryBlocked", "isRetired"} {
		srv.Returns(xfer, m, false)
	}
	srv.Returns(xfer, "guardianKeys", keys)
	flags := [6]struct {
		BlockNumber uint64
		Time        uint64
	}{}
	srv.Returns(xfer, "routeFlags", flags)
	srv.Returns(xfer, "refundsFlags", flags)
	srv.Returns(xfer, "routeHold", flags[0])
	srv.Returns(xfer, "refundsHold", flags[0])
	srv.Returns(xfer, "quarantineSeconds", uint64(0))
	srv.Returns(xfer, "quarantineEnd", big.NewInt(0))
	type lim struct {
		Set    bool
		Limits struct {
			MaxPerMessage *big.Int
			DayCap        *big.Int
			RefundCap     *big.Int
			SendMax       *big.Int
			SendMin       *big.Int
		}
		ReceiveLevel *big.Int
		RefundLevel  *big.Int
	}
	l := lim{ReceiveLevel: big.NewInt(0), RefundLevel: big.NewInt(0)}
	l.Limits.MaxPerMessage, l.Limits.DayCap, l.Limits.RefundCap, l.Limits.SendMax, l.Limits.SendMin = big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0)
	srv.Returns(xfer, "limits", l)
	for _, m := range []string{"reserved", "losses", "acceptedDeficit"} {
		srv.Returns(xfer, m, big.NewInt(0))
	}
	srv.Returns(xfer, "incidents", []struct {
		Kind        uint8
		Status      uint8
		Client      [32]byte
		SuspectFrom uint64
	}{}, uint64(0), uint64(0))
	srv.Returns(xfer, "clientRecon", uint64(0), uint32(0), uint64(0), uint64(0), uint64(0))
	cs, err := ethviews.EncodeClientState("gno-eth-dev", 0, 4200, 1, 3, 14*86400, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	srv.Returns(lc, "getClientState", cs)
	srv.Returns(lc, "getConsensusState", struct {
		Timestamp          uint64
		AppHash            [32]byte
		NextValidatorsHash [32]byte
	}{Timestamp: uint64(head.UnixNano())})
	srv.Returns(lc, "isFrozen", false)
	srv.Returns(lc, "isExpired", false)
	c, err := ethrpc.Dial(context.Background(), srv.URL, "eth-fixture", ethrpc.Finality{Mode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	v, err := ethviews.New(c, ethviews.Addresses{Admin: admin, Transfer: xfer, LightClient: lc})
	if err != nil {
		t.Fatal(err)
	}
	return v, c, srv
}

func TestEngineEndToEnd(t *testing.T) {
	f, err := gnorpctest.Load("../gnoviews/testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	gsrv := gnorpctest.New(f)
	t.Cleanup(gsrv.Close)
	gno := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-fixture")), gnoviews.DefaultPaths)
	gno2 := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-second")), gnoviews.DefaultPaths)
	// The GnoLightClient's head is 13.5 days old with a 14-day trusting
	// period: inside the critical window.
	eth, ethc, _ := ethFixture(t, f.Time.Add(-14*24*time.Hour+6*time.Hour))

	st := store.NewMemory(10)
	sink := &recordingSink{}
	eng := &Engine{
		Collector: &Collector{Gno: gno, Gno2: gno2, Eth: eth, Eth2: ethc, RequireCrossCheck: true, Store: st},
		Checks:    []Check{ClientExpiry{Config: DefaultClientExpiryConfig()}},
		Alerts:    alert.NewManager(st, nil, sink),
		Store:     st,
		Now:       func() time.Time { return f.Time },
	}
	results := eng.RunOnce(context.Background())
	if len(results) != 1 || results[0].Status != model.CheckAlarm {
		t.Fatalf("results %+v", results)
	}
	pins, ok := st.Snapshot(store.KindPins, "latest")
	if !ok {
		t.Fatal("no pins")
	}
	p := pins.Value.(Pins)
	if p.GnoError != "" || p.EthError != "" {
		t.Fatalf("collection errors: %s / %s", p.GnoError, p.EthError)
	}
	if p.GnoVerified != model.VerificationCrossChecked || p.EthVerified != model.VerificationCrossChecked || !p.GnoState.Verified || !p.EthState.Verified {
		t.Fatalf("verification %s / %s (%s / %s)", p.GnoVerified, p.EthVerified, p.GnoCrossCheck, p.EthCrossCheck)
	}
	if len(st.Snapshots(store.KindGnoClients)) != 2 || len(st.Snapshots(store.KindEthClients)) != 1 || len(st.Snapshots(store.KindEthRoutes)) != 1 {
		t.Fatalf("snapshots: gno clients %d, eth clients %d, eth routes %d", len(st.Snapshots(store.KindGnoClients)), len(st.Snapshots(store.KindEthClients)), len(st.Snapshots(store.KindEthRoutes)))
	}
	ethRoute := st.Snapshots(store.KindEthRoutes)[0].Value.(model.EthRoute)
	if ethRoute.CurrentClient != "client-0" || ethRoute.Provenance.Verification != model.VerificationCrossChecked {
		t.Fatalf("eth route %+v", ethRoute)
	}
	alarms := st.Alarms(false)
	keys := map[string]model.Severity{}
	for _, a := range alarms {
		keys[a.Key] = a.Severity
	}
	// The expired Tendermint client and the nearly expired GnoLightClient
	// alarm; the fresh Ethereum client on gno.land does not.
	if keys["client_expiry/gno/tm-1"] != model.SeverityCritical || keys["client_expiry/ethereum/client-0"] != model.SeverityCritical {
		t.Fatalf("alarms %+v", keys)
	}
	if _, ok := keys["client_expiry/gno/ethereum-1"]; ok {
		t.Fatalf("fresh client alarmed: %+v", keys)
	}
	if n := sink.count(2); n != 2 {
		t.Fatalf("sink events %d", n)
	}
	// A second run changes nothing: no new transitions.
	eng.RunOnce(context.Background())
	time.Sleep(50 * time.Millisecond)
	if n := sink.count(3); n != 2 {
		t.Fatalf("duplicate transitions: %d", n)
	}
}

func TestEngineDeferredAlarm(t *testing.T) {
	st := store.NewMemory(10)
	sink := &recordingSink{}
	eng := &Engine{Checks: []Check{ClientExpiry{Config: DefaultClientExpiryConfig()}}, Alerts: alert.NewManager(st, nil, sink), Store: st, DeferredAlarmFailures: 3}
	for i := 0; i < 2; i++ {
		eng.RunOnce(context.Background())
	}
	time.Sleep(50 * time.Millisecond)
	if n := sink.count(1); n != 0 {
		t.Fatalf("alarmed too early: %d", n)
	}
	eng.RunOnce(context.Background())
	if n := sink.count(1); n != 1 || sink.first().Alarm.Key != "client_expiry_unavailable" {
		t.Fatalf("events %d", n)
	}
	r, _ := st.LatestResult("client_expiry")
	if r.ConsecutiveSkips != 3 {
		t.Fatalf("skips %d", r.ConsecutiveSkips)
	}
}
