// Package alert turns check findings into stateful, deduplicated alarms and
// sends their transitions to sinks (docs/design.md §8). Delivery is
// asynchronous and bounded per sink, so a slow sink never delays the
// evaluation loop or another sink; what a sink could not take is kept as a
// pending set per alarm key and reconciled until delivered.
package alert

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// Sink receives alarm transitions.
type Sink interface {
	Name() string
	Send(ctx context.Context, e model.AlarmEvent) error
}

// DefaultQueue is the per-sink queue length.
const DefaultQueue = 256

// Options tune delivery (docs/design.md §8.2).
type Options struct {
	// Queue is the per-sink queue length (DefaultQueue when zero).
	Queue int
	// BacklogCritical is how long a sink's pending set may stay non-empty
	// before sink_backlog is critical (default 10 min).
	BacklogCritical time.Duration
	// FailingAfter is how long a sink may refuse every delivery before
	// sink_failing is raised (default 5 min).
	FailingAfter time.Duration
	// ReconcileEvery runs Reconcile on a timer (zero: only when called).
	ReconcileEvery time.Duration
}

func (o Options) withDefaults() Options {
	if o.Queue <= 0 {
		o.Queue = DefaultQueue
	}
	if o.BacklogCritical <= 0 {
		o.BacklogCritical = 10 * time.Minute
	}
	if o.FailingAfter <= 0 {
		o.FailingAfter = 5 * time.Minute
	}
	return o
}

// SinksCheck is the synthetic check whose findings are the sinks' backlog
// and failure alarms.
const SinksCheck = "sinks"

// Manager keeps alarm state and fans transitions out.
type Manager struct {
	store  store.Store
	queues []*queue
	log    *slog.Logger
	now    func() time.Time
	opts   Options
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// mu serializes Apply (a read-modify-write of the alarm state), which
	// the engine and the reconciliation timer both call.
	mu sync.Mutex
}

// NewManager returns a manager over the store with default options, one
// bounded queue and worker per sink. Close it to drain.
func NewManager(st store.Store, log *slog.Logger, sinks ...Sink) *Manager {
	return NewManagerWith(st, log, Options{}, sinks...)
}

// NewManagerWith is NewManager with options.
func NewManagerWith(st store.Store, log *slog.Logger, opts Options, sinks ...Sink) *Manager {
	if log == nil {
		log = slog.Default()
	}
	opts = opts.withDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{store: st, log: log, now: time.Now, opts: opts, ctx: ctx, cancel: cancel}
	names := map[string]int{}
	for _, s := range sinks {
		id := s.Name()
		if n := names[s.Name()]; n > 0 {
			id = fmt.Sprintf("%s#%d", s.Name(), n)
		}
		names[s.Name()]++
		q := &queue{sink: s, id: id, ch: make(chan model.AlarmEvent, opts.Queue), log: log, pending: map[string]model.AlarmEvent{}, now: m.clock}
		m.queues = append(m.queues, q)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			q.run(ctx)
		}()
	}
	if opts.ReconcileEvery > 0 {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			t := time.NewTicker(opts.ReconcileEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					m.Reconcile(ctx)
				}
			}
		}()
	}
	return m
}

func (m *Manager) clock() time.Time { return m.now() }

