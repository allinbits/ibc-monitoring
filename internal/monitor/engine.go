package monitor

import (
	"context"
	"log/slog"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/alert"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// Engine runs the collector and the checks on a schedule.
type Engine struct {
	Collector *Collector
	Checks    []Check
	Alerts    *alert.Manager
	Store     store.Store
	Interval  time.Duration
	// DeferredAlarmFailures is how many consecutive skips of a check raise
	// its <check>_unavailable alarm (docs/design.md §8.3).
	DeferredAlarmFailures int
	Log                   *slog.Logger
	// Now is the clock the checks evaluate against (tests set it).
	Now func() time.Time

	skips map[string]int
}

// RunOnce collects and evaluates every check once.
func (e *Engine) RunOnce(ctx context.Context) []model.CheckResult {
	if e.skips == nil {
		e.skips = map[string]int{}
	}
	if e.Collector != nil {
		e.Collector.Collect(ctx)
	}
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	var results []model.CheckResult
	for _, c := range e.Checks {
		r := c.Run(ctx, e.Store, now)
		if r.Status == model.CheckSkipped {
			e.skips[c.Name()]++
		} else {
			e.skips[c.Name()] = 0
		}
		r.ConsecutiveSkips = e.skips[c.Name()]
		e.Store.PutResult(r)
		if e.Alerts != nil {
			e.Alerts.Apply(ctx, r)
			e.applyDeferred(ctx, r)
		}
		results = append(results, r)
	}
	return results
}

// applyDeferred raises <check>_unavailable after N consecutive skips, as a
// finding of a synthetic check so the ordinary alarm rules apply.
func (e *Engine) applyDeferred(ctx context.Context, r model.CheckResult) {
	threshold := e.DeferredAlarmFailures
	if threshold <= 0 {
		threshold = 6
	}
	name := r.Check + "_unavailable"
	synthetic := model.CheckResult{Check: name, Status: model.CheckOK, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Findings: []model.Finding{}}
	if r.ConsecutiveSkips >= threshold {
		synthetic.Status = model.CheckAlarm
		synthetic.Findings = []model.Finding{{
			Subject:  r.Check,
			Severity: model.SeverityWarning,
			Title:    r.Check + " could not evaluate " + itoa(r.ConsecutiveSkips) + " times in a row",
			Detail:   r.Reason,
			AlarmKey: name,
		}}
	}
	e.Alerts.Apply(ctx, synthetic)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Run loops until the context ends.
func (e *Engine) Run(ctx context.Context) {
	log := e.Log
	if log == nil {
		log = slog.Default()
	}
	interval := e.Interval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		results := e.RunOnce(ctx)
		for _, r := range results {
			log.Info("check", "name", r.Check, "status", r.Status, "findings", len(r.Findings), "reason", r.Reason)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
