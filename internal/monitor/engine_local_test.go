package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/alert"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethviews"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

func (r *recordingSink) has(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Alarm.Key == key {
			return true
		}
	}
	return false
}

// A healthy chain's chain-local checks run and deliver their alarms as soon
// as that chain is collected, before the other chain's stalled collection
// reaches its deadline (review finding 8).
func TestHealthyChainChecksRunBeforeStalledDeadline(t *testing.T) {
	f, gno, gno2 := gnoFixture(t)
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
	sink := &recordingSink{}
	const deadline = 3 * time.Second
	eng := &Engine{
		Collector: &Collector{Gno: gno, Gno2: gno2, Eth: eth, Eth2: ec, RequireCrossCheck: true, Store: st, Timeout: deadline},
		Checks:    []Check{ClientExpiry{Config: DefaultClientExpiryConfig(), Chain: model.ChainGno}, ClientExpiry{Config: DefaultClientExpiryConfig(), Chain: model.ChainEthereum}},
		Alerts:    alert.NewManager(st, nil, sink),
		Store:     st,
		Now:       func() time.Time { return f.Time },
	}
	start := time.Now()
	finished := make(chan []model.CheckResult, 1)
	go func() { finished <- eng.RunOnce(context.Background()) }()
	// The fixture's expired Tendermint client alarms on gno.land; its alarm
	// must be delivered well before the Ethereum deadline.
	for !sink.has("client_expiry/gno/tm-1") {
		if time.Since(start) > deadline-500*time.Millisecond {
			t.Fatalf("the gno alarm was not delivered before the Ethereum deadline (%s)", time.Since(start))
		}
		time.Sleep(10 * time.Millisecond)
	}
	delivered := time.Since(start)
	results := <-finished
	if total := time.Since(start); total < deadline-500*time.Millisecond {
		t.Fatalf("the Ethereum side did not stall as the test needs (%s)", total)
	}
	if delivered > deadline-500*time.Millisecond {
		t.Fatalf("gno alarm delivered at %s, after the Ethereum deadline", delivered)
	}
	var gnoRes, ethRes *model.CheckResult
	for i := range results {
		switch results[i].Check {
		case "client_expiry/gno":
			gnoRes = &results[i]
		case "client_expiry/ethereum":
			ethRes = &results[i]
		}
	}
	if gnoRes == nil || gnoRes.Status != model.CheckAlarm {
		t.Fatalf("gno result %+v", gnoRes)
	}
	if ethRes == nil || ethRes.Status != model.CheckSkipped {
		t.Fatalf("ethereum result %+v", ethRes)
	}
	pins, _ := latestPins(st)
	if pins.EthState.OK || !pins.GnoState.OK {
		t.Fatalf("states %+v %+v", pins.GnoState, pins.EthState)
	}
}
