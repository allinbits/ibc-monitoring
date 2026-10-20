// Package redact removes secrets from text before it is stored, logged or
// served (docs/design.md §10.2): URLs are reduced to scheme and host, user
// info, query strings and token-like path segments go, and registered secret
// values are replaced wherever they occur.
package redact

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	mu      sync.RWMutex
	secrets []string
)

// Register adds secret values (RPC URLs with tokens, webhook URLs, secrets,
// keys) that must never appear in any output. Empty values are ignored.
func Register(values ...string) {
	mu.Lock()
	defer mu.Unlock()
	for _, v := range values {
		if v == "" {
			continue
		}
		secrets = append(secrets, v)
	}
	// Longest first, so a value that contains another is replaced whole.
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
}

// Reset forgets every registered secret (tests).
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	secrets = nil
}

// urlRe matches an absolute URL (scheme://...) up to whitespace or a quote.
var urlRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>]+`)

// tokenSegment is a path segment that looks like a credential: long and
// made of hex / base64url characters.
var tokenSegment = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

// String redacts s.
func String(s string) string {
	mu.RLock()
	for _, sec := range secrets {
		s = strings.ReplaceAll(s, sec, "[redacted]")
	}
	mu.RUnlock()
	return urlRe.ReplaceAllStringFunc(s, redactURL)
}

// redactURL keeps scheme, host and port and the path's non-token segments.
func redactURL(u string) string {
	trailing := ""
	for len(u) > 0 && strings.ContainsRune(".,;:)]}", rune(u[len(u)-1])) {
		trailing = string(u[len(u)-1]) + trailing
		u = u[:len(u)-1]
	}
	scheme, rest, ok := strings.Cut(u, "://")
	if !ok {
		return u + trailing
	}
	// Drop the fragment and query.
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest = rest[:i]
	}
	hostport, path, _ := strings.Cut(rest, "/")
	// Drop user info.
	if i := strings.LastIndexByte(hostport, '@'); i >= 0 {
		hostport = hostport[i+1:]
	}
	var segs []string
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			continue
		}
		if tokenSegment.MatchString(seg) {
			segs = append(segs, "[redacted]")
		} else {
			segs = append(segs, seg)
		}
	}
	out := scheme + "://" + hostport
	if len(segs) > 0 {
		out += "/" + strings.Join(segs, "/")
	}
	return out + trailing
}

// Error returns an error whose message is the redacted message of err (nil
// stays nil). The chain of wrapped errors is not preserved on purpose: a
// wrapped error could carry the unredacted text.
func Error(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(String(err.Error()))
}

// Errorf formats and redacts in one step.
func Errorf(format string, args ...any) error {
	return Error(fmt.Errorf(format, args...))
}
