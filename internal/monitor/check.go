// Package monitor is the T4 engine: it collects snapshots from both chains
// into the store, runs the checks against them and hands the findings to the
// alert manager (docs/design.md §3.2, §7).
package monitor

import (
	"context"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// Check evaluates one rule against the store's snapshots. It never reads the
// chains itself: its inputs are what the dashboard shows.
type Check interface {
	Name() string
	Run(ctx context.Context, st store.Store, now time.Time) model.CheckResult
}

// Skip builds a skipped result (data unavailable; retried; §8.3).
func Skip(name string, started time.Time, reason string) model.CheckResult {
	return model.CheckResult{Check: name, Status: model.CheckSkipped, StartedAt: started, FinishedAt: time.Now(), Reason: reason, Findings: []model.Finding{}}
}

// Done builds a finished result whose status follows its worst finding.
func Done(name string, started time.Time, findings []model.Finding, prov []model.Provenance) model.CheckResult {
	status := model.CheckOK
	for _, f := range findings {
		if f.AlarmKey != "" {
			status = model.CheckAlarm
			break
		}
	}
	if findings == nil {
		findings = []model.Finding{}
	}
	return model.CheckResult{Check: name, Status: status, StartedAt: started, FinishedAt: time.Now(), Findings: findings, Provenance: prov}
}

// TODO(phase 3, docs/design.md §7.2–§7.5): closed_pair, supply_identity,
// orphan_receives and proof_heights. TODO(phase 2, §7.6–§7.7):
// recovery_candidate, relay_stall, claims_age, float_runway, quarantine,
// maturity, gov_ledger.
