// Package config loads ibcmon.yaml with ${VAR} expansion from the
// environment. Secrets never live in the file (docs/design.md §10.1).
package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the whole configuration.
type Config struct {
	Instance Instance `yaml:"instance"`
	Gno      Gno      `yaml:"gno"`
	Eth      Eth      `yaml:"eth"`
	Monitor  Monitor  `yaml:"monitor"`
	Alerts   Alerts   `yaml:"alerts"`
	AutoFlag AutoFlag `yaml:"autoflag"`
	Worker   Worker   `yaml:"worker"`
}

type Instance struct {
	// Public enables the non-authoritative, read-only public mode (§3.4).
	Public  bool   `yaml:"public"`
	Listen  string `yaml:"listen"`
	DataDir string `yaml:"data_dir"`
	// History is how many results per check the store keeps.
	History int `yaml:"history"`
	// RequireCrossCheck (default true) refuses a chain without a second
	// provider: header agreement fails closed (design §3.3). Only the local
	// e2e target turns it off.
	RequireCrossCheck *bool `yaml:"require_cross_check"`
}

// CrossCheckRequired returns instance.require_cross_check (default true).
func (i Instance) CrossCheckRequired() bool {
	return i.RequireCrossCheck == nil || *i.RequireCrossCheck
}

// Browser endpoints are explicitly public and are never copied from private RPC URLs.
type Gno struct {
	ChainID       string `yaml:"chain_id" json:"chain_id"`
	BrowserRPCURL string `yaml:"browser_rpc_url" json:"rpc_url"`
	WebURL        string `yaml:"web_url" json:"web_url"`
	RouteClientID string `yaml:"route_client_id" json:"route_client_id"`
	ClientType    string `yaml:"client_type" json:"client_type"`
	RPCURL        string `yaml:"rpc_url"`
	RPCURL2       string `yaml:"rpc_url_2"`
	Name          string `yaml:"name"`
	Name2         string `yaml:"name_2"`
	PkgPaths      struct {
		Core     string `yaml:"core"`
		Transfer string `yaml:"transfer"`
		IBCGov   string `yaml:"ibcgov"`
	} `yaml:"pkgpaths"`
}

type NativeCurrency struct {
	Name     string `yaml:"name" json:"name"`
	Symbol   string `yaml:"symbol" json:"symbol"`
	Decimals uint8  `yaml:"decimals" json:"decimals"`
}

type Eth struct {
	ChainID        string         `yaml:"chain_id"`
	ChainName      string         `yaml:"chain_name"`
	NetworkName    string         `yaml:"network_name"`
	ExplorerURL    string         `yaml:"explorer_url"`
	BrowserRPCURL  string         `yaml:"browser_rpc_url"`
	NativeCurrency NativeCurrency `yaml:"native_currency"`
	LogChunk       uint64         `yaml:"log_chunk"`
	// MaxRPS spaces each EVM provider's requests at most this many a second
	// and retries its HTTP 429 answers with a backoff (0: unthrottled).
	MaxRPS    float64 `yaml:"max_rps"`
	RPCURL    string  `yaml:"rpc_url"`
	RPCURL2   string  `yaml:"rpc_url_2"`
	Name      string  `yaml:"name"`
	Name2     string  `yaml:"name_2"`
	BeaconURL string  `yaml:"beacon_url"`
	// Finality: "tag" (finalized), "depth:N" or "latest".
	Finality string `yaml:"finality"`
	// LightClient is true when rpc_url is a Helios (or equivalent) endpoint.
	LightClient bool `yaml:"light_client"`
	Addresses   struct {
		Router       string `yaml:"router"`
		Transfer     string `yaml:"transfer"`
		Admin        string `yaml:"admin"`
		Exec         string `yaml:"exec"`
		LightClient  string `yaml:"light_client"`
		UgnotVoucher string `yaml:"ugnot_voucher"`
	} `yaml:"addresses"`
	// DeploymentFile is gno-eth-ibc's contracts/deployments/<chain>.json; its
	// addresses fill any address left empty above.
	DeploymentFile string `yaml:"deployment_file"`
	// HistoryFromBlock is the transfer proxy's deployment block, where the
	// route history (GnoRouteMigrated) is read from.
	HistoryFromBlock uint64 `yaml:"history_from_block"`
	// HistoryFromDeployment uses deployBlock from deployment_file when no explicit start is set.
	HistoryFromDeployment bool `yaml:"history_from_deployment"`
}

