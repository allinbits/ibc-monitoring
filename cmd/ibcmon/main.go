// Command ibcmon is the monitor (T4), the watch worker (T5) and the console's
// API in one binary. See docs/design.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/alert"
	"github.com/allinbits/ibc-monitoring/internal/api"
	"github.com/allinbits/ibc-monitoring/internal/config"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethviews"
	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/gnoviews"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/monitor"
	"github.com/allinbits/ibc-monitoring/internal/redact"
	"github.com/allinbits/ibc-monitoring/internal/store"
	"github.com/allinbits/ibc-monitoring/internal/worker"
)

// version is set by the build (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:], true, true)
	case "monitor":
		err = runServe(os.Args[2:], true, false)
	case "check":
		err = runCheck(os.Args[2:])
	case "version":
		fmt.Println("ibcmon", version)
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		slog.Error("ibcmon", "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: ibcmon <command> [flags]

  serve    run the monitor, the worker and the API with the console
  monitor  run the monitor and the API only (no worker)
  check    run one check once and print its result as JSON
  version  print the version

flags (serve, monitor, check): -config ibcmon.yaml`)
}

type runtime struct {
	cfg   *config.Config
	store *store.Memory
	log   *slog.Logger
	gno   *gnoviews.Views
	gno2  *gnoviews.Views
	eth   *ethviews.Views
	eth2  *ethrpc.Client
	ethc  *ethrpc.Client
}

func setup(ctx context.Context, configPath string) (*runtime, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	// Every configured endpoint and secret is redacted wherever it would
	// appear (docs/design.md §10.2).
	redact.Register(cfg.Gno.RPCURL, cfg.Gno.RPCURL2, cfg.Eth.RPCURL, cfg.Eth.RPCURL2, cfg.Eth.BeaconURL)
	for _, s := range cfg.Alerts.Sinks {
		redact.Register(s.URL, s.Secret)
	}
	redact.Register(os.Getenv("IBCMON_GNO_WATCH_MNEMONIC"), os.Getenv("IBCMON_ETH_WATCH_PRIVATE_KEY"))
	rt := &runtime{cfg: cfg, store: store.NewMemory(cfg.Instance.History), log: slog.Default()}
	if cfg.Gno.RPCURL != "" {
		paths := gnoviews.DefaultPaths
		if cfg.Gno.PkgPaths.Core != "" {
			paths = gnoviews.Paths{Core: cfg.Gno.PkgPaths.Core, Transfer: cfg.Gno.PkgPaths.Transfer, IBCGov: cfg.Gno.PkgPaths.IBCGov}
		}
		rt.gno = gnoviews.New(gnorpc.New(cfg.Gno.RPCURL, gnorpc.WithName(cfg.Gno.Name)), paths)
		if cfg.Gno.RPCURL2 != "" {
			rt.gno2 = gnoviews.New(gnorpc.New(cfg.Gno.RPCURL2, gnorpc.WithName(cfg.Gno.Name2)), paths)
		}
	}
	if cfg.Eth.RPCURL != "" {
		fin, err := ethrpc.ParseFinality(cfg.Eth.Finality)
		if err != nil {
			return nil, err
		}
		rt.ethc, err = ethrpc.Dial(ctx, cfg.Eth.RPCURL, cfg.Eth.Name, fin)
		if err != nil {
			return nil, err
		}
		addrs, err := loadAddresses(cfg)
		if err != nil {
			return nil, err
		}
		rt.eth, err = ethviews.New(rt.ethc, addrs)
		if err != nil {
			return nil, err
		}
		if cfg.Eth.RPCURL2 != "" {
			rt.eth2, err = ethrpc.Dial(ctx, cfg.Eth.RPCURL2, cfg.Eth.Name2, fin)
			if err != nil {
				return nil, err
			}
		}
	}
	if rt.gno == nil && rt.eth == nil {
		return nil, errors.New("config: neither gno.rpc_url nor eth.rpc_url is set")
	}
	return rt, nil
}

// loadAddresses takes the configured addresses, filling any empty one from
// gno-eth-ibc's deployment file when given.
func loadAddresses(cfg *config.Config) (ethviews.Addresses, error) {
	a := cfg.Eth.Addresses
	if cfg.Eth.DeploymentFile != "" {
		b, err := os.ReadFile(cfg.Eth.DeploymentFile)
		if err != nil {
			return ethviews.Addresses{}, err
		}
		var dep struct {
			Router       string `json:"ics26Router"`
			Transfer     string `json:"ics20Transfer"`
			Admin        string `json:"ibcAdmin"`
			Exec         string `json:"ibcAdminExec"`
			LightClient  string `json:"gnoLightClient"`
			UgnotVoucher string `json:"ugnotVoucher"`
		}
		if err := json.Unmarshal(b, &dep); err != nil {
			return ethviews.Addresses{}, fmt.Errorf("deployment file: %w", err)
		}
		fill := func(dst *string, src string) {
			if *dst == "" {
				*dst = src
			}
		}
		fill(&a.Router, dep.Router)
		fill(&a.Transfer, dep.Transfer)
		fill(&a.Admin, dep.Admin)
		fill(&a.Exec, dep.Exec)
		fill(&a.LightClient, dep.LightClient)
		fill(&a.UgnotVoucher, dep.UgnotVoucher)
	}
	parse := func(name, s string) (common.Address, error) {
		if s == "" {
			return common.Address{}, nil
		}
		if !common.IsHexAddress(s) {
			return common.Address{}, fmt.Errorf("eth.addresses.%s: %q is not an address", name, s)
		}
		return common.HexToAddress(s), nil
	}
	var out ethviews.Addresses
	var err error
	if out.Router, err = parse("router", a.Router); err != nil {
		return out, err
	}
	if out.Transfer, err = parse("transfer", a.Transfer); err != nil {
		return out, err
	}
	if out.Admin, err = parse("admin", a.Admin); err != nil {
		return out, err
	}
	if out.Exec, err = parse("exec", a.Exec); err != nil {
		return out, err
	}
	if out.LightClient, err = parse("light_client", a.LightClient); err != nil {
		return out, err
	}
	if out.UgnotVoucher, err = parse("ugnot_voucher", a.UgnotVoucher); err != nil {
		return out, err
	}
	if out.Transfer == (common.Address{}) || out.Admin == (common.Address{}) {
		return out, errors.New("eth.addresses: transfer and admin are required")
	}
	return out, nil
}

func (rt *runtime) collector() *monitor.Collector {
	c := &monitor.Collector{Gno: rt.gno, Gno2: rt.gno2, Eth: rt.eth, Eth2: rt.eth2, RequireCrossCheck: rt.cfg.Instance.CrossCheckRequired(), Store: rt.store, Log: rt.log}
	if rt.cfg.Eth.LightClient {
		c.EthVerification = model.VerificationLightClient
	}
	return c
}

func (rt *runtime) checks() []monitor.Check {
	ce := monitor.DefaultClientExpiryConfig()
	mc := rt.cfg.Monitor.ClientExpiry
	ce.WarnBefore, ce.CriticalBefore = mc.WarnBefore, mc.CriticalBefore
	for k, d := range mc.StaleAfter {
		ce.StaleAfter[model.ClientKind(k)] = d
	}
	return []monitor.Check{monitor.ClientExpiry{Config: ce}}
}

func (rt *runtime) sinks() []alert.Sink {
	var sinks []alert.Sink
	for _, s := range rt.cfg.Alerts.Sinks {
		switch s.Type {
		case "log":
			sinks = append(sinks, alert.LogSink{Log: rt.log})
		case "webhook":
			sinks = append(sinks, alert.WebhookSink{URL: s.URL, Secret: s.Secret})
		}
	}
	return sinks
}

func runServe(args []string, withMonitor, withWorker bool) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "ibcmon.yaml", "configuration file")
	noMonitor := fs.Bool("no-monitor", false, "serve the API without running the monitor")
	noWorker := fs.Bool("no-worker", false, "serve without the worker")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rt, err := setup(ctx, *configPath)
	if err != nil {
		return err
	}
	withMonitor = withMonitor && !*noMonitor
	withWorker = withWorker && !*noWorker && rt.cfg.Worker.Enabled

	info := api.Info{Version: version, Public: rt.cfg.Instance.Public, StartedAt: time.Now().UTC(), GnoNode: rt.cfg.Gno.Name, EthNode: rt.cfg.Eth.Name, EthFinality: rt.cfg.Eth.Finality, AutoFlagRequested: "off", AutoFlagEffective: "off", Worker: withWorker}
	if info.AutoFlagRequested, info.AutoFlagEffective, err = autoFlagStatus(rt.cfg.AutoFlag, os.Getenv); err != nil {
		return err
	}
	if info.AutoFlagRequested != info.AutoFlagEffective {
		rt.log.Warn("autoflag requested but the auto-flag sink is not built yet: effective status unavailable, alarms only (docs/design.md §8.4, phase 3)", "requested", info.AutoFlagRequested)
	}
	srv := &api.Server{Store: rt.store, Info: info}
	if withWorker && rt.ethc != nil {
		w := &worker.Worker{Eth: rt.ethc, Log: rt.log}
		srv.Jobs = w.Jobs
		// TODO(phase 5): schedule codehash/manifest jobs from the governance
		// watch's pending kind 5/6/7/9 decisions and recovery candidates.
	}
	if withMonitor {
		alerts := alert.NewManager(rt.store, rt.log, rt.sinks()...)
		defer alerts.Close(5 * time.Second)
		eng := &monitor.Engine{Collector: rt.collector(), Checks: rt.checks(), Alerts: alerts, Store: rt.store, Interval: rt.cfg.Monitor.PollInterval, DeferredAlarmFailures: rt.cfg.Monitor.DeferredAlarmFailures, Log: rt.log}
		go eng.Run(ctx)
	}
	httpSrv := &http.Server{Addr: rt.cfg.Instance.Listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
	}()
	rt.log.Info("ibcmon listening", "addr", rt.cfg.Instance.Listen, "public", rt.cfg.Instance.Public, "monitor", withMonitor, "worker", withWorker)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	configPath := fs.String("config", "ibcmon.yaml", "configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := fs.Arg(0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rt, err := setup(ctx, *configPath)
	if err != nil {
		return err
	}
	var checks []monitor.Check
	for _, c := range rt.checks() {
		if name == "" || c.Name() == name {
			checks = append(checks, c)
		}
	}
	if len(checks) == 0 {
		return fmt.Errorf("unknown check %q", name)
	}
	eng := &monitor.Engine{Collector: rt.collector(), Checks: checks, Store: rt.store, Log: rt.log}
	results := eng.RunOnce(ctx)
	pins, _ := rt.store.Snapshot(store.KindPins, "latest")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"pins": pins.Value, "results": results})
}

// autoFlagStatus derives the requested and the effective auto-flag status
// (design §8.4). The sink is not built (phase 3), so whatever is requested
// the effective status is "unavailable", never "on" or "dry-run"; and the
// watch-key requirement already applies, so a configuration that could
// never run live is refused now rather than silently reported.
func autoFlagStatus(cfg config.AutoFlag, getenv func(string) string) (requested, effective string, err error) {
	if !cfg.Enabled {
		return "off", "off", nil
	}
	requested = "on"
	if cfg.DryRun {
		requested = "dry-run"
	}
	if getenv("IBCMON_GNO_WATCH_MNEMONIC") == "" || getenv("IBCMON_ETH_WATCH_PRIVATE_KEY") == "" {
		return "", "", errors.New("autoflag.enabled needs IBCMON_GNO_WATCH_MNEMONIC and IBCMON_ETH_WATCH_PRIVATE_KEY in the environment (docs/design.md §8.4)")
	}
	return requested, "unavailable", nil
}
