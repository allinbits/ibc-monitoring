package config

import (
	"os"
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
}

func TestArcExampleAndLegacyDefaults(t *testing.T) {
	b, err := os.ReadFile("../../ibcmon.arc.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(b, func(string) (string, bool) { return "https://gno-second.example", true })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gno.ClientType != "arc" || cfg.Gno.RouteClientID != "arc-1" || cfg.Gno.ChainID != "test-arc-1" || cfg.Eth.ChainID != "5042002" || cfg.Eth.ChainName != "Arc testnet" || cfg.Eth.BeaconURL != "" || cfg.Eth.Finality != "tag" || cfg.Eth.LogChunk != 1000 || !cfg.Eth.HistoryFromDeployment {
		t.Fatalf("arc: %+v", cfg)
	}
	if cfg.Monitor.Freshness.EthereumMaxHeadAge != 30*time.Second || cfg.Monitor.Freshness.EthMaxProviderLag != 20 || cfg.Eth.NativeCurrency.Symbol != "USDC" || cfg.Eth.NativeCurrency.Decimals != 18 {
		t.Fatalf("arc settings: %+v", cfg)
	}
	old, err := Parse(nil, os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	if old.Gno.ClientType != "ethereum" || old.Gno.RouteClientID != "ethereum-1" || old.Eth.ChainName != "Ethereum" || old.Eth.ChainID != "11155111" || old.Eth.LogChunk != 2000 || old.Eth.HistoryFromDeployment || old.Monitor.Freshness.EthereumMaxHeadAge != 45*time.Minute || old.Monitor.Freshness.EthMaxProviderLag != 64 {
		t.Fatalf("legacy defaults: %+v", old)
	}
	for _, invalid := range []string{`eth: {chain_id: "0"}`, `eth: {chain_id: "not-a-chain"}`, `gno: {client_type: typo}`, `eth: {browser_rpc_url: "https://user:secret@example.com"}`} {
		if _, err := Parse([]byte(invalid), os.LookupEnv); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
}
