package gnoviews

import (
	"context"
	"testing"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc/gnorpctest"
	"github.com/allinbits/ibc-monitoring/internal/model"
)

func devnet(t *testing.T) (*Views, Pin, *gnorpctest.Server) {
	t.Helper()
	f, err := gnorpctest.Load("testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := gnorpctest.New(f)
	t.Cleanup(srv.Close)
	v := New(gnorpc.New(srv.URL, gnorpc.WithName("fixture")), DefaultPaths)
	v.now = func() time.Time { return f.Time }
	pin, err := v.LatestPin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pin.Height != 4200 || pin.BlockHash == "" {
		t.Fatalf("pin %+v", pin)
	}
	return v, pin, srv
}

func TestClients(t *testing.T) {
	v, pin, srv := devnet(t)
	clients, err := v.Clients(context.Background(), pin)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 {
		t.Fatalf("got %d clients", len(clients))
	}
	eth := clients[0]
	if eth.ID != "ethereum-1" || eth.Kind != model.ClientKindEthereum || eth.Status != model.ClientActive {
		t.Fatalf("eth: %+v", eth)
	}
	if eth.VerifierVersion != "ethereum/v0" || eth.TrustGeneration != 2 || eth.CounterpartyID != "client-0" {
		t.Fatalf("eth: %+v", eth)
	}
	if eth.LatestHeight != 10000000 || eth.LatestExecutionBlock != 9876543 || eth.ChainID != "11155111" {
		t.Fatalf("eth head: %+v", eth)
	}
	// 8 periods × 256 epochs × 32 slots × 12 s.
	wantTP := time.Duration(8*256*32*12) * time.Second
	if eth.TrustingPeriod != wantTP {
		t.Fatalf("trusting period %s, want %s", eth.TrustingPeriod, wantTP)
	}
	wantHead := time.Unix(1792600000, 0).UTC()
	if !eth.HeadTime.Equal(wantHead) || !eth.Expiry.Equal(wantHead.Add(wantTP)) {
		t.Fatalf("head %s expiry %s", eth.HeadTime, eth.Expiry)
	}
	if eth.Provenance.Height != 4200 || eth.Provenance.Chain != model.ChainGno || eth.Provenance.Node != "fixture" {
		t.Fatalf("provenance %+v", eth.Provenance)
	}

	tm := clients[1]
	if tm.Kind != model.ClientKindTendermint || tm.Status != model.ClientExpired || !tm.RetiredForTransfer || tm.RetiredForGov {
		t.Fatalf("tm: %+v", tm)
	}
	if tm.TrustingPeriod != 14*24*time.Hour || !tm.Expiry.Equal(time.Unix(1760000000, 0).UTC().Add(14*24*time.Hour)) {
		t.Fatalf("tm expiry: %s %s", tm.TrustingPeriod, tm.Expiry)
	}
	if tm.LatestHeight != 500 || tm.LatestRevision != 1 {
		t.Fatalf("tm head: %+v", tm)
	}
	if len(srv.Unknown) != 0 {
		t.Fatalf("queries missing from the fixture: %v", srv.Unknown)
	}
}

func TestRoutes(t *testing.T) {
	v, pin, srv := devnet(t)
	routes, err := v.Routes(context.Background(), pin)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("got %d routes", len(routes))
	}
	r := routes[0]
	if r.Key != "ethereum-1" || r.Client != "ethereum-1" || r.Closed || r.Activated != 100 {
		t.Fatalf("route: %+v", r)
	}
	if !r.Enrolled.Equal(time.Unix(1760000000, 0)) {
		t.Fatalf("enrolled %s", r.Enrolled)
	}
	if len(r.Denoms) != 2 {
		t.Fatalf("denoms %d", len(r.Denoms))
	}
	ugnot := r.Denoms[0]
	if ugnot.Kind != "unescrow" || ugnot.Lock == nil || ugnot.Lock.String() != "200000000000" {
		t.Fatalf("ugnot lock: %+v", ugnot.Lock)
	}
	// cap = max(10_000 GNOT, lock/10 = 20_000 GNOT) = 20_000 GNOT.
	if ugnot.Meter.Cap.String() != "20000000000" {
		t.Fatalf("cap %s", ugnot.Meter.Cap)
	}
	// stored 4_000 GNOT at 1776686400; now is 2026-10-20T12:00:00Z = 1792612800 → capped at the cap.
	if ugnot.Meter.Live.String() != "20000000000" || ugnot.Meter.LiveSource != "client" {
		t.Fatalf("live %s (%s)", ugnot.Meter.Live, ugnot.Meter.LiveSource)
	}
	if ugnot.Totals.InFlight.String() != "2000000000" || ugnot.Totals.Owed.String() != "1000000000" || ugnot.Waiting != 1 {
		t.Fatalf("totals %+v", ugnot.Totals)
	}
	mint := r.Denoms[1]
	if mint.Kind != "mint" || mint.SupplyCap.String() != "9000000000000000000" || mint.QueuedRefunds != 2 {
		t.Fatalf("mint: %+v", mint)
	}
	if mint.Meter.Cap.String() != "500000000" || mint.Lock.String() != "0" {
		t.Fatalf("mint cap %s lock %s", mint.Meter.Cap, mint.Lock)
	}
	if len(srv.Unknown) != 0 {
		t.Fatalf("queries missing from the fixture: %v", srv.Unknown)
	}

	sends, err := v.Sends(context.Background(), "ethereum-1", pin)
	if err != nil {
		t.Fatal(err)
	}
	if len(sends) != 1 || sends[0].Key != "ethereum-1/00000000000000000001" || sends[0].Amount.String() != "2000000000" || sends[0].Outcome != "" {
		t.Fatalf("sends %+v", sends)
	}
}