// Close stops the workers after they drain what they can within timeout.
func (m *Manager) Close(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		for _, q := range m.queues {
			q.drain(m.ctx)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
	m.cancel()
	m.wg.Wait()
}

// Dropped returns how many events were dropped for a full queue, per sink
// index (sinks may share a name).
func (m *Manager) Dropped() []int64 {
	out := make([]int64, len(m.queues))
	for i, q := range m.queues {
		out[i] = q.dropped.Load()
	}
	return out
}

// Pending returns the alarm keys each sink has not delivered yet, per sink
// index.
func (m *Manager) Pending() [][]string {
	out := make([][]string, len(m.queues))
	for i, q := range m.queues {
		out[i] = q.pendingKeys()
	}
	return out
}

// Apply reconciles a check's result with the alarms it owns: a finding with
// an AlarmKey raises or updates its alarm; an alarm of this check whose key
// the result no longer reports is cleared. A skipped or errored result
// changes nothing (the alarm keeps its last state until data returns).
func (m *Manager) Apply(_ context.Context, r model.CheckResult) {
	if r.Status == model.CheckSkipped || r.Status == model.CheckError {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	seen := map[string]bool{}
	for _, f := range r.Findings {
		if f.AlarmKey == "" {
			continue
		}
		seen[f.AlarmKey] = true
		a, ok := m.store.Alarm(f.AlarmKey)
		if !ok || a.State == model.AlarmCleared {
			// A new incident generation: the transition count continues,
			// so every transition of the key stays monotonically ordered
			// across reopenings (docs/design.md §8.2).
			a = model.Alarm{Key: f.AlarmKey, Check: r.Check, FirstSeen: now, State: model.AlarmRaised, Generation: a.Generation + 1, Transitions: a.Transitions}
			m.update(&a, f, now)
			m.emit("raised", a, now)
			continue
		}
		changed := a.Severity != f.Severity || a.Title != f.Title
		m.update(&a, f, now)
		if changed {
			m.emit("updated", a, now)
		} else {
			m.store.PutAlarm(a)
		}
	}
	// An alarm is cleared only by an evaluation of its subject that no longer
	// finds it: subjects the check could not judge keep theirs (§7.11).
	unevaluated := map[string]bool{}
	for _, k := range r.Unevaluated {
		unevaluated[k] = true
	}
	for _, a := range m.store.Alarms(false) {
		if a.Check != r.Check || seen[a.Key] || unevaluated[a.Key] {
			continue
		}
		a.State = model.AlarmCleared
		t := now
		a.ClearedAt = &t
		a.LastSeen = now
		m.emit("cleared", a, now)
	}
}

// Reconcile re-enqueues, for every sink, the current state of each alarm
// whose latest transition the sink has not delivered (dropped for a full
// queue, or refused), independently of any new transition, and applies the
// sinks' own alarms: sink_backlog/<sink> while a pending set is non-empty
// and sink_failing/<sink> while a sink refuses every delivery
// (docs/design.md §8.2). Never blocks: a queue with no room is retried next
// time.
func (m *Manager) Reconcile(ctx context.Context) {
	now := m.now()
	r := model.CheckResult{Check: SinksCheck, Status: model.CheckOK, StartedAt: now, FinishedAt: now, Findings: []model.Finding{}}
	for _, q := range m.queues {
		q.resend()
		pending, since, dropped, failed, failingSince := q.status()
		if pending > 0 {
			sev := model.SeverityWarning
			if !since.IsZero() && now.Sub(since) >= m.opts.BacklogCritical {
				sev = model.SeverityCritical
			}
			r.Status = model.CheckAlarm
			r.Findings = append(r.Findings, model.Finding{
				Subject: q.id, Severity: sev, AlarmKey: "sink_backlog/" + q.id,
				Title:  fmt.Sprintf("sink %s has %d undelivered alarm notification(s)", q.id, pending),
				Detail: fmt.Sprintf("%d dropped for a full queue, %d refused by the sink; the latest state of each alarm is re-sent until the sink accepts it (design §8.2)", dropped, failed),
				Values: map[string]string{"pending": fmt.Sprint(pending), "dropped": fmt.Sprint(dropped), "failed": fmt.Sprint(failed), "since": since.UTC().Format(time.RFC3339)},
			})
		}
		if !failingSince.IsZero() && now.Sub(failingSince) >= m.opts.FailingAfter {
			r.Status = model.CheckAlarm
			r.Findings = append(r.Findings, model.Finding{
				Subject: q.id, Severity: model.SeverityCritical, AlarmKey: "sink_failing/" + q.id,
				Title:  fmt.Sprintf("sink %s has refused every delivery since %s", q.id, failingSince.UTC().Format(time.RFC3339)),
				Detail: "notifications are kept pending and re-sent; check the sink's endpoint and credentials",
			})
		}
	}
	m.Apply(ctx, r)
}

func (m *Manager) update(a *model.Alarm, f model.Finding, now time.Time) {
	a.Severity = f.Severity
	a.Title = f.Title
	a.Detail = f.Detail
	a.Evidence = f
	a.AutoFlag = f.AutoFlag
	a.LastSeen = now
}

// emit stores the alarm and the transition, then enqueues it for every sink.
// The event's AutoFlag is the sink's eligibility (docs/design.md §8.1): only
// a raised or updated `break` alarm the check marked eligible; a cleared
// alarm never triggers anything, whatever its own flag says.
func (m *Manager) emit(typ string, a model.Alarm, now time.Time) {
	a.Transitions++
	m.store.PutAlarm(a)
	e := model.AlarmEvent{Type: typ, At: now, Alarm: a}
	e.AutoFlag = typ != "cleared" && a.AutoFlag && a.Severity == model.SeverityBreak
	m.store.PutAlarmEvent(e)
	for _, q := range m.queues {
		q.enqueue(e)
	}
}

// queue is one sink's bounded delivery queue with its pending set.
type queue struct {
	sink    Sink
	id      string
	ch      chan model.AlarmEvent
	log     *slog.Logger
	now     func() time.Time
	dropped atomic.Int64
	failed  atomic.Int64

	mu sync.Mutex
	// pending is, per alarm key, the latest transition the sink has not
	// delivered; pendingSince when it became non-empty.
	pending      map[string]model.AlarmEvent
	pendingSince time.Time
	// failingSince is the start of the current streak of refused
	// deliveries (zero: the last delivery succeeded).
	failingSince time.Time
}

// enqueue never blocks: a full queue drops the oldest event, counts it and
// keeps it pending.
func (q *queue) enqueue(e model.AlarmEvent) {
	for {
		select {
		case q.ch <- e:
			return
		default:
		}
		select {
		case old := <-q.ch:
			q.dropped.Add(1)
			q.markPending(old)
			q.log.Warn("alert queue full, dropping oldest", "sink", q.id, "dropped", old.Alarm.Key)
		default:
		}
	}
}

// later reports whether a's transition comes after b's: by generation,
// then by transition count.
func later(a, b model.Alarm) bool {
	if a.Generation != b.Generation {
		return a.Generation > b.Generation
	}
	return a.Transitions > b.Transitions
}

// markPending keeps e as the key's undelivered transition unless a later
// one is pending already.
func (q *queue) markPending(e model.AlarmEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if cur, ok := q.pending[e.Alarm.Key]; ok && later(cur.Alarm, e.Alarm) {
		return
	}
	if len(q.pending) == 0 {
		q.pendingSince = q.now()
	}
	q.pending[e.Alarm.Key] = e
}

// delivered removes the key's pending transition unless a later one is
// pending.
func (q *queue) delivered(e model.AlarmEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if cur, ok := q.pending[e.Alarm.Key]; ok && !later(cur.Alarm, e.Alarm) {
		delete(q.pending, e.Alarm.Key)
	}
	if len(q.pending) == 0 {
		q.pendingSince = time.Time{}
	}
}

func (q *queue) pendingKeys() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.pending))
	for k := range q.pending {
		out = append(out, k)
	}
	return out
}

