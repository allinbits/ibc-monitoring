package gnoviews

import (
	"context"
	"encoding/json"
	"strconv"
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
	if ugnot.Kind != "unescrow" || ugnot.Lock == nil || ugnot.Lock.String() != "200000000000" || !ugnot.LockExact {
		t.Fatalf("ugnot lock: %+v", ugnot.Lock)
	}
	if ugnot.Token != "0x4ed7c70f96b99c776995fb64377f0d4ab3b0e1c1" || ugnot.Trace != "" {
		t.Fatalf("ugnot token: %q %q", ugnot.Token, ugnot.Trace)
	}
	// The stored level is shown; the live level is never recomputed here.
	if ugnot.Meter.Level.String() != "4000000000" || ugnot.Meter.At != 1776686400 || ugnot.Meter.Epoch != 1 {
		t.Fatalf("stored meter %+v", ugnot.Meter)
	}
	if ugnot.Meter.Live != nil || ugnot.Meter.LiveSource != "unavailable" || ugnot.Meter.LiveReason == "" {
		t.Fatalf("live meter must be unavailable: %+v", ugnot.Meter)
	}
	if ugnot.RefundMeter.Live != nil || ugnot.RefundMeter.LiveSource != "unavailable" {
		t.Fatalf("live refund meter must be unavailable: %+v", ugnot.RefundMeter)
	}
	if ugnot.Totals.InFlight.String() != "2000000000" || ugnot.Totals.Owed.String() != "1000000000" || ugnot.Waiting != 1 {
		t.Fatalf("totals %+v", ugnot.Totals)
	}
	mint := r.Denoms[1]
	if mint.Kind != "mint" || mint.SupplyCap.String() != "9000000000000000000" || mint.QueuedRefunds != 2 {
		t.Fatalf("mint: %+v", mint)
	}
	// A minted denom has no remote: its token is the trace base, lower-cased,
	// over a hop of the route.
	if mint.Remote != "" || mint.Token != "0xdac17f958d2ee523a2206206994597c13d831ec7" || mint.Trace != "transfer/ethereum-1" {
		t.Fatalf("mint token: %+v", mint)
	}
	if mint.Lock.String() != "0" || !mint.LockExact {
		t.Fatalf("mint lock %s", mint.Lock)
	}
	// Saturated counters are flagged as lower bounds, never read as exact.
	if len(ugnot.Saturated) != 0 || len(mint.Saturated) != 2 || mint.Saturated[0] != "in_flight" || mint.Saturated[1] != "lost" {
		t.Fatalf("saturated: %v %v", ugnot.Saturated, mint.Saturated)
	}
	if mint.Totals.InFlight.String() != model.Int64Max {
		t.Fatalf("saturated value %s", mint.Totals.InFlight)
	}
	if len(r.Saturated) != 0 {
		t.Fatalf("route saturated %v", r.Saturated)
	}
	if len(srv.Unknown) != 0 {
		t.Fatalf("queries missing from the fixture: %v", srv.Unknown)
	}

	sends, err := v.Sends(context.Background(), "ethereum-1", pin)
	if err != nil {
		t.Fatal(err)
	}
	if len(sends) != 3 || sends[0].Key != "ethereum-1/00000000000000000001" || sends[0].Amount.String() != "2000000000" || sends[0].Outcome != "" {
		t.Fatalf("sends %+v", sends)
	}
	// Payloads are kept whole: records 1 and 2 differ only in the payload
	// value, and each commitment is checked against its own packet.
	if len(sends[0].Packet.Payloads) != 1 || string(sends[0].Packet.Payloads[0].Value) != "\x00" || string(sends[1].Packet.Payloads[0].Value) != "\x01" {
		t.Fatalf("payloads %+v %+v", sends[0].Packet.Payloads, sends[1].Packet.Payloads)
	}
	if sends[0].Packet.Payloads[0].Encoding != "application/x-solidity-abi" || !sends[0].IsTransfer {
		t.Fatalf("payload fields %+v", sends[0].Packet.Payloads[0])
	}
	if !sends[0].CommitmentOK || !sends[1].CommitmentOK || sends[0].Commitment == sends[1].Commitment {
		t.Fatalf("commitments: %v %v", sends[0].CommitmentOK, sends[1].CommitmentOK)
	}
	// Record 3 carries record 1's commitment with record 2's payload: not a
	// usable witness.
	if sends[2].CommitmentOK {
		t.Fatal("a commitment that does not match its packet must not verify")
	}
}

