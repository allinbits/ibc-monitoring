// Package alert turns check findings into stateful, deduplicated alarms and
// sends their transitions to sinks (docs/design.md §8). Delivery is
// asynchronous and bounded per sink, so a slow sink never delays the
// evaluation loop or another sink.
package alert

import (
	"context"
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

// Manager keeps alarm state and fans transitions out.
type Manager struct {
	store  store.Store
	queues []*queue
	log    *slog.Logger
	now    func() time.Time
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewManager returns a manager over the store, with one bounded queue and
// worker per sink. Close it to drain.
func NewManager(st store.Store, log *slog.Logger, sinks ...Sink) *Manager {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{store: st, log: log, now: time.Now, ctx: ctx, cancel: cancel}
	for _, s := range sinks {
		q := &queue{sink: s, ch: make(chan model.AlarmEvent, DefaultQueue), log: log}
		m.queues = append(m.queues, q)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			q.run(ctx)
		}()
	}
	return m
}

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

// Apply reconciles a check's result with the alarms it owns: a finding with
// an AlarmKey raises or updates its alarm; an alarm of this check whose key
// the result no longer reports is cleared. A skipped or errored result
// changes nothing (the alarm keeps its last state until data returns).
func (m *Manager) Apply(_ context.Context, r model.CheckResult) {
	if r.Status == model.CheckSkipped || r.Status == model.CheckError {
		return
	}
	now := m.now()
	seen := map[string]bool{}
	for _, f := range r.Findings {
		if f.AlarmKey == "" {
			continue
		}
		seen[f.AlarmKey] = true
		a, ok := m.store.Alarm(f.AlarmKey)
		if !ok || a.State == model.AlarmCleared {
			a = model.Alarm{Key: f.AlarmKey, Check: r.Check, FirstSeen: now, State: model.AlarmRaised}
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
	for _, a := range m.store.Alarms(false) {
		if a.Check != r.Check || seen[a.Key] {
			continue
		}
		a.State = model.AlarmCleared
		t := now
		a.ClearedAt = &t
		a.LastSeen = now
		m.emit("cleared", a, now)
	}
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

// queue is one sink's bounded delivery queue.
type queue struct {
	sink    Sink
	ch      chan model.AlarmEvent
	log     *slog.Logger
	dropped atomic.Int64
}

// enqueue never blocks: a full queue drops the oldest event and counts it.
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
			q.log.Warn("alert queue full, dropping oldest", "sink", q.sink.Name(), "dropped", old.Alarm.Key)
		default:
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
		q.log.Error("alert sink failed", "sink", q.sink.Name(), "alarm", e.Alarm.Key, "err", err)
	}
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