func (q *queue) status() (pending int, since time.Time, dropped, failed int64, failingSince time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending), q.pendingSince, q.dropped.Load(), q.failed.Load(), q.failingSince
}

// resend enqueues each pending transition, one event per key, while the
// queue has room. The pending entry is the key's latest transition, kept
// whole (type, time, eligibility and alarm together), so a sink never sees
// an older event's envelope around a newer alarm.
func (q *queue) resend() {
	q.mu.Lock()
	events := make([]model.AlarmEvent, 0, len(q.pending))
	for _, e := range q.pending {
		events = append(events, e)
	}
	q.mu.Unlock()
	for _, e := range events {
		if len(q.ch) >= cap(q.ch) {
			return
		}
		select {
		case q.ch <- e:
		default:
			return
		}
	}
}

func (q *queue) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-q.ch:
			q.deliver(ctx, e)
		}
	}
}

func (q *queue) deliver(ctx context.Context, e model.AlarmEvent) {
	if err := q.sink.Send(ctx, e); err != nil {
		q.failed.Add(1)
		q.markPending(e)
		q.mu.Lock()
		if q.failingSince.IsZero() {
			q.failingSince = q.now()
		}
		q.mu.Unlock()
		q.log.Error("alert sink failed", "sink", q.id, "alarm", e.Alarm.Key, "err", err)
		return
	}
	q.mu.Lock()
	q.failingSince = time.Time{}
	q.mu.Unlock()
	q.delivered(e)
}

// drain delivers what is queued now (used on Close).
func (q *queue) drain(ctx context.Context) {
	for {
		select {
		case e := <-q.ch:
			q.deliver(ctx, e)
		default:
			return
		}
	}
}
