package monitor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/allinbits/ibc-monitoring/abi"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc/gnorpctest"
	"github.com/allinbits/ibc-monitoring/internal/gnoviews"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

func gnoFixture(t *testing.T) (*gnorpctest.Fixture, *gnoviews.Views, *gnoviews.Views) {
	t.Helper()
	f, _, gno, gno2 := gnoFixtureServer(t)
	return f, gno, gno2
}

func gnoFixtureServer(t *testing.T) (*gnorpctest.Fixture, *gnorpctest.Server, *gnoviews.Views, *gnoviews.Views) {
	t.Helper()
	f, err := gnorpctest.Load("../gnoviews/testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	gsrv := gnorpctest.New(f)
	t.Cleanup(gsrv.Close)
	gno := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-fixture")), gnoviews.DefaultPaths)
	gno2 := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-second")), gnoviews.DefaultPaths)
	return f, gsrv, gno, gno2
}

func ethRoute(t *testing.T, st store.Store) (model.EthRoute, store.Snapshot) {
	t.Helper()
	snaps := st.Snapshots(store.KindEthRoutes)
	if len(snaps) != 1 {
		t.Fatalf("eth routes %d", len(snaps))
	}
	return snaps[0].Value.(model.EthRoute), snaps[0]
}

// The start-up race (review finding 6): an Ethereum collection with no gno
// discovery to read is collected but incomplete, never fresh, and says what
// it lacks.
func TestEthereumWithoutGnoDiscoveryIsIncomplete(t *testing.T) {
	f, _, _ := gnoFixture(t)
	eth, ethc, _ := ethFixture(t, f.Time.Add(-time.Hour))
	st := store.NewMemory(10)
	c := &Collector{Eth: eth, Eth2: ethc, RequireCrossCheck: true, Store: st}
	pins := c.Collect(context.Background())
	if !pins.EthState.OK || !pins.EthState.Verified || pins.EthState.Complete || !strings.Contains(pins.EthState.Incomplete, "no gno discovery") {
		t.Fatalf("state %+v", pins.EthState)
	}
	er, snap := ethRoute(t, st)
	if er.GnoDiscovery != 0 || len(er.Incomplete) == 0 || er.CurrentClient != "client-0" || er.RouteKey != "client-0" {
		t.Fatalf("route %+v", er)
	}
	// Readiness is per subject (review finding 7 of the seventh round): the
	// chain is fresh for what needs nothing from gno.land — its clients and
	// governance — while the route says what it lacks and the checks that
	// depend on it defer.
	if !pins.Fresh(model.ChainEthereum, snap.Provenance) {
		t.Fatal("the Ethereum collection should be fresh for its own subjects")
	}
	clients := st.Snapshots(store.KindEthClients)
	if len(clients) != 1 || !pins.Fresh(model.ChainEthereum, clients[0].Provenance) {
		t.Fatalf("eth client snapshots %d, fresh: %v", len(clients), len(clients) == 1 && pins.Fresh(model.ChainEthereum, clients[0].Provenance))
	}
	if r := (RouteRelationship{}).Run(context.Background(), st, time.Now()); r.Status != model.CheckSkipped || len(r.Unevaluated) != 1 {
		t.Fatalf("relationship on incomplete: %+v", r)
	}
}

// Without any gno discovery, the Ethereum-local checks still run on the
// collected Ethereum subjects: a nearly expired GnoLightClient alarms while
// the route-dependent relationship check defers (seventh round, finding 7).
func TestEthereumLocalChecksRunWithoutGnoDiscovery(t *testing.T) {
	f, _, _ := gnoFixture(t)
	eth, ethc, _ := ethFixture(t, f.Time.Add(-14*24*time.Hour+6*time.Hour))
	st := store.NewMemory(10)
	eng := &Engine{
		Collector: &Collector{Eth: eth, Eth2: ethc, RequireCrossCheck: true, Store: st},
		Checks:    []Check{ClientExpiry{Config: DefaultClientExpiryConfig(), Chain: model.ChainEthereum}, RouteRelationship{}},
		Store:     st,
		Now:       func() time.Time { return f.Time },
	}
	results := eng.RunOnce(context.Background())
	byName := map[string]model.CheckResult{}
	for _, r := range results {
		byName[r.Check] = r
	}
	ce := byName["client_expiry/ethereum"]
	if ce.Status != model.CheckAlarm || len(ce.Findings) != 1 || ce.Findings[0].AlarmKey != "client_expiry/ethereum/client-0" || ce.Findings[0].Severity != model.SeverityCritical {
		t.Fatalf("ethereum expiry without gno: %+v", ce)
	}
	if rr := byName["route_relationship"]; rr.Status != model.CheckSkipped {
		t.Fatalf("relationship without gno inputs: %+v", rr)
	}
	pins, _ := latestPins(st)
	if pins.EthState.Complete || !pins.EthState.OK {
		t.Fatalf("state %+v", pins.EthState)
	}
}

// A global hold that comes entirely from flags on a client outside the
// enrolled routes is collected and explained (seventh round, finding 9):
// the flag subjects are the route keys plus the standalone clients, a
// route's own client being an alias of its key.
func TestHoldFromStandaloneClientFlags(t *testing.T) {
	f, gsrv, gno, gno2 := gnoFixtureServer(t)
	gsrv.Set(`vm/qeval|gno.land/r/aib/ibc/ibcgov.Flags("ethereum-1")`, "(slice[] []string)")
	gsrv.Set(`vm/qeval|gno.land/r/aib/ibc/ibcgov.Flags("tm-1")`, `(slice[("credits seat 0 at 1776600000" string),("payouts seat 1 at 1776600100" string)] []string)`)
	st := store.NewMemory(10)
	c := &Collector{Gno: gno, Gno2: gno2, RequireCrossCheck: true, Store: st}
	pins := c.Collect(context.Background())
	if !pins.GnoState.OK || !pins.GnoState.Verified {
		t.Fatalf("gno state %+v", pins.GnoState)
	}
	gs, ok := st.Snapshot(store.KindGnoGovernance, "ibcgov")
	if !ok {
		t.Fatal("no governance snapshot")
	}
	gov := gs.Value.(model.GnoGovernance)
	if len(gov.Flags) != 2 || gov.Flags[0].Route != "tm-1" || gov.Flags[1].Route != "tm-1" {
		t.Fatalf("flags %+v", gov.Flags)
	}
	if !gov.Hold.Held || gov.Hold.GuardianSeats != 2 || !gov.Hold.Explained {
		t.Fatalf("hold %+v", gov.Hold)
	}
	_ = f
	// The subjects: the route key once (its client is an alias), then the
	// standalone client.
	routes := []model.Route{{Key: "ethereum-1", Client: "ethereum-1", Replaced: []string{"ethereum-0"}}}
	clients := []model.Client{{ID: "ethereum-1"}, {ID: "ethereum-0"}, {ID: "tm-1"}}
	if got := flagSubjects(routes, clients); len(got) != 2 || got[0] != "ethereum-1" || got[1] != "tm-1" {
		t.Fatalf("subjects %v", got)
	}
}

// With both chains collected, the Ethereum collection waits for this
// round's gno discovery and reads it as one object: the route names the
// discovery's collection, every gno snapshot carries that same collection,
// and the relationship agrees.
func TestEthereumWaitsForGnoDiscovery(t *testing.T) {
	f, gno, gno2 := gnoFixture(t)
	eth, ethc, _ := ethFixture(t, f.Time.Add(-time.Hour))
	st := store.NewMemory(10)
	c := &Collector{Gno: gno, Gno2: gno2, Eth: eth, Eth2: ethc, RequireCrossCheck: true, Store: st}
	pins := c.Collect(context.Background())
	if !pins.EthState.Complete || !pins.GnoState.Complete || pins.EthState.Incomplete != "" {
		t.Fatalf("states %+v %+v", pins.GnoState, pins.EthState)
	}
	er, snap := ethRoute(t, st)
	if er.GnoDiscovery != pins.GnoState.LastCollection || er.GnoRoute == "" || er.GnoCounterparty != "client-0" || er.RelationshipMismatch != "" {
		t.Fatalf("route %+v", er)
	}
	if !pins.Fresh(model.ChainEthereum, snap.Provenance) {
		t.Fatal("complete collection not fresh")
	}
	ds, ok := st.Snapshot(store.KindGnoDiscovery, "latest")
	if !ok {
		t.Fatal("no discovery")
	}
	disc := ds.Value.(GnoDiscovery)
	if disc.Collection != pins.GnoState.LastCollection || len(disc.Routes) == 0 || len(disc.Clients) == 0 {
		t.Fatalf("discovery %+v", disc)
	}
	for _, kind := range []string{store.KindGnoClients, store.KindGnoRoutes, store.KindGnoGovernance} {
		for _, s := range st.Snapshots(kind) {
			if s.Provenance.Collection != disc.Collection {
				t.Fatalf("%s/%s from collection %d, discovery %d: not one bundle", kind, s.Key, s.Provenance.Collection, disc.Collection)
			}
		}
	}
	if r := (RouteRelationship{}).Run(context.Background(), st, time.Now()); r.Status != model.CheckOK || len(r.Findings) != 1 || r.Findings[0].AlarmKey != "" {
		t.Fatalf("relationship: %+v", r)
	}
}

// The replacement race (review findings 6 and 7): Ethereum has migrated the
// route to a successor (currentClient and its own history say so) while
// gno.land still names the old client. The fresh Ethereum snapshot carries
// Ethereum's current client and generations, the gno relationship is
// reported as a mismatch by the cross-chain check, and nothing attributes
// the old client to the fresh snapshot.
func TestReplacementBeforeGnoProcessesItIsAMismatch(t *testing.T) {
	f, gno, gno2 := gnoFixture(t)
	eth, ethc, esrv := ethFixture(t, f.Time.Add(-time.Hour))
	xfer := common.HexToAddress("0xbbbb")
	esrv.Returns(xfer, "currentClient", "client-1")
	ev := abi.MustLoad(abi.Transfer).Events["GnoRouteMigrated"]
	data, err := ev.Inputs.Pack("client-0", "client-1", "client-0")
	if err != nil {
		t.Fatal(err)
	}
	esrv.AddLog(types.Log{Address: xfer, Topics: []common.Hash{ev.ID}, Data: data, BlockNumber: 50})
	st := store.NewMemory(10)
	c := &Collector{Gno: gno, Gno2: gno2, Eth: eth, Eth2: ethc, RequireCrossCheck: true, Store: st}
	pins := c.Collect(context.Background())
	if !pins.EthState.Complete {
		t.Fatalf("state %+v", pins.EthState)
	}
	er, _ := ethRoute(t, st)
	if er.CurrentClient != "client-1" || len(er.Migrations) != 1 || er.Migrations[0].Old != "client-0" || er.Migrations[0].Block != 50 {
		t.Fatalf("route %+v", er)
	}
	if er.GnoCounterparty != "client-0" || !strings.Contains(er.RelationshipMismatch, "client-1") || !strings.Contains(er.RelationshipMismatch, "client-0") {
		t.Fatalf("relationship %+v", er)
	}
	if len(er.Recon) != 2 || er.Recon[0].ClientID != "client-1" || er.Recon[1].ClientID != "client-0" {
		t.Fatalf("recon %+v", er.Recon)
	}
	r := (RouteRelationship{}).Run(context.Background(), st, time.Now())
	if r.Status != model.CheckAlarm || len(r.Findings) != 1 || r.Findings[0].AlarmKey != "route_relationship/client-0" || r.Findings[0].Severity != model.SeverityWarning {
		t.Fatalf("relationship check: %+v", r)
	}
	// Recomputed from both bundles, with both provenances.
	if p := r.Findings[0].Provenance; len(p) != 2 || p[0].Chain != model.ChainEthereum || p[1].Chain != model.ChainGno || p[1].Collection != pins.GnoState.LastCollection {
		t.Fatalf("provenance %+v", p)
	}
}

// Both chains configured and gno.land failing from the start (eighth round,
// finding 4): the Ethereum collection waits for a discovery at most
// DiscoveryWait, then publishes its own subjects — the nearly expired
// GnoLightClient alarms — with the route incomplete, well within its
// deadline, and never reads the route under an expired context.
func TestEthereumPublishesWhileGnoFails(t *testing.T) {
	f, gsrv, gno, gno2 := gnoFixtureServer(t)
	gsrv.Close()
	eth, ethc, _ := ethFixture(t, f.Time.Add(-14*24*time.Hour+6*time.Hour))
	st := store.NewMemory(10)
	eng := &Engine{
		Collector: &Collector{Gno: gno, Gno2: gno2, Eth: eth, Eth2: ethc, RequireCrossCheck: true, Store: st, Timeout: 10 * time.Second, DiscoveryWait: 200 * time.Millisecond},
		Checks:    []Check{ClientExpiry{Config: DefaultClientExpiryConfig(), Chain: model.ChainEthereum}, RouteRelationship{}},
		Store:     st,
		Now:       func() time.Time { return f.Time },
	}
	start := time.Now()
	results := eng.RunOnce(context.Background())
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the Ethereum collection waited %s on a failing gno.land", took)
	}
	pins, _ := latestPins(st)
	if pins.GnoState.OK || !pins.EthState.OK || pins.EthState.Complete || !strings.Contains(pins.EthState.Incomplete, "no gno discovery") {
		t.Fatalf("states %+v %+v", pins.GnoState, pins.EthState)
	}
	for _, r := range results {
		switch r.Check {
		case "client_expiry/ethereum":
			if r.Status != model.CheckAlarm || len(r.Findings) != 1 || r.Findings[0].AlarmKey != "client_expiry/ethereum/client-0" {
				t.Fatalf("ethereum expiry: %+v", r)
			}
		case "route_relationship":
			if r.Status != model.CheckSkipped {
				t.Fatalf("relationship: %+v", r)
			}
		}
	}
	er, _ := ethRoute(t, st)
	if len(er.Incomplete) == 0 || er.GnoDiscovery != 0 {
		t.Fatalf("route %+v", er)
	}
}

