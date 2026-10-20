package config

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	env := map[string]string{"IBCMON_GNO_RPC_URL": "http://gno:26657", "IBCMON_WEBHOOK_URL": "https://hooks.example/x"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	c, err := Parse([]byte(`
gno:
  rpc_url: ${IBCMON_GNO_RPC_URL}
  rpc_url_2: ${IBCMON_GNO_RPC_URL_2}
eth:
  rpc_url: http://eth:8545
  finality: depth:5
monitor:
  poll_interval: 3s
alerts:
  sinks:
    - type: log
    - type: webhook
      url: ${IBCMON_WEBHOOK_URL}
`), lookup)
	if err != nil {
		t.Fatal(err)
	}
	if c.Gno.RPCURL != "http://gno:26657" || c.Gno.RPCURL2 != "" {
		t.Fatalf("gno %+v", c.Gno)
	}
	if c.Monitor.PollInterval != 3*time.Second || c.Monitor.DeferredAlarmFailures != 6 || c.Monitor.ClientExpiry.WarnBefore != 72*time.Hour {
		t.Fatalf("monitor %+v", c.Monitor)
	}
	if len(c.Alerts.Sinks) != 2 || c.Alerts.Sinks[1].URL != "https://hooks.example/x" {
		t.Fatalf("sinks %+v", c.Alerts.Sinks)
	}
	if c.Instance.Listen != ":8080" || c.Eth.Finality != "depth:5" {
		t.Fatalf("defaults %+v", c)
	}
	// Unknown keys are refused: a typo must not silently disable a setting.
	if _, err := Parse([]byte("autoflagg:\n  enabled: true\n"), lookup); err == nil || !strings.Contains(err.Error(), "autoflagg") {
		t.Fatalf("unknown key: %v", err)
	}
	// Auto-flag on a public instance is refused.
	if _, err := Parse([]byte("instance:\n  public: true\nautoflag:\n  enabled: true\n"), lookup); err == nil {
		t.Fatal("public + autoflag must fail")
	}
	// A webhook needs a URL (an unset ${VAR} expands to "").
	if _, err := Parse([]byte("alerts:\n  sinks:\n    - type: webhook\n      url: ${NOPE}\n"), lookup); err == nil {
		t.Fatal("webhook without url must fail")
	}
	// An empty file is a valid configuration of defaults.
	if _, err := Parse([]byte(""), lookup); err != nil {
		t.Fatalf("empty: %v", err)
	}
}