func TestLiveLevel(t *testing.T) {
	cap := model.MustAmount("86400")
	at := int64(1000)
	// 100 seconds later: +100 (cap/86400 per second).
	got := LiveLevel(model.MustAmount("10"), at, time.Unix(1100, 0), cap)
	if got.String() != "110" {
		t.Fatalf("got %s", got)
	}
	// Never above the cap.
	got = LiveLevel(model.MustAmount("10"), at, time.Unix(at+200000, 0), cap)
	if got.String() != "86400" {
		t.Fatalf("got %s", got)
	}
	// A stored level above a lowered cap is cut to it; no backdating.
	got = LiveLevel(model.MustAmount("100000"), at, time.Unix(at, 0), cap)
	if got.String() != "86400" {
		t.Fatalf("got %s", got)
	}
	// Clock behind the stored time: no refill.
	got = LiveLevel(model.MustAmount("10"), at, time.Unix(at-5, 0), cap)
	if got.String() != "10" {
		t.Fatalf("got %s", got)
	}
}

func TestGovernance(t *testing.T) {
	v, pin, srv := devnet(t)
	g, err := v.Governance(context.Background(), []string{"ethereum-1"}, pin)
	if err != nil {
		t.Fatal(err)
	}
	if g.Governor != "g1governor" || g.Yielded != 0 || g.GovDAOGoverns {
		t.Fatalf("governor: %+v", g)
	}
	if len(g.Seats) != 6 || g.Seats[0].Address != "g1guardian0" || g.Seats[2].Address != "" || g.Seats[4].Role != "watch" || g.Seats[4].Address != "g1watch0" {
		t.Fatalf("seats: %+v", g.Seats)
	}
	if !g.Hold.Held || g.Hold.Since != 1776600000 {
		t.Fatalf("hold: %+v", g.Hold)
	}
	if len(g.Flags) != 3 {
		t.Fatalf("flags: %+v", g.Flags)
	}
	f := g.Flags[0]
	if f.Function != "payouts" || f.Seat != 0 || f.At != 1776600000 || len(f.Naming) != 2 || f.Naming[1] != "ethereum-1/00000000000000000009" {
		t.Fatalf("flag: %+v", f)
	}
	if g.Flags[2].Function != "sends" || g.Flags[2].Seat != 4 || g.Flags[2].Naming != nil {
		t.Fatalf("flag: %+v", g.Flags[2])
	}
	if g.Epoch != 1 || g.EpochClient != "ethereum-1" || g.LastSeq != 2 {
		t.Fatalf("channel: %+v", g)
	}
	if len(g.Decisions) != 3 || g.Decisions[1].State != "pending" || g.Decisions[1].Due != 1777104800 || g.Decisions[1].Note != "deferred until 1778414400" {
		t.Fatalf("decisions: %+v", g.Decisions)
	}
	if len(g.Dispatches) != 2 || g.Dispatches[0].Kind != 4 || g.Dispatches[0].Status != 1 || !g.Dispatches[1].InFlight || g.Dispatches[1].Kind != 1 {
		t.Fatalf("dispatches: %+v", g.Dispatches)
	}
	epoch, at, err := v.PayoutsEpoch(context.Background(), "ethereum-1", pin)
	if err != nil || epoch != 1 || at != 1760000000 {
		t.Fatalf("payouts epoch %d %d %v", epoch, at, err)
	}
	if len(srv.Unknown) != 0 {
		t.Fatalf("queries missing from the fixture: %v", srv.Unknown)
	}
}

func TestUnknownQueryIsAnError(t *testing.T) {
	v, pin, _ := devnet(t)
	if _, err := v.Route(context.Background(), "nope", pin); err == nil {
		t.Fatal("expected an error for a route the fixture does not hold")
	}
}