// A discovery a failing gno.land left behind is stale (eighth round, finding
// 5): after a good round, gno.land goes away; the next Ethereum collection
// leaves its route incomplete instead of carrying the cached comparison, the
// relationship defers, and the Ethereum-local checks still run.
func TestStaleGnoDiscoveryLeavesRoutesIncomplete(t *testing.T) {
	f, gsrv, gno, gno2 := gnoFixtureServer(t)
	eth, ethc, _ := ethFixture(t, f.Time.Add(-14*24*time.Hour+6*time.Hour))
	st := store.NewMemory(10)
	c := &Collector{Gno: gno, Gno2: gno2, Eth: eth, Eth2: ethc, RequireCrossCheck: true, Store: st, DiscoveryWait: 200 * time.Millisecond}
	pins := c.Collect(context.Background())
	if !pins.EthState.Complete || !pins.GnoState.OK {
		t.Fatalf("first round %+v %+v", pins.GnoState, pins.EthState)
	}
	if r := (RouteRelationship{}).Run(context.Background(), st, f.Time); r.Status != model.CheckOK || len(r.Findings) != 1 || len(r.Findings[0].Provenance) != 2 {
		t.Fatalf("relationship after a good round: %+v", r)
	}
	gsrv.Close()
	pins = c.Collect(context.Background())
	if pins.GnoState.OK || !pins.EthState.OK || pins.EthState.Complete || !strings.Contains(pins.EthState.Incomplete, "stale") {
		t.Fatalf("second round %+v %+v", pins.GnoState, pins.EthState)
	}
	er, _ := ethRoute(t, st)
	if len(er.Incomplete) == 0 || er.GnoDiscovery != 0 || er.RelationshipMismatch != "" {
		t.Fatalf("route carried a cached comparison: %+v", er)
	}
	if r := (RouteRelationship{}).Run(context.Background(), st, f.Time); r.Status != model.CheckSkipped || len(r.Unevaluated) != 1 {
		t.Fatalf("relationship on a stale discovery: %+v", r)
	}
	if r := (ClientExpiry{Config: DefaultClientExpiryConfig(), Chain: model.ChainEthereum}).Run(context.Background(), st, f.Time); r.Status != model.CheckAlarm {
		t.Fatalf("ethereum expiry during the gno outage: %+v", r)
	}
}
