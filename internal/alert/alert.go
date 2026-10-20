// Package alert turns check findings into stateful, deduplicated alarms and
// sends their transitions to sinks (docs/design.md §8).
package alert

import (
	"context"
	"log/slog"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// Sink receives alarm transitions.
type Sink interface {
	Name() string
	Send(ctx context.Context, e model.AlarmEvent) error
}

// Manager keeps alarm state and fans transitions out.
type Manager struct {
	store store.Store
	sinks []Sink
	log   *slog.Logger
	now   func() time.Time
}

// NewManager returns a manager over the store with the given sinks.
func NewManager(st store.Store, log *slog.Logger, sinks ...Sink) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{store: st, sinks: sinks, log: log, now: time.Now}
}

// Apply reconciles a check's result with the alarms it owns: a finding with
// an AlarmKey raises or updates its alarm; an alarm of this check whose key
// the result no longer reports is cleared. A skipped or errored result
// changes nothing (the alarm keeps its last state until data returns).
func (m *Manager) Apply(ctx context.Context, r model.CheckResult) {
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
			m.store.PutAlarm(a)
			m.emit(ctx, "raised", a, now)
			continue
		}
		changed := a.Severity != f.Severity || a.Title != f.Title
		m.update(&a, f, now)
		m.store.PutAlarm(a)
		if changed {
			m.emit(ctx, "updated", a, now)
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
		m.store.PutAlarm(a)
		m.emit(ctx, "cleared", a, now)
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

func (m *Manager) emit(ctx context.Context, typ string, a model.Alarm, now time.Time) {
	a.Transitions++
	m.store.PutAlarm(a)
	e := model.AlarmEvent{Type: typ, At: now, Alarm: a}
	m.store.PutAlarmEvent(e)
	for _, s := range m.sinks {
		if err := s.Send(ctx, e); err != nil {
			m.log.Error("alert sink failed", "sink", s.Name(), "alarm", a.Key, "err", err)
		}
	}
}
