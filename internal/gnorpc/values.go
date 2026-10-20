package gnorpc

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// vm/qeval renders each result as `(<value> <type>)` on its own line:
//
//	("ethereum-1" string)
//	(42 uint64)
//	(true bool)
//	(slice[("a" string),("b" string)] []string)
//
// A string is Go-quoted. A persisted slice renders as `slice[ref(...)]`, which
// carries no values; the views avoid such expressions.

// Value is one parsed qeval result.
type Value struct {
	Type string
	Raw  string // the text between the parentheses, before the type
}

var valueLine = regexp.MustCompile(`(?s)^\((.*) ([^\s()]+)\)$`)

// Values splits a qeval answer into its results.
func Values(s string) ([]Value, error) {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil, nil
	}
	var out []Value
	for _, line := range splitResults(s) {
		m := valueLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("qeval: cannot parse result %q", line)
		}
		out = append(out, Value{Type: m[2], Raw: m[1]})
	}
	return out, nil
}

// splitResults splits on newlines that end a result, i.e. that follow a ')'
// balanced with the result's opening '('. Strings may contain newlines.
func splitResults(s string) []string {
	var out []string
	depth, inStr, esc := 0, false, false
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case esc:
			esc = false
		case inStr && ch == '\\':
			esc = true
		case ch == '"':
			inStr = !inStr
		case inStr:
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case ch == '\n' && depth == 0:
			if line := strings.TrimSpace(s[start:i]); line != "" {
				out = append(out, line)
			}
			start = i + 1
		}
	}
	if line := strings.TrimSpace(s[start:]); line != "" {
		out = append(out, line)
	}
	return out
}

// String returns the first result as a string.
func (v Value) String() (string, error) {
	if v.Type != "string" {
		return "", fmt.Errorf("qeval: expected string, got %s", v.Type)
	}
	return strconv.Unquote(v.Raw)
}

// Int returns the first result as an int64 (any integer type).
func (v Value) Int() (int64, error) {
	switch v.Type {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return strconv.ParseInt(v.Raw, 10, 64)
	}
	return 0, fmt.Errorf("qeval: expected integer, got %s", v.Type)
}

// Uint returns the first result as a uint64 (any integer type).
func (v Value) Uint() (uint64, error) {
	switch v.Type {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return strconv.ParseUint(v.Raw, 10, 64)
	}
	return 0, fmt.Errorf("qeval: expected integer, got %s", v.Type)
}

// Bool returns the result as a bool.
func (v Value) Bool() (bool, error) {
	if v.Type != "bool" {
		return false, fmt.Errorf("qeval: expected bool, got %s", v.Type)
	}
	return strconv.ParseBool(v.Raw)
}

var sliceItem = regexp.MustCompile(`\("((?:[^"\\]|\\.)*)" string\)`)

// Strings returns a []string result. A persisted slice (rendered as refs)
// is an error, since its elements are not in the answer.
func (v Value) Strings() ([]string, error) {
	if v.Type != "[]string" {
		return nil, fmt.Errorf("qeval: expected []string, got %s", v.Type)
	}
	if strings.Contains(v.Raw, "ref(") {
		return nil, errors.New("qeval: slice rendered as references, elements unavailable")
	}
	if v.Raw == "nil" || v.Raw == "slice[]" {
		return nil, nil
	}
	var out []string
	for _, m := range sliceItem.FindAllStringSubmatch(v.Raw, -1) {
		s, err := strconv.Unquote(`"` + m[1] + `"`)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// One parses a qeval answer that holds exactly one result.
func One(s string) (Value, error) {
	vs, err := Values(s)
	if err != nil {
		return Value{}, err
	}
	if len(vs) != 1 {
		return Value{}, fmt.Errorf("qeval: expected 1 result, got %d", len(vs))
	}
	return vs[0], nil
}
