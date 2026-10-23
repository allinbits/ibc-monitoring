package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/ethviews"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// RouteRelationship is the cross-chain comparison of a route's two views
// (docs/design.md §4.2): Ethereum's own current client and generations
// against the counterparty gno.land's route client names and its replaced
// clients. It is recomputed at evaluation from the two selected bundles —
// the fresh Ethereum route and the fresh gno discovery — and its finding
// carries both provenances; it defers while either chain is not fresh.
// Alarm only (`warning`); it runs after both collections.
type RouteRelationship struct{}

func (RouteRelationship) Name() string { return "route_relationship" }

func (c RouteRelationship) Run(_ context.Context, st store.Store, now time.Time) model.CheckResult {
	started := now
	pins, ok := latestPins(st)
	if !ok {
		return Skip(c.Name(), started, "no collection yet")
	}
	ds, ok := st.Snapshot(store.KindGnoDiscovery, "latest")
	disc, _ := ds.Value.(GnoDiscovery)
	gnoFresh := ok && pins.GnoState.OK && pins.GnoState.Verified && disc.Collection == pins.GnoState.LastCollection
	var findings []model.Finding
	var prov []model.Provenance
	var unevaluated []string
	for _, s := range st.Snapshots(store.KindEthRoutes) {
		er, ok := s.Value.(model.EthRoute)
		if !ok {
			continue
		}
		subject := er.RouteKey
		key := "route_relationship/" + subject
		// Both inputs must be fresh: the Ethereum route from its chain's
		// latest collection, the gno discovery from gno.land's (readiness
		// per subject, docs/design.md §7.11).
		if !pins.Fresh(model.ChainEthereum, s.Provenance) || !gnoFresh {
			unevaluated = append(unevaluated, key)
			continue
		}
		d := ethviews.RouteDiscovery{RouteKey: er.RouteKey, CurrentClient: er.CurrentClient, Migrations: er.Migrations}
		seen := map[string]bool{}
		for _, m := range er.Migrations {
			if m.Old != er.CurrentClient && !seen[m.Old] {
				seen[m.Old] = true
				d.Retired = append(d.Retired, m.Old)
			}
		}
		gp := disc.Provenance()
		f := model.Finding{Subject: subject, Severity: model.SeverityInfo, Provenance: []model.Provenance{s.Provenance, gp}, Values: map[string]string{
			"current_client": er.CurrentClient,
			"gno_discovery":  fmt.Sprintf("%d", disc.Collection),
		}}
		r, cp, found := matchGnoRoute(disc, d)
		mismatch := ""
		if found {
			f.Values["gno_route"], f.Values["gno_counterparty"] = r.Key, cp
			mismatch = relationship(disc, r, cp, d)
		} else {
			mismatch = fmt.Sprintf("no gno.land route names Ethereum's route %s (current client %s) as its counterparty", d.RouteKey, d.CurrentClient)
		}
		if mismatch != "" {
			f.Severity, f.AlarmKey = model.SeverityWarning, key
			f.Title = fmt.Sprintf("route %s: gno.land and Ethereum disagree on its clients", subject)
			f.Detail = mismatch + " (a replacement or recovery one chain executed before the other processed it, or a misconfiguration)"
		} else {
			f.Title = fmt.Sprintf("route %s: gno.land route %s names Ethereum's current client", subject, r.Key)
		}
		findings = append(findings, f)
		prov = append(prov, s.Provenance, gp)
	}
	if len(findings) == 0 {
		r := Skip(c.Name(), started, "no fresh Ethereum route with a fresh gno discovery")
		r.Unevaluated = unevaluated
		return r
	}
	r := Done(c.Name(), started, findings, prov)
	r.Unevaluated = unevaluated
	return r
}