type Monitor struct {
	PollInterval          time.Duration `yaml:"poll_interval"`
	DeferredAlarmFailures int           `yaml:"deferred_alarm_failures"`
	// RequestTimeout bounds every RPC request on both chains (default 20s);
	// CollectionTimeout bounds each chain's collection (default 60s). A
	// provider that never answers is a failed collection, never a stall
	// (design §7.11).
	RequestTimeout    time.Duration `yaml:"request_timeout"`
	CollectionTimeout time.Duration `yaml:"collection_timeout"`
	// DiscoveryWait bounds how long the Ethereum collection waits for the
	// first gno discovery before publishing without it (default 5s).
	DiscoveryWait time.Duration `yaml:"discovery_wait"`
	// Freshness bounds the selected head per chain (design §7.11).
	Freshness    Freshness `yaml:"freshness"`
	ClientExpiry struct {
		WarnBefore     time.Duration            `yaml:"warn_before"`
		CriticalBefore time.Duration            `yaml:"critical_before"`
		StaleAfter     map[string]time.Duration `yaml:"stale_after"`
	} `yaml:"client_expiry"`
}

// Freshness bounds the selected head (design §7.11): a genuine but old
// head is stale and its collection not fresh.
type Freshness struct {
	GnoMaxHeadAge      time.Duration `yaml:"gno_max_head_age"`          // default 2m
	EthereumMaxHeadAge time.Duration `yaml:"ethereum_max_head_age"`     // default 45m
	GnoMaxProviderLag  uint64        `yaml:"gno_max_provider_lag"`      // default 20 blocks
	EthMaxProviderLag  uint64        `yaml:"ethereum_max_provider_lag"` // default 64 blocks
	HeadStallAfter     time.Duration `yaml:"head_stall_after"`          // default 10m
}

type Sink struct {
	Type   string `yaml:"type"`
	URL    string `yaml:"url"`
	Secret string `yaml:"secret"`
}

type Alerts struct {
	Sinks []Sink `yaml:"sinks"`
	// Queue is the per-sink queue length (256); BacklogCritical how long a
	// sink's undelivered set may stand before sink_backlog is critical
	// (10m); FailingAfter how long a sink may refuse every delivery before
	// sink_failing (5m); ReconcileEvery the resend timer (30s). Design §8.2.
	Queue           int           `yaml:"queue"`
	BacklogCritical time.Duration `yaml:"sink_backlog_critical"`
	FailingAfter    time.Duration `yaml:"sink_failing_after"`
	ReconcileEvery  time.Duration `yaml:"reconcile_every"`
}

type AutoFlag struct {
	Enabled bool `yaml:"enabled"`
	DryRun  bool `yaml:"dry_run"`
}

type Worker struct {
	Enabled           bool   `yaml:"enabled"`
	DockerImage       string `yaml:"docker_image"`
	GnoEthIBCRevision string `yaml:"gno_eth_ibc_revision"`
	GnoEthIBCCheckout string `yaml:"gno_eth_ibc_checkout"`
	Manifest          string `yaml:"manifest"`
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Load reads and expands the file. A ${VAR} that is not set expands to "".
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b, os.LookupEnv)
}