func TestDenomTraceRefusals(t *testing.T) {
	v, pin, srv := devnet(t)
	// A trace over a client outside the route is refused.
	srv.Set("vm/qrender|gno.land/r/aib/ibc/apps/transfer:denoms/ibc/ABCDEF", `{"base":"0xdac17f958d2ee523a2206206994597c13d831ec7","path":"transfer/other-9","denom":"ibc/ABCDEF"}`)
	if _, err := v.Routes(context.Background(), pin); err == nil {
		t.Fatal("expected an error for a foreign hop")
	}
	// A base that is not an address is refused.
	srv.Set("vm/qrender|gno.land/r/aib/ibc/apps/transfer:denoms/ibc/ABCDEF", `{"base":"uatom","path":"transfer/ethereum-1","denom":"ibc/ABCDEF"}`)
	if _, err := v.Routes(context.Background(), pin); err == nil {
		t.Fatal("expected an error for a non-address base")
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

// An Ethereum client on a chain with a non-zero genesis slot: the head time
// derived from the latest slot is genesis_time + (slot − genesis_slot) ·
// seconds_per_slot, the realm's own conversion, never off by genesis_slot
// slots (tenth round, finding 1).
func TestExpiryWithNonZeroGenesisSlot(t *testing.T) {
	f, err := gnorpctest.Load("testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	// The reader takes the clients page and each client's detail: both
	// carry the client state and the stored consensus state, so both get
	// the non-zero genesis slot and lose the stored timestamp (the test is
	// about the derivation, not the stored timestamp).
	srv := gnorpctest.New(f)
	t.Cleanup(srv.Close)
	var cs map[string]any
	for _, key := range []string{"vm/qrender|gno.land/r/aib/ibc/core:clients?page=1&limit=100", "vm/qrender|gno.land/r/aib/ibc/core:clients/ethereum-1"} {
		var doc map[string]any
		if err := json.Unmarshal([]byte(f.Queries[key]), &doc); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		items := []map[string]any{doc}
		if list, ok := doc["items"].([]any); ok {
			items = nil
			for _, it := range list {
				items = append(items, it.(map[string]any))
			}
		}
		for _, item := range items {
			if item["type"] != "ethereum" {
				continue
			}
			cs = item["client_state"].(map[string]any)
			cs["genesis_slot"] = "1000"
			delete(item, "last_consensus_state")
		}
		raw, _ := json.Marshal(doc)
		srv.Set(key, string(raw))
	}
	if cs == nil {
		t.Fatal("no ethereum client in the fixture")
	}
	v := New(gnorpc.New(srv.URL, gnorpc.WithName("fixture")), DefaultPaths)
	pin, err := v.LatestPin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	clients, err := v.Clients(context.Background(), pin)
	if err != nil {
		t.Fatal(err)
	}
	atoi := func(k string) int64 {
		n, err := strconv.ParseInt(cs[k].(string), 10, 64)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		return n
	}
	for _, c := range clients {
		if c.Kind != model.ClientKindEthereum {
			continue
		}
		want := time.Unix(atoi("genesis_time")+(int64(c.LatestHeight)-1000)*atoi("seconds_per_slot"), 0).UTC()
		if !c.HeadTime.Equal(want) {
			t.Fatalf("head time %s, want %s (slot %d, genesis slot 1000)", c.HeadTime, want, c.LatestHeight)
		}
		if !c.Expiry.Equal(want.Add(c.TrustingPeriod)) {
			t.Fatalf("expiry %s", c.Expiry)
		}
		return
	}
	t.Fatal("no ethereum client parsed")
}
