package api

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// Public mode filters every response centrally (docs/design.md §10.2): the
// operator-only fields are removed before anything is served. It works on
// the encoded JSON so that no handler can forget a field: a key listed here
// is replaced wherever it appears, at any depth.
//
// The values are already redacted of secrets at their source (§10.2); this
// filter removes what a public visitor has no business seeing at all: node
// labels, collection error texts, check reasons and worker logs.
var publicReplacements = map[string]any{
	"node":                 "node",
	"gno_node":             "primary",
	"eth_node":             "primary",
	"gno_error":            "unavailable",
	"eth_error":            "unavailable",
	"error":                "unavailable",
	"gno_cross_check":      "",
	"eth_cross_check":      "",
	"reason":               "",
	"log":                  "",
	"verdict":              "",
	"detail":               "",
	"values":               nil,
	"consecutive_failures": nil,
}

// publicFilter rewrites v for a public instance. Numbers are decoded as
// tokens (UseNumber), so no identifier passes through a float64.
func publicFilter(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return v
	}
	return filterValue(generic)
}

// PublicJob is the explicit schema a public instance serves for a worker
// job: identity and verdict class only, never the texts (design §10.2).
type PublicJob struct {
	ID         int       `json:"id"`
	Kind       string    `json:"kind"`
	Subject    string    `json:"subject"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Passed     bool      `json:"passed"`
}

// publicJobs projects the worker's jobs onto PublicJob, whatever their type.
func publicJobs(v any) []PublicJob {
	b, err := json.Marshal(v)
	if err != nil {
		return []PublicJob{}
	}
	var raw []struct {
		ID         int       `json:"id"`
		Kind       string    `json:"kind"`
		Subject    string    `json:"subject"`
		Status     string    `json:"status"`
		StartedAt  time.Time `json:"started_at"`
		FinishedAt time.Time `json:"finished_at"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return []PublicJob{}
	}
	out := make([]PublicJob, 0, len(raw))
	for _, j := range raw {
		out = append(out, PublicJob{ID: j.ID, Kind: j.Kind, Subject: j.Subject, Status: j.Status, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt, Passed: j.Status == "passed"})
	}
	return out
}

func filterValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if repl, ok := publicReplacements[k]; ok {
				switch {
				case repl == nil:
					delete(t, k)
				case isEmptyish(val):
					// keep empty values as they are
				default:
					t[k] = repl
				}
				continue
			}
			t[k] = filterValue(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = filterValue(t[i])
		}
		return t
	}
	return v
}

func isEmptyish(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	}
	return false
}
