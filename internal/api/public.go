package api

import (
	"encoding/json"
	"strings"
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
	"detail":               "",
	"values":               nil,
	"consecutive_failures": nil,
}

// publicFilter rewrites v for a public instance.
func publicFilter(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		return v
	}
	return filterValue(generic)
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
