// Package store keeps the latest snapshots of every view, the checks'
// results with their history, and the alarms. This is the in-memory store of
// phase 1; the SQLite store of docs/design.md §11 implements the same
// interface (TODO phase 2: internal/store/sqlite).
package store

import (
	"sort"
	"sync"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
)

// Snapshot kinds.
const (
	KindGnoClients    = "gno_clients"
	KindGnoRoutes     = "gno_routes"
	KindGnoGovernance = "gno_governance"
	KindEthClients    = "eth_clients"
	KindEthRoutes     = "eth_routes"
	KindEthGovernance = "eth_governance"
	KindPins          = "pins"
)

// Snapshot is one stored view with its provenance.
type Snapshot struct {
	Kind       string           `json:"kind"`
	Key        string           `json:"key"`
	Value      any              `json:"value"`
	Provenance model.Provenance `json:"provenance"`
	StoredAt   time.Time        `json:"stored_at"`
}

// Store is what the collector writes and the checks and API read.
type Store interface {
	PutSnapshot(s Snapshot)
	Snapshot(kind, key string) (Snapshot, bool)
	Snapshots(kind string) []Snapshot

	PutResult(r model.CheckResult)
	LatestResult(check string) (model.CheckResult, bool)
	LatestResults() []model.CheckResult
	ResultHistory(check string, limit int) []model.CheckResult

	PutAlarm(a model.Alarm)
	Alarm(key string) (model.Alarm, bool)
	Alarms(includeCleared bool) []model.Alarm
	PutAlarmEvent(e model.AlarmEvent)
	AlarmEvents(limit int) []model.AlarmEvent
}

// Memory is the in-memory store.
type Memory struct {
	mu        sync.RWMutex
	snapshots map[string]map[string]Snapshot
	results   map[string][]model.CheckResult
	alarms    map[string]model.Alarm
	events    []model.AlarmEvent
	history   int
}

// NewMemory returns a store keeping `history` results per check.
func NewMemory(history int) *Memory {
	if history <= 0 {
		history = 500
	}
	return &Memory{snapshots: map[string]map[string]Snapshot{}, results: map[string][]model.CheckResult{}, alarms: map[string]model.Alarm{}, history: history}
}

func (m *Memory) PutSnapshot(s Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshots[s.Kind] == nil {
		m.snapshots[s.Kind] = map[string]Snapshot{}
	}
	if s.StoredAt.IsZero() {
		s.StoredAt = time.Now()
	}
	m.snapshots[s.Kind][s.Key] = s
}

func (m *Memory) Snapshot(kind, key string) (Snapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.snapshots[kind][key]
	return s, ok
}

func (m *Memory) Snapshots(kind string) []Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Snapshot, 0, len(m.snapshots[kind]))
	for _, s := range m.snapshots[kind] {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (m *Memory) PutResult(r model.CheckResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.results[r.Check]
	h = append(h, r)
	if len(h) > m.history {
		h = h[len(h)-m.history:]
	}
	m.results[r.Check] = h
}

func (m *Memory) LatestResult(check string) (model.CheckResult, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h := m.results[check]
	if len(h) == 0 {
		return model.CheckResult{}, false
	}
	return h[len(h)-1], true
}

func (m *Memory) LatestResults() []model.CheckResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.CheckResult, 0, len(m.results))
	for _, h := range m.results {
		if len(h) > 0 {
			out = append(out, h[len(h)-1])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Check < out[j].Check })
	return out
}

func (m *Memory) ResultHistory(check string, limit int) []model.CheckResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h := m.results[check]
	if limit > 0 && len(h) > limit {
		h = h[len(h)-limit:]
	}
	out := make([]model.CheckResult, len(h))
	copy(out, h)
	return out
}

func (m *Memory) PutAlarm(a model.Alarm) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alarms[a.Key] = a
}

func (m *Memory) Alarm(key string) (model.Alarm, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.alarms[key]
	return a, ok
}

func (m *Memory) Alarms(includeCleared bool) []model.Alarm {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Alarm, 0, len(m.alarms))
	for _, a := range m.alarms {
		if !includeCleared && a.State == model.AlarmCleared {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].State != out[j].State {
			return out[i].State < out[j].State
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func (m *Memory) PutAlarmEvent(e model.AlarmEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	if len(m.events) > m.history*4 {
		m.events = m.events[len(m.events)-m.history*4:]
	}
}

func (m *Memory) AlarmEvents(limit int) []model.AlarmEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e := m.events
	if limit > 0 && len(e) > limit {
		e = e[len(e)-limit:]
	}
	out := make([]model.AlarmEvent, len(e))
	copy(out, e)
	return out
}
