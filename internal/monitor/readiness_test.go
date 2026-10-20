package monitor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/alert"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc/gnorpctest"
	"github.com/allinbits/ibc-monitoring/internal/gnoviews"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// A chain that fails to collect leaves its snapshots stale: the check skips
// those subjects, says so, and the deferred counter advances when nothing
// fresh is left (review finding 12).
func TestStaleSubjectsAreSkipped(t *testing.T) {
	f, err := gnorpctest.Load("../gnoviews/testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	gsrv := gnorpctest.New(f)
	gno := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-fixture")), gnoviews.DefaultPaths)
	gno2 := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-second")), gnoviews.DefaultPaths)
	eth, ethc, esrv := ethFixture(t, f.Time.Add(-time.Hour))
	st := store.NewMemory(10)
	sink := &recordingSink{}
	eng := &Engine{
		Collector:             &Collector{Gno: gno, Gno2: gno2, Eth: eth, Eth2: ethc, RequireCrossCheck: true, Store: st},
		Checks:                []Check{ClientExpiry{Config: DefaultClientExpiryConfig()}},
		Alerts:                alert.NewManager(st, nil, sink),
		Store:                 st,
		DeferredAlarmFailures: 2,
		Now:                   func() time.Time { return f.Time },
	}
	// Three clients: the fixture's expired Tendermint client alarms, the
	// other two are fine.
	r := eng.RunOnce(context.Background())[0]
	if r.Status != model.CheckAlarm || len(r.Findings) != 3 || r.Reason != "" {
		t.Fatalf("first run: %+v", r)
	}
	// gno.land goes away: its two clients are stale, the Ethereum one is
	// still evaluated, and the result says which were skipped.
	gsrv.Close()
	r = eng.RunOnce(context.Background())[0]
	if r.Status != model.CheckOK || len(r.Findings) != 1 || r.Findings[0].Subject != "ethereum/client-0" {
		t.Fatalf("stale gno: %+v", r)
	}
	if len(r.Unevaluated) != 2 || r.Unevaluated[1] != "client_expiry/gno/tm-1" {
		t.Fatalf("unevaluated %v", r.Unevaluated)
	}
	// The expired Tendermint client's alarm is kept: its subject was not
	// evaluated, so nothing says it recovered.
	if a, ok := st.Alarm("client_expiry/gno/tm-1"); !ok || a.State != model.AlarmRaised {
		t.Fatalf("stale subject's alarm cleared: %+v", a)
	}
	if !strings.Contains(r.Reason, "2 stale subject(s) not evaluated") || !strings.Contains(r.Reason, "gno/ethereum-1") || !strings.Contains(r.Reason, "gno: ") {
		t.Fatalf("reason: %q", r.Reason)
	}
	pins, _ := latestPins(st)
	if pins.GnoState.OK || pins.GnoState.ConsecutiveFailures != 1 || pins.GnoState.LastCollection != 1 || !pins.EthState.OK || pins.EthState.LastCollection != 2 {
		t.Fatalf("states: %+v %+v", pins.GnoState, pins.EthState)
	}
	// The gno client snapshots are still served, marked by their collection.
	for _, s := range st.Snapshots(store.KindGnoClients) {
		if pins.Fresh(model.ChainGno, s.Provenance) {
			t.Fatal("a stale snapshot reads as fresh")
		}
	}
	// The error text in the pins is redacted (no user info, no query).
	if strings.ContainsAny(pins.GnoState.Error, "@?") {
		t.Fatalf("unredacted error: %s", pins.GnoState.Error)
	}
	// Ethereum goes away too: nothing fresh, the check skips and the
	// deferred alarm fires after two non-evaluations.
	esrv.Close()
	r = eng.RunOnce(context.Background())[0]
	if r.Status != model.CheckSkipped || !strings.Contains(r.Reason, "stale") {
		t.Fatalf("all stale: %+v", r)
	}
	r = eng.RunOnce(context.Background())[0]
	if r.ConsecutiveSkips != 2 {
		t.Fatalf("skips %d", r.ConsecutiveSkips)
	}
	if _, ok := st.Alarm("client_expiry_unavailable"); !ok {
		t.Fatal("deferred alarm missing")
	}
	// Per chain: gno failed four times, Ethereum twice → both alarm at 2.
	for _, k := range []string{"collection_unavailable/gno", "collection_unavailable/ethereum"} {
		if a, ok := st.Alarm(k); !ok || a.State != model.AlarmRaised {
			t.Fatalf("%s missing", k)
		}
	}
}

// A collection whose header does not agree is not fresh: its subjects are
// not evaluated, and sustained disagreement alarms per chain.
func TestUnverifiedIsNotFresh(t *testing.T) {
	f, err := gnorpctest.Load("../gnoviews/testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	gsrv := gnorpctest.New(f)
	t.Cleanup(gsrv.Close)
	gno := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-fixture")), gnoviews.DefaultPaths)
	dead := gnoviews.New(gnorpc.New("http://127.0.0.1:1", gnorpc.WithName("dead")), gnoviews.DefaultPaths)
	st := store.NewMemory(10)
	sink := &recordingSink{}
	eng := &Engine{
		Collector:             &Collector{Gno: gno, Gno2: dead, RequireCrossCheck: true, Store: st},
		Checks:                []Check{ClientExpiry{Config: DefaultClientExpiryConfig()}},
		Alerts:                alert.NewManager(st, nil, sink),
		Store:                 st,
		DeferredAlarmFailures: 2,
		Now:                   func() time.Time { return f.Time },
	}
	r := eng.RunOnce(context.Background())[0]
	if r.Status != model.CheckSkipped || len(r.Unevaluated) != 2 {
		t.Fatalf("unverified evaluated: %+v", r)
	}
	pins, _ := latestPins(st)
	if !pins.GnoState.OK || pins.GnoState.Verified || pins.GnoState.ConsecutiveUnverified != 1 {
		t.Fatalf("state %+v", pins.GnoState)
	}
	eng.RunOnce(context.Background())
	if a, ok := st.Alarm("verification_failing/gno"); !ok || a.State != model.AlarmRaised {
		t.Fatal("verification alarm missing")
	}
}

// An errored check counts toward the deferred alarm like a skip.
func TestErrorsCountAsNonEvaluation(t *testing.T) {
	st := store.NewMemory(10)
	eng := &Engine{Checks: []Check{erroring{}}, Alerts: alert.NewManager(st, nil), Store: st, DeferredAlarmFailures: 2}
	eng.RunOnce(context.Background())
	r := eng.RunOnce(context.Background())[0]
	if r.ConsecutiveSkips != 2 {
		t.Fatalf("skips %d", r.ConsecutiveSkips)
	}
	if _, ok := st.Alarm("boom_unavailable"); !ok {
		t.Fatal("deferred alarm missing after errors")
	}
}

type erroring struct{}

func (erroring) Name() string { return "boom" }
func (erroring) Run(_ context.Context, _ store.Store, now time.Time) model.CheckResult {
	return model.CheckResult{Check: "boom", Status: model.CheckError, StartedAt: now, FinishedAt: now, Reason: "boom"}
}

// Header agreement fails closed (review finding 8): a second provider that
// is configured but unavailable, or missing where required, leaves the
// snapshots unverified; only an agreeing second provider cross-checks.
func TestVerificationFailsClosed(t *testing.T) {
	f, err := gnorpctest.Load("../gnoviews/testdata/devnet.json")
	if err != nil {
		t.Fatal(err)
	}
	gsrv := gnorpctest.New(f)
	t.Cleanup(gsrv.Close)
	gno := gnoviews.New(gnorpc.New(gsrv.URL, gnorpc.WithName("gno-fixture")), gnoviews.DefaultPaths)
	dead := gnoviews.New(gnorpc.New("http://127.0.0.1:1", gnorpc.WithName("dead")), gnoviews.DefaultPaths)

	// Second provider down → unverified.
	st := store.NewMemory(10)
	pins := (&Collector{Gno: gno, Gno2: dead, RequireCrossCheck: true, Store: st}).Collect(context.Background())
	if pins.GnoVerified != model.VerificationUnverified || !strings.Contains(pins.GnoCrossCheck, "second provider unavailable") {
		t.Fatalf("down second provider: %s %s", pins.GnoVerified, pins.GnoCrossCheck)
	}
	if strings.Contains(pins.GnoCrossCheck, "127.0.0.1:1/") {
		t.Fatalf("cross-check text not redacted: %s", pins.GnoCrossCheck)
	}
	for _, s := range st.Snapshots(store.KindGnoClients) {
		if s.Provenance.Verification != model.VerificationUnverified || s.Provenance.StateVerification != model.StateNone {
			t.Fatalf("snapshot %+v", s.Provenance)
		}
	}
	// No second provider where required → unverified.
	pins = (&Collector{Gno: gno, RequireCrossCheck: true, Store: store.NewMemory(10)}).Collect(context.Background())
	if pins.GnoVerified != model.VerificationUnverified {
		t.Fatalf("no second provider: %s", pins.GnoVerified)
	}
	// No second provider, requirement off (local e2e) → provider level.
	pins = (&Collector{Gno: gno, RequireCrossCheck: false, Store: store.NewMemory(10)}).Collect(context.Background())
	if pins.GnoVerified != model.VerificationProvider {
		t.Fatalf("opt-out: %s", pins.GnoVerified)
	}
	// Agreeing second provider → cross-checked.
	pins = (&Collector{Gno: gno, Gno2: gno, RequireCrossCheck: true, Store: store.NewMemory(10)}).Collect(context.Background())
	if pins.GnoVerified != model.VerificationCrossChecked {
		t.Fatalf("agreeing: %s", pins.GnoVerified)
	}
	// A disagreeing second provider → unverified with the mismatch.
	g2 := *f
	g2.Hash = "DEADBEEF"
	q2 := map[string]string{}
	for k, v := range f.Queries {
		q2[k] = v
	}
	g2.Queries = q2
	liar := gnorpctest.New(&g2)
	t.Cleanup(liar.Close)
	pins = (&Collector{Gno: gno, Gno2: gnoviews.New(gnorpc.New(liar.URL), gnoviews.DefaultPaths), RequireCrossCheck: true, Store: store.NewMemory(10)}).Collect(context.Background())
	if pins.GnoVerified != model.VerificationUnverified || !strings.Contains(pins.GnoCrossCheck, "header mismatch") {
		t.Fatalf("disagreeing: %s %s", pins.GnoVerified, pins.GnoCrossCheck)
	}
}
