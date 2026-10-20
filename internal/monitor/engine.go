package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
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

	// mu serializes check evaluation and alarm application: chain-local
	// checks run on the collecting goroutines.
	mu    sync.Mutex
	skips map[string]int
}

// ChainLocal is a check that needs one chain's collection only; the engine
// runs it as soon as that chain's bundle is published, without waiting for
// the other chain (docs/design.md §7.11). Checks without it are cross-chain
// and run after both collections.
type ChainLocal interface {
	LocalChain() model.Chain
}

func chainOf(c Check) model.Chain {
	if cl, ok := c.(ChainLocal); ok {
		return cl.LocalChain()
	}
	return ""
}

// RunOnce collects and evaluates every check once: the chain-local checks
// of each chain when its collection completes, the cross-chain ones after
// both.
func (e *Engine) RunOnce(ctx context.Context) []model.CheckResult {
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	var results []model.CheckResult
	var rmu sync.Mutex
	run := func(c Check) {
		r := e.runCheck(ctx, c, now)
		rmu.Lock()
		results = append(results, r)
		rmu.Unlock()
	}
	if e.Collector != nil {
		e.Collector.CollectWith(ctx, func(chain model.Chain, _ Pins) {
			for _, c := range e.Checks {
				if chainOf(c) == chain {
					run(c)
				}
			}
		})
	}
	if e.Alerts != nil {
		e.mu.Lock()
		e.applyChainAlarms(ctx, now)
		e.mu.Unlock()
	}
	for _, c := range e.Checks {
		if chainOf(c) != "" && e.Collector != nil {
			continue // ran on its chain's completion
		}
		run(c)
	}
	if e.Alerts != nil {
		e.Alerts.Reconcile(ctx)
	}
	return results
}

// runCheck evaluates one check and applies its alarms, under the engine's
// lock.
func (e *Engine) runCheck(ctx context.Context, c Check, now time.Time) model.CheckResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.skips == nil {
		e.skips = map[string]int{}
	}
	r := c.Run(ctx, e.Store, now)
	// A skip and an error both mean "not evaluated" for the deferred
	// alarm (docs/design.md §8.3).
	if r.Status == model.CheckSkipped || r.Status == model.CheckError {
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
	return r
}

// applyChainAlarms raises collection_unavailable/<chain> after N consecutive
// failed collections and verification_failing/<chain> after N consecutive
// unverified ones, per chain, whatever the other chain does (§7.11).
func (e *Engine) applyChainAlarms(ctx context.Context, now time.Time) {
	threshold := e.DeferredAlarmFailures
	if threshold <= 0 {
		threshold = 6
	}
	pins, ok := latestPins(e.Store)
	if !ok {
		return
	}
	r := model.CheckResult{Check: "collection", Status: model.CheckOK, StartedAt: now, FinishedAt: now, Findings: []model.Finding{}}
	for _, ch := range []model.Chain{model.ChainGno, model.ChainEthereum} {
		st := pins.State(ch)
		if st.ConsecutiveFailures >= threshold {
			r.Status = model.CheckAlarm
			r.Findings = append(r.Findings, model.Finding{
				Subject:  string(ch),
				Severity: model.SeverityCritical,
				Title:    fmt.Sprintf("%s could not be read %d times in a row", ch, st.ConsecutiveFailures),
				Detail:   st.Error,
				AlarmKey: "collection_unavailable/" + string(ch),
			})
		}
		if st.ConsecutiveUnverified >= threshold {
			r.Status = model.CheckAlarm
			r.Findings = append(r.Findings, model.Finding{
				Subject:  string(ch),
				Severity: model.SeverityCritical,
				Title:    fmt.Sprintf("%s headers did not agree %d times in a row", ch, st.ConsecutiveUnverified),
				Detail:   "the second provider disagrees or is unavailable; nothing from this chain is evaluated until it agrees (design §3.3)",
				AlarmKey: "verification_failing/" + string(ch),
			})
		}
		// A genuine but old head (design §7.11): a liveness finding that
		// fails readiness, never an auto-flag.
		if st.ConsecutiveStale >= threshold {
			r.Status = model.CheckAlarm
			r.Findings = append(r.Findings, model.Finding{
				Subject:  string(ch),
				Severity: model.SeverityCritical,
				Title:    fmt.Sprintf("%s pinned a stale head %d times in a row", ch, st.ConsecutiveStale),
				Detail:   st.StaleReason + "; nothing from this chain is evaluated until its pin is current again",
				AlarmKey: "head_stale/" + string(ch),
			})
		}
	}
	e.Alerts.Apply(ctx, r)
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

// Run loops until the context ends: one loop per chain, each collecting its
// chain at the interval, running the chain's local checks, then the
// cross-chain checks and the chain alarms over the latest bundles, so a
// chain whose provider stalls never costs the other its cadence
// (docs/design.md §7.11). Without a collector, one loop runs every check.
func (e *Engine) Run(ctx context.Context) {
	log := e.Log
	if log == nil {
		log = slog.Default()
	}
	interval := e.Interval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	report := func(results []model.CheckResult) {
		for _, r := range results {
			log.Info("check", "name", r.Check, "status", r.Status, "findings", len(r.Findings), "reason", r.Reason)
		}
	}
	var chains []model.Chain
	if e.Collector != nil {
		chains = e.Collector.Chains()
	}
	if len(chains) == 0 {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			report(e.RunOnce(ctx))
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}
	var wg sync.WaitGroup
	for _, chain := range chains {
		wg.Add(1)
		go func(chain model.Chain) {
			defer wg.Done()
			t := time.NewTicker(interval)
			defer t.Stop()
			for {
				e.Collector.CollectChain(ctx, chain)
				report(e.afterChain(ctx, chain))
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}(chain)
	}
	wg.Wait()
}

// afterChain runs, once a chain has published, its chain-local checks, then
// the chain alarms, the cross-chain checks and the sinks' reconciliation.
func (e *Engine) afterChain(ctx context.Context, chain model.Chain) []model.CheckResult {
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	var results []model.CheckResult
	for _, c := range e.Checks {
		if chainOf(c) == chain {
			results = append(results, e.runCheck(ctx, c, now))
		}
	}
	if e.Alerts != nil {
		e.mu.Lock()
		e.applyChainAlarms(ctx, now)
		e.mu.Unlock()
	}
	for _, c := range e.Checks {
		if chainOf(c) == "" {
			results = append(results, e.runCheck(ctx, c, now))
		}
	}
	if e.Alerts != nil {
		e.Alerts.Reconcile(ctx)
	}
	return results
}
