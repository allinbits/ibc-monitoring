package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethviews"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc/gnorpctest"
	"github.com/allinbits/ibc-monitoring/internal/gnoviews"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// A provider that accepts requests and never answers is a failed collection
// for its chain within the collection bound, and the other chain's
// collection proceeds meanwhile (review finding 9).
func TestStalledProviderIsBounded(t *testing.T) {
	f, err := gnorpctest.Load("../gnoviews/testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	gsrv := gnorpctest.New(f)
	t.Cleanup(gsrv.Close)
	gno := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-fixture")), gnoviews.DefaultPaths)
	gno2 := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-second")), gnoviews.DefaultPaths)

	done := make(chan struct{})
	hanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-done }))
	t.Cleanup(hanging.Close)
	t.Cleanup(func() { close(done) })
	ec, err := ethrpc.Dial(context.Background(), hanging.URL, "hanging", ethrpc.Finality{Mode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	eth, err := ethviews.New(ec, ethviews.Addresses{Admin: common.HexToAddress("0xaaaa"), Transfer: common.HexToAddress("0xbbbb")})
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory(10)
	c := &Collector{Gno: gno, Gno2: gno2, Eth: eth, Eth2: ec, RequireCrossCheck: true, Store: st, Timeout: 300 * time.Millisecond}
	start := time.Now()
	pins := c.Collect(context.Background())
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the collection was not bounded: %s", elapsed)
	}
	if pins.EthState.OK || !strings.Contains(pins.EthState.Error, "timed out") || pins.EthState.ConsecutiveFailures != 1 {
		t.Fatalf("stalled ethereum: %+v", pins.EthState)
	}
	if !pins.GnoState.OK || !pins.GnoState.Verified {
		t.Fatalf("gno should have collected: %+v", pins.GnoState)
	}
	if len(st.Snapshots(store.KindGnoClients)) == 0 {
		t.Fatal("no gno snapshots")
	}
	// A second collection counts the failure again, never blocks.
	pins = c.Collect(context.Background())
	if pins.EthState.ConsecutiveFailures != 2 || !pins.GnoState.OK {
		t.Fatalf("second collection: %+v %+v", pins.EthState, pins.GnoState)
	}
}
