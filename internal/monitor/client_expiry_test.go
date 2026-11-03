package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

func TestClientExpiry(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cfg := DefaultClientExpiryConfig()
	mk := func(id string, chain model.Chain, kind model.ClientKind, status model.ClientStatus, head time.Time, tp time.Duration) model.Client {
		return model.Client{Chain: chain, ID: id, Kind: kind, Status: status, HeadTime: head, TrustingPeriod: tp, Expiry: head.Add(tp), Provenance: model.Provenance{Chain: chain, Height: 1}}
	}
	cases := []struct {
		name     string
		client   model.Client
		severity model.Severity
		alarm    bool
	}{
		{"active, fresh", mk("ethereum-1", model.ChainGno, model.ClientKindEthereum, model.ClientActive, now.Add(-1*time.Hour), 9*24*time.Hour), model.SeverityInfo, false},
		{"Arc active", mk("arc-1", model.ChainGno, model.ClientKindArc, model.ClientActive, now.Add(-time.Second), 14*24*time.Hour), model.SeverityInfo, false},
		{"Arc stale", mk("arc-1", model.ChainGno, model.ClientKindArc, model.ClientActive, now.Add(-25*time.Hour), 14*24*time.Hour), model.SeverityWarning, true},
		{"Arc at expiry", mk("arc-1", model.ChainGno, model.ClientKindArc, model.ClientActive, now.Add(-14*24*time.Hour), 14*24*time.Hour), model.SeverityCritical, true},
		{"Arc frozen", mk("arc-1", model.ChainGno, model.ClientKindArc, model.ClientFrozen, now, 14*24*time.Hour), model.SeverityCritical, true},
		{"stale head", mk("ethereum-1", model.ChainGno, model.ClientKindEthereum, model.ClientActive, now.Add(-30*time.Hour), 9*24*time.Hour), model.SeverityWarning, true},
		{"warn before expiry", mk("client-0", model.ChainEthereum, model.ClientKindGnoLightClient, model.ClientActive, now.Add(-12*24*time.Hour), 14*24*time.Hour), model.SeverityWarning, true},
		{"critical before expiry", mk("client-0", model.ChainEthereum, model.ClientKindGnoLightClient, model.ClientActive, now.Add(-14*24*time.Hour+6*time.Hour), 14*24*time.Hour), model.SeverityCritical, true},
		{"expired by time", mk("client-0", model.ChainEthereum, model.ClientKindGnoLightClient, model.ClientActive, now.Add(-15*24*time.Hour), 14*24*time.Hour), model.SeverityCritical, true},
		{"expired by status", mk("tm-1", model.ChainGno, model.ClientKindTendermint, model.ClientExpired, now.Add(-1*time.Hour), 14*24*time.Hour), model.SeverityCritical, true},
		{"frozen", mk("client-0", model.ChainEthereum, model.ClientKindGnoLightClient, model.ClientFrozen, now, 14*24*time.Hour), model.SeverityCritical, true},
		{"unknown expiry", model.Client{Chain: model.ChainGno, ID: "x", Kind: model.ClientKindEthereum, Status: model.ClientActive}, model.SeverityWarning, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := ClientExpiry{Config: cfg}.evaluate(tc.client, now)
			if f.Severity != tc.severity {
				t.Fatalf("severity %s, want %s (%s)", f.Severity, tc.severity, f.Title)
			}
			if (f.AlarmKey != "") != tc.alarm {
				t.Fatalf("alarm key %q, want alarm=%v (%s)", f.AlarmKey, tc.alarm, f.Title)
			}
			if f.AutoFlag {
				t.Fatal("liveness never auto-flags")
			}
			if len(f.Provenance) != 1 {
				t.Fatal("provenance missing")
			}
		})
	}
}

func TestClientExpiryRun(t *testing.T) {
	st := store.NewMemory(10)
	now := time.Now()
	r := ClientExpiry{Config: DefaultClientExpiryConfig()}.Run(context.Background(), st, now)
	if r.Status != model.CheckSkipped || r.Reason != "no collection yet" {
		t.Fatalf("empty store: %+v", r)
	}
	// A collection that read both chains: snapshots of collection 1 are fresh.
	st.PutSnapshot(store.Snapshot{Kind: store.KindPins, Key: "latest", Value: Pins{Collection: 1, GnoState: ChainState{OK: true, Verified: true, Complete: true, LastCollection: 1}, EthState: ChainState{OK: true, Verified: true, Complete: true, LastCollection: 1}}})
	r = ClientExpiry{Config: DefaultClientExpiryConfig()}.Run(context.Background(), st, now)
	if r.Status != model.CheckSkipped || r.Reason != "no client snapshot yet" {
		t.Fatalf("no clients: %+v", r)
	}
	ok := model.Client{Chain: model.ChainGno, ID: "a", Kind: model.ClientKindEthereum, Status: model.ClientActive, HeadTime: now, TrustingPeriod: time.Hour * 24 * 9, Expiry: now.Add(9 * 24 * time.Hour)}
	bad := model.Client{Chain: model.ChainEthereum, ID: "client-0", Address: "0x1", Kind: model.ClientKindGnoLightClient, Status: model.ClientExpired, HeadTime: now.Add(-20 * 24 * time.Hour), TrustingPeriod: 14 * 24 * time.Hour, Expiry: now.Add(-6 * 24 * time.Hour)}
	ok.Provenance.Collection, bad.Provenance.Collection = 1, 1
	st.PutSnapshot(store.Snapshot{Kind: store.KindGnoClients, Key: "a", Value: ok, Provenance: ok.Provenance})
	st.PutSnapshot(store.Snapshot{Kind: store.KindEthClients, Key: "0x1", Value: bad, Provenance: bad.Provenance})
	r = ClientExpiry{Config: DefaultClientExpiryConfig()}.Run(context.Background(), st, now)
	if r.Status != model.CheckAlarm || len(r.Findings) != 2 {
		t.Fatalf("result %+v", r)
	}
	var alarms int
	for _, f := range r.Findings {
		if f.AlarmKey != "" {
			alarms++
			if f.AlarmKey != "client_expiry/ethereum/client-0" {
				t.Fatalf("key %s", f.AlarmKey)
			}
		}
	}
	if alarms != 1 {
		t.Fatalf("alarms %d", alarms)
	}
}
