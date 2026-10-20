package main

import (
	"testing"

	"github.com/allinbits/ibc-monitoring/internal/config"
)

// The effective auto-flag status is never "on" or "dry-run" while the sink
// is not built, and enabling it without the watch keys is refused (review
// finding 8, design §8.4).
func TestAutoFlagStatus(t *testing.T) {
	none := func(string) string { return "" }
	keys := func(string) string { return "set" }
	if r, e, err := autoFlagStatus(config.AutoFlag{}, none); r != "off" || e != "off" || err != nil {
		t.Fatalf("off: %s %s %v", r, e, err)
	}
	if _, _, err := autoFlagStatus(config.AutoFlag{Enabled: true, DryRun: true}, none); err == nil {
		t.Fatal("enabled without watch keys accepted")
	}
	if r, e, err := autoFlagStatus(config.AutoFlag{Enabled: true, DryRun: true}, keys); r != "dry-run" || e != "unavailable" || err != nil {
		t.Fatalf("dry-run: %s %s %v", r, e, err)
	}
	if r, e, err := autoFlagStatus(config.AutoFlag{Enabled: true}, keys); r != "on" || e != "unavailable" || err != nil {
		t.Fatalf("on: %s %s %v", r, e, err)
	}
}
