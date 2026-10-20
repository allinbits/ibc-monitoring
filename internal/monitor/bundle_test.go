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
	f, err := gnorpctest.Load("../gnoviews/testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	gsrv := gnorpctest.New(f)
	t.Cleanup(gsrv.Close)
	gno := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-fixture")), gnoviews.DefaultPaths)
	gno2 := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-second")), gnoviews.DefaultPaths)
	return f, gno, gno2
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
	if pins.Fresh(model.ChainEthereum, snap.Provenance) {
		t.Fatal("an incomplete collection counted as fresh")
	}
	if r := (RouteRelationship{}).Run(context.Background(), st, time.Now()); r.Status != model.CheckSkipped || len(r.Unevaluated) != 1 {
		t.Fatalf("relationship on incomplete: %+v", r)
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
	if er.GnoDiscovery != pins.Collection || er.GnoRoute == "" || er.GnoCounterparty != "client-0" || er.RelationshipMismatch != "" {
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
	if disc.Collection != pins.Collection || len(disc.Routes) == 0 || len(disc.Clients) == 0 {
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
}