// Parse parses YAML text, expanding ${VAR} with lookup.
func Parse(b []byte, lookup func(string) (string, bool)) (*Config, error) {
	expanded := envRef.ReplaceAllStringFunc(string(b), func(m string) string {
		name := envRef.FindStringSubmatch(m)[1]
		v, _ := lookup(name)
		return v
	})
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(expanded))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: %w", err)
	}
	c.defaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) defaults() {
	if c.Instance.Listen == "" {
		c.Instance.Listen = ":8080"
	}
	if c.Instance.History == 0 {
		c.Instance.History = 500
	}
	if c.Gno.Name == "" {
		c.Gno.Name = "gno-primary"
	}
	if c.Gno.Name2 == "" {
		c.Gno.Name2 = "gno-second"
	}
	if c.Eth.Name == "" {
		c.Eth.Name = "eth-primary"
	}
	if c.Eth.Name2 == "" {
		c.Eth.Name2 = "eth-second"
	}
	if c.Gno.ChainID == "" {
		c.Gno.ChainID = "test-ibc-1"
	}
	if c.Gno.BrowserRPCURL == "" {
		c.Gno.BrowserRPCURL = "https://rpc.2-29-108-12.sslip.io"
	}
	if c.Gno.WebURL == "" {
		c.Gno.WebURL = "https://gnoweb.2-29-108-12.sslip.io"
	}
	if c.Gno.ClientType == "" {
		c.Gno.ClientType = "ethereum"
	}
	if c.Gno.RouteClientID == "" {
		c.Gno.RouteClientID = c.Gno.ClientType + "-1"
	}
	if c.Eth.ChainID == "" {
		c.Eth.ChainID = "11155111"
	}
	if c.Eth.ChainName == "" {
		c.Eth.ChainName = "Ethereum"
	}
	if c.Eth.NetworkName == "" {
		c.Eth.NetworkName = c.Eth.ChainName
		if c.Eth.ChainName == "Ethereum" {
			c.Eth.NetworkName = "Sepolia"
		}
	}
	if c.Eth.ExplorerURL == "" {
		c.Eth.ExplorerURL = "https://sepolia.etherscan.io"
	}
	if c.Eth.BrowserRPCURL == "" {
		c.Eth.BrowserRPCURL = "https://rpc.sepolia.ethpandaops.io"
	}
	if c.Eth.NativeCurrency.Name == "" {
		c.Eth.NativeCurrency = NativeCurrency{Name: "Sepolia Ether", Symbol: "ETH", Decimals: 18}
	}
	if c.Eth.LogChunk == 0 {
		c.Eth.LogChunk = 2000
	}
	if c.Eth.Finality == "" {
		c.Eth.Finality = "tag"
	}
	if c.Monitor.PollInterval == 0 {
		c.Monitor.PollInterval = 10 * time.Second
	}
	if c.Alerts.ReconcileEvery == 0 {
		c.Alerts.ReconcileEvery = 30 * time.Second
	}
	if c.Monitor.RequestTimeout == 0 {
		c.Monitor.RequestTimeout = 20 * time.Second
	}
	if c.Monitor.CollectionTimeout == 0 {
		c.Monitor.CollectionTimeout = 60 * time.Second
	}
	if c.Monitor.DiscoveryWait == 0 {
		c.Monitor.DiscoveryWait = 5 * time.Second
	}
	fr := &c.Monitor.Freshness
	if fr.GnoMaxHeadAge == 0 {
		fr.GnoMaxHeadAge = 2 * time.Minute
	}
	if fr.EthereumMaxHeadAge == 0 {
		fr.EthereumMaxHeadAge = 45 * time.Minute
	}
	if fr.GnoMaxProviderLag == 0 {
		fr.GnoMaxProviderLag = 20
	}
	if fr.EthMaxProviderLag == 0 {
		fr.EthMaxProviderLag = 64
	}
	if fr.HeadStallAfter == 0 {
		fr.HeadStallAfter = 10 * time.Minute
	}
	if c.Monitor.DeferredAlarmFailures == 0 {
		c.Monitor.DeferredAlarmFailures = 6
	}
	if c.Monitor.ClientExpiry.WarnBefore == 0 {
		c.Monitor.ClientExpiry.WarnBefore = 72 * time.Hour
	}
	if c.Monitor.ClientExpiry.CriticalBefore == 0 {
		c.Monitor.ClientExpiry.CriticalBefore = 12 * time.Hour
	}
	if c.Worker.DockerImage == "" {
		c.Worker.DockerImage = "ghcr.io/foundry-rs/foundry:v1.7.1"
	}
	if len(c.Alerts.Sinks) == 0 {
		c.Alerts.Sinks = []Sink{{Type: "log"}}
	}
}

func (c *Config) validate() error {
	if id, err := strconv.ParseUint(c.Eth.ChainID, 10, 64); err != nil || id == 0 {
		return errors.New("config: eth.chain_id must be a positive decimal uint64")
	}
	if c.Gno.ClientType != "ethereum" && c.Gno.ClientType != "arc" {
		return errors.New("config: gno.client_type must be ethereum or arc")
	}
	for _, endpoint := range []string{c.Gno.BrowserRPCURL, c.Gno.WebURL, c.Eth.BrowserRPCURL, c.Eth.ExplorerURL} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("config: browser URLs must be public HTTP(S) URLs without credentials, query or fragment")
		}
	}
	if c.AutoFlag.Enabled && c.Instance.Public {
		return errors.New("config: autoflag cannot be enabled on a public instance")
	}
	if c.Instance.CrossCheckRequired() {
		if c.Gno.RPCURL != "" && c.Gno.RPCURL2 == "" {
			return errors.New("config: gno.rpc_url_2 is required (instance.require_cross_check is on; design §3.3)")
		}
		if c.Eth.RPCURL != "" && c.Eth.RPCURL2 == "" {
			return errors.New("config: eth.rpc_url_2 is required (instance.require_cross_check is on; design §3.3)")
		}
	}
	for _, s := range c.Alerts.Sinks {
		switch s.Type {
		case "log":
		case "webhook":
			if s.URL == "" {
				return errors.New("config: webhook sink needs a url")
			}
		default:
			return fmt.Errorf("config: unknown sink type %q", s.Type)
		}
	}
	return nil
}
