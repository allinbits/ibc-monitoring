package model

import "time"

// Severity of a finding or alarm.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
	// SeverityBreak is an integrity break (ADR §7.11): the only severity the
	// auto-flag sink may act on.
	SeverityBreak Severity = "break"
)

// CheckStatus is the outcome of one evaluation.
type CheckStatus string

const (
	CheckOK CheckStatus = "ok"
	// CheckSkipped: data unavailable; retried; alarms only after N failures
	// (docs/design.md §8.3).
	CheckSkipped CheckStatus = "skipped"
	CheckAlarm   CheckStatus = "alarm"
	CheckError   CheckStatus = "error"
)

// Finding is one thing a check says about one subject (a client, a route and
// denom, a decision), with the provenance of what it read.
type Finding struct {
	Subject    string            `json:"subject"`
	Severity   Severity          `json:"severity"`
	Title      string            `json:"title"`
	Detail     string            `json:"detail,omitempty"`
	Values     map[string]string `json:"values,omitempty"`
	Provenance []Provenance      `json:"provenance"`
	// AlarmKey is set when the finding raises (or keeps) an alarm.
	AlarmKey string `json:"alarm_key,omitempty"`
	// AutoFlag marks a finding the auto-flag sink may act on (§7.8).
	AutoFlag bool `json:"autoflag,omitempty"`
}

// CheckResult is one evaluation of a check.
type CheckResult struct {
	Check      string      `json:"check"`
	Status     CheckStatus `json:"status"`
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt time.Time   `json:"finished_at"`
	// Reason explains a skip or an error.
	Reason   string    `json:"reason,omitempty"`
	Findings []Finding `json:"findings"`
	// Provenance of every input the check read.
	Provenance []Provenance `json:"provenance"`
	// ConsecutiveSkips counts skips since the last successful evaluation.
	ConsecutiveSkips int `json:"consecutive_skips,omitempty"`
}

// AlarmState of an alarm.
type AlarmState string

const (
	AlarmRaised       AlarmState = "raised"
	AlarmAcknowledged AlarmState = "acknowledged"
	AlarmCleared      AlarmState = "cleared"
)

// Alarm is the deduplicated, stateful form of a finding (docs/design.md §8.1).
type Alarm struct {
	Key       string     `json:"key"`
	Check     string     `json:"check"`
	Severity  Severity   `json:"severity"`
	State     AlarmState `json:"state"`
	FirstSeen time.Time  `json:"first_seen"`
	LastSeen  time.Time  `json:"last_seen"`
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
	Title     string     `json:"title"`
	Detail    string     `json:"detail,omitempty"`
	Evidence  Finding    `json:"evidence"`
	AutoFlag  bool       `json:"autoflag"`
	// Transitions counts raise/update/clear events sent to the sinks.
	Transitions int `json:"transitions"`
}

// AlarmEvent is one transition sent to the sinks.
type AlarmEvent struct {
	Type  string    `json:"type"` // raised | updated | cleared
	At    time.Time `json:"at"`
	Alarm Alarm     `json:"alarm"`
}
