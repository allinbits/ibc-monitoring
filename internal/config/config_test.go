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
instance:
  require_cross_check: false
gno:
  rpc_url: ${IBCMON_GNO_RPC_URL}
  rpc_url_2: ${IBCMON_GNO_RPC_URL_2}
eth:
  rpc_url: http://eth:8545
  rpc_url_2: http://eth2:8545
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
	// A chain without a second provider is refused unless the cross-check
	// requirement is turned off explicitly (design §3.3).
	if _, err := Parse([]byte("gno:\n  rpc_url: http://a\n"), lookup); err == nil || !strings.Contains(err.Error(), "rpc_url_2") {
		t.Fatalf("single provider accepted: %v", err)
	}
	if c, err := Parse([]byte("instance:\n  require_cross_check: false\ngno:\n  rpc_url: http://a\n"), lookup); err != nil || c.Instance.CrossCheckRequired() {
		t.Fatalf("explicit opt-out: %v", err)
	}
	// instance.cors_origins takes bare origins only, never a wildcard.
	if c, err := Parse([]byte("instance:\n  cors_origins: [\"https://console.example\", \"http://127.0.0.1:5173\"]\n"), lookup); err != nil || len(c.Instance.CORSOrigins) != 2 {
		t.Fatalf("valid origins: %v", err)
	}
	// Values are stored as a browser sends them in the Origin header.
	c, err = Parse([]byte("instance:\n  cors_origins: [\"HTTPS://Console.Example:443\", \"http://a.example:80\", \"https://a.example:08443\", \"http://[0:0:0:0:0:0:0:1]:5173\", \"http://127.0.0.1:5173\", \"https://xn--bcher-kva.example\"]\n"), lookup)
	if err != nil || strings.Join(c.Instance.CORSOrigins, " ") != "https://console.example http://a.example https://a.example:8443 http://[::1]:5173 http://127.0.0.1:5173 https://xn--bcher-kva.example" {
		t.Fatalf("canonical origins: %v %v", c.Instance.CORSOrigins, err)
	}
	for _, bad := range []string{"*", "https://*.example", "https://console.example/", "https://console.example/app", "console.example", "ftp://console.example", "https://u@console.example", "https://console.example?", "https://", "https://bücher.example", "http://[::ffff:127.0.0.1]", "http://127.1", "http://010.0.0.1", "http://[fe80::1%25en0]", "https://console.example:0", "https://console.example:70000", "https://console.example:"} {
		if _, err := Parse([]byte("instance:\n  cors_origins: [\""+bad+"\"]\n"), lookup); err == nil || !strings.Contains(err.Error(), "cors_origins") {
			t.Fatalf("origin %q accepted: %v", bad, err)
		}
	}
}
