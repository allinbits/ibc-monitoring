package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// RouteRelationship is the cross-chain comparison of a route's two views
// (docs/design.md §4.2): Ethereum's own current client and generations
// against the counterparty gno.land's route client names and its replaced
// clients. Alarm only (`warning`); it runs after both collections.
type RouteRelationship struct{}

func (RouteRelationship) Name() string { return "route_relationship" }

func (c RouteRelationship) Run(_ context.Context, st store.Store, now time.Time) model.CheckResult {
	started := now
	pins, ok := latestPins(st)
	if !ok {
		return Skip(c.Name(), started, "no collection yet")
	}
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
		if !pins.Fresh(model.ChainEthereum, s.Provenance) || er.GnoDiscovery == 0 {
			unevaluated = append(unevaluated, key)
			continue
		}
		f := model.Finding{Subject: subject, Severity: model.SeverityInfo, Provenance: []model.Provenance{s.Provenance}, Values: map[string]string{
			"current_client":   er.CurrentClient,
			"gno_route":        er.GnoRoute,
			"gno_counterparty": er.GnoCounterparty,
			"gno_discovery":    fmt.Sprintf("%d", er.GnoDiscovery),
		}}
		if er.RelationshipMismatch != "" {
			f.Severity, f.AlarmKey = model.SeverityWarning, key
			f.Title = fmt.Sprintf("route %s: gno.land and Ethereum disagree on its clients", subject)
			f.Detail = er.RelationshipMismatch + " (a replacement or recovery one chain executed before the other processed it, or a misconfiguration)"
		} else {
			f.Title = fmt.Sprintf("route %s: gno.land route %s names Ethereum's current client", subject, er.GnoRoute)
		}
		findings = append(findings, f)
		prov = append(prov, s.Provenance)
	}
	if len(findings) == 0 {
		r := Skip(c.Name(), started, "no fresh Ethereum route with a gno discovery")
		r.Unevaluated = unevaluated
		return r
	}
	r := Done(c.Name(), started, findings, prov)
	r.Unevaluated = unevaluated
	return r
}
