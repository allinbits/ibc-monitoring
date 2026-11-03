package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/api"
	"github.com/allinbits/ibc-monitoring/internal/config"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethviews"
	"github.com/allinbits/ibc-monitoring/internal/store"
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

func TestArcDeploymentAndPublicNetwork(t *testing.T) {
	file := t.TempDir() + "/5042002.json"
	dep := `{"ics20Transfer":"0x1111111111111111111111111111111111111111","ibcAdmin":"0x2222222222222222222222222222222222222222","deployBlock":123456}`
	if err := os.WriteFile(file, []byte(dep), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse([]byte(`gno:
  chain_id: test-arc-1
  client_type: arc
  browser_rpc_url: https://gno.example
eth:
  chain_id: "5042002"
  chain_name: Arc testnet
  rpc_url: https://operator.example/private-secret
  rpc_url_2: https://operator-second.example/private-secret
  browser_rpc_url: https://rpc.testnet.arc.io
  explorer_url: https://testnet.arcscan.app
  history_from_deployment: true
`), os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Eth.DeploymentFile = file
	addrs, err := loadAddresses(cfg)
	if err != nil || addrs.Admin != common.HexToAddress("0x2222222222222222222222222222222222222222") || cfg.Eth.HistoryFromBlock != 123456 {
		t.Fatalf("deployment: %+v %v", addrs, err)
	}
	cfg.Eth.HistoryFromBlock = 999
	cfg.Eth.Addresses.Admin = "0x3333333333333333333333333333333333333333"
	addrs, err = loadAddresses(cfg)
	if err != nil || cfg.Eth.HistoryFromBlock != 999 || addrs.Admin != common.HexToAddress(cfg.Eth.Addresses.Admin) {
		t.Fatalf("override: %+v %v", addrs, err)
	}
	evm, err := ethrpc.Dial(context.Background(), "http://127.0.0.1:1", "unused", ethrpc.Finality{Mode: "tag"})
	if err != nil {
		t.Fatal(err)
	}
	defer evm.Close()
	views, err := ethviews.New(evm, addrs)
	if err != nil {
		t.Fatal(err)
	}
	rt := &runtime{cfg: cfg, eth: views}
	srv := api.Server{Store: store.NewMemory(10), Info: api.Info{Public: true, Network: rt.network()}}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/info", nil))
	if strings.Contains(rec.Body.String(), "private-secret") {
		t.Fatal("private RPC leaked")
	}
	var info api.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Network.Gno.RouteClientID != "arc-1" || info.Network.EVM.AdminAddress != addrs.Admin.Hex() || info.Network.EVM.ChainName != "Arc testnet" {
		t.Fatalf("network: %+v", info.Network)
	}
}
