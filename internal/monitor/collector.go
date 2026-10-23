package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethviews"
	"github.com/allinbits/ibc-monitoring/internal/gnoviews"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/redact"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// Collector reads both chains into the store. Each collection pins one
// height per chain, checks its hash against the second provider (header
// agreement, docs/design.md §3.3) and stamps every snapshot with that
// verification and the collection it came from. It fails closed: a second
// provider that is configured but cannot answer, or no second provider on an
// instance that requires one, leaves the snapshot unverified.
type Collector struct {
	Gno  *gnoviews.Views
	Gno2 *gnoviews.Views // second provider for the header cross-check
	Eth  *ethviews.Views
	Eth2 *ethrpc.Client // second provider
	// RequireCrossCheck refuses `provider`-level verification: without a
	// second provider every snapshot is unverified (guardian instances).
	RequireCrossCheck bool
	// EthVerification is the primary endpoint's level when it is a light
	// client ("light-client"); otherwise "provider".
	EthVerification model.Verification
	Store           store.Store
	Log             *slog.Logger
	// Timeout bounds each chain's collection (docs/design.md §7.11): a
	// collection past it is a failed collection for that chain. Zero leaves
	// it unbounded (tests).
	Timeout time.Duration
	// DiscoveryWait bounds how long the Ethereum collection waits for the
	// first gno discovery before publishing its own subjects without it
	// (default 5 s, never its whole deadline).
	DiscoveryWait time.Duration
	// Freshness bounds the selected head (docs/design.md §7.11): a genuine
	// but old head is stale, and its collection not fresh. Zero bounds are
	// unenforced (tests); the configuration sets them.
	Freshness Freshness
	// Now is the clock the bounds are judged against (tests set it).
	Now func() time.Time

	mu         sync.Mutex
	collection uint64
	gnoState   ChainState
	ethState   ChainState
	// last is the latest pins snapshot; a chain that has not completed the
	// current round keeps its previous part in it.
	last Pins
	// discoveryReady is closed when the first gno discovery is published;
	// the Ethereum collection waits on it only while none exists.
	initOnce       sync.Once
	discoveryReady chan struct{}
	discoveryOnce  sync.Once
	// progress is, per chain, the last pinned height and when it last
	// advanced (the progress bound).
	progress map[model.Chain]*progressMark
}

type progressMark struct {
	height  uint64
	advance time.Time
}

// Freshness bounds the selected pin per chain (docs/design.md §7.11).
type Freshness struct {
	// MaxHeadAge is how old the pin's block time may be.
	MaxGnoHeadAge, MaxEthHeadAge time.Duration
	// MaxProviderLag is how many blocks the second provider's own head may
	// be past the selected pin before the primary counts as withholding.
	MaxGnoProviderLag, MaxEthProviderLag uint64
	// HeadStallAfter is how long the pin may stay at one height while the
	// chain collects without error.
	HeadStallAfter time.Duration
}

// Heads are both providers' independently reported heads for one chain and
// the selected pin's age at collection.
type Heads struct {
	Primary uint64 `json:"primary"`
	Second  uint64 `json:"second,omitempty"`
	// AgeSeconds is the collection time minus the pin's block time.
	AgeSeconds int64 `json:"age_seconds"`
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// staleHead judges the selected pin against the bounds: "" when fresh.
func staleHead(h Heads, maxAge time.Duration, maxLag uint64) string {
	if maxAge > 0 && h.AgeSeconds > int64(maxAge/time.Second) {
		return fmt.Sprintf("the pinned block is %s old, past the %s bound", (time.Duration(h.AgeSeconds) * time.Second).String(), maxAge)
	}
	if maxLag > 0 && h.Second > h.Primary && h.Second-h.Primary > maxLag {
		return fmt.Sprintf("the second provider's own head %d is %d blocks past the pinned %d, past the %d bound: the primary is withholding", h.Second, h.Second-h.Primary, h.Primary, maxLag)
	}
	return ""
}

func (c *Collector) init() {
	c.initOnce.Do(func() { c.discoveryReady = make(chan struct{}) })
}

// Chains lists the chains this collector reads.
func (c *Collector) Chains() []model.Chain {
	var out []model.Chain
	if c.Gno != nil {
		out = append(out, model.ChainGno)
	}
	if c.Eth != nil {
		out = append(out, model.ChainEthereum)
	}
	return out
}

// GnoDiscovery is the gno collection's clients and routes as one immutable
// object, published atomically with the snapshots they came from, so that
// the Ethereum collection reads a coherent view of one gno collection
// (docs/design.md §7.11).
type GnoDiscovery struct {
	Collection uint64                  `json:"collection"`
	Pin        gnoviews.Pin            `json:"pin"`
	Verified   model.Verification      `json:"verification"`
	Clients    map[string]model.Client `json:"clients"`
	Routes     []model.Route           `json:"routes"`
	// Node and ReadAt are the provider the discovery was read from and
	// when its last view was read.
	Node   string    `json:"node"`
	ReadAt time.Time `json:"read_at"`
}

// Provenance is the provenance of a value taken from the discovery: its
// pin, provider, read time and verification.
func (d GnoDiscovery) Provenance() model.Provenance {
	return model.Provenance{Chain: model.ChainGno, Height: uint64(d.Pin.Height), BlockHash: d.Pin.BlockHash, Node: d.Node, ReadAt: d.ReadAt,
		Verification: d.Verified, Finality: "committed", StateVerification: model.StateNone, Collection: d.Collection}
}

// ChainState is one chain's collection readiness (docs/design.md §7.11).
type ChainState struct {
	// OK is true when the latest collection read the chain without error.
	OK bool `json:"ok"`
	// Verified is true when the latest collection's header was agreed (or
	// light-client verified); an unverified collection is not fresh.
	Verified bool `json:"verified"`
	// Complete is true when the latest collection had every input it
	// needs; one whose cross-chain dependency was missing (the Ethereum
	// collection without a gno discovery) is collected but not fresh, and
	// Incomplete says what it lacked (docs/design.md §7.11).
	Complete   bool   `json:"complete"`
	Incomplete string `json:"incomplete,omitempty"`
	// Stale is true when the latest collection's pin failed a freshness
	// bound (head age, provider lag, progress; docs/design.md §7.11):
	// collected and agreed, but an old head, so not fresh. StaleReason says
	// which bound; ConsecutiveStale counts such collections in a row.
	Stale            bool   `json:"stale"`
	StaleReason      string `json:"stale_reason,omitempty"`
	ConsecutiveStale int    `json:"consecutive_stale"`
	// Error is the latest collection's error, redacted ("" when OK).
	Error string `json:"error,omitempty"`
	// ConsecutiveFailures counts collections that failed in a row;
	// ConsecutiveUnverified counts collections whose header did not agree.
	ConsecutiveFailures   int       `json:"consecutive_failures"`
	ConsecutiveUnverified int       `json:"consecutive_unverified"`
	LastSuccess           time.Time `json:"last_success,omitempty"`
	// LastCollection is the collection id of the latest successful,
	// header-agreed read; snapshots carrying another id are stale.
	LastCollection uint64 `json:"last_collection"`
}

// Pins are the heights a collection was read at and each chain's state.
type Pins struct {
	Collection    uint64             `json:"collection"`
	CollectedAt   time.Time          `json:"collected_at"`
	Gno           gnoviews.Pin       `json:"gno"`
	GnoVerified   model.Verification `json:"gno_verification"`
	GnoCrossCheck string             `json:"gno_cross_check,omitempty"`
	GnoHeads      Heads              `json:"gno_heads"`
	GnoState      ChainState         `json:"gno_state"`
	Eth           ethrpc.Block       `json:"eth"`
	EthVerified   model.Verification `json:"eth_verification"`
	EthCrossCheck string             `json:"eth_cross_check,omitempty"`
	EthHeads      Heads              `json:"eth_heads"`
	// EthFinalityIndependent is true when the second provider's own
	// finalized head confirmed the selected block (design §3.3); integrity
	// containment requires it.
	EthFinalityIndependent bool       `json:"eth_finality_independent"`
	EthState               ChainState `json:"eth_state"`
	// GnoError and EthError repeat the states' errors for the dashboard.
	GnoError string `json:"gno_error,omitempty"`
	EthError string `json:"eth_error,omitempty"`
}

// Fresh reports whether a snapshot of chain is from that chain's latest
// successful, header-agreed collection.
// Readiness is per subject: a chain whose cross-chain dependency was
// missing is still fresh for the snapshots that need nothing from the other
// chain (its clients, its governance); the snapshots that do (the routes)
// carry what they lack themselves (model.EthRoute.Incomplete).
func (p Pins) Fresh(chain model.Chain, prov model.Provenance) bool {
	st := p.State(chain)
	return st.OK && st.Verified && !st.Stale && prov.Collection == st.LastCollection
}

// State returns one chain's state.
func (p Pins) State(chain model.Chain) ChainState {
	if chain == model.ChainEthereum {
		return p.EthState
	}
	return p.GnoState
}

// Collect reads every view once; see CollectWith.
func (c *Collector) Collect(ctx context.Context) Pins { return c.CollectWith(ctx, nil) }

// CollectWith reads every view once, both chains concurrently, each under
// its own deadline, and calls onChain, when given, as each chain publishes
// (docs/design.md §7.11). One round for the `check` command and the tests;
// the engine's loops collect each chain on its own schedule with
// CollectChain.
func (c *Collector) CollectWith(ctx context.Context, onChain func(model.Chain, Pins)) Pins {
	var wg sync.WaitGroup
	for _, chain := range c.Chains() {
		wg.Add(1)
		go func(chain model.Chain) {
			defer wg.Done()
			p := c.CollectChain(ctx, chain)
			if onChain != nil {
				onChain(chain, p)
			}
		}(chain)
	}
	wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// CollectChain reads one chain's views under the collector's deadline and
// publishes them as one atomic bundle, then the pins snapshot with this
// chain's state updated (the other chain's part is its latest), and
// returns the published pins. Each chain runs on its own schedule, so a
// stall on one costs the other nothing. A collection that runs past Timeout
// is a failed collection for that chain.
func (c *Collector) CollectChain(ctx context.Context, chain model.Chain) Pins {
	c.init()
	c.mu.Lock()
	c.collection++
	id := c.collection
	c.last.Collection, c.last.CollectedAt = id, time.Now()
	c.mu.Unlock()
	cctx, cancel := ctx, context.CancelFunc(func() {})
	if c.Timeout > 0 {
		cctx, cancel = context.WithTimeout(ctx, c.Timeout)
	}
	defer cancel()
	var local Pins
	var incomplete, stale string
	var err error
	var height uint64
	switch chain {
	case model.ChainGno:
		stale, err = c.collectGno(cctx, &local, id)
		height = uint64(local.Gno.Height)
	case model.ChainEthereum:
		incomplete, stale, err = c.collectEth(cctx, &local, id)
		height = local.Eth.Number
	default:
		err = fmt.Errorf("unknown chain %q", chain)
	}
	if err != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		err = fmt.Errorf("collection timed out after %s: %w", c.Timeout, err)
	}
	c.mu.Lock()
	if err == nil && stale == "" {
		stale = c.checkProgress(chain, height)
	}
	p := &c.last
	at := p.CollectedAt
	if chain == model.ChainGno {
		p.Gno, p.GnoVerified, p.GnoCrossCheck, p.GnoHeads = local.Gno, local.GnoVerified, local.GnoCrossCheck, local.GnoHeads
		c.gnoState = c.gnoState.update(err, verified(local.GnoVerified), incomplete, stale, id, at)
		p.GnoState, p.GnoError = c.gnoState, c.gnoState.Error
	} else {
		p.Eth, p.EthVerified, p.EthCrossCheck, p.EthFinalityIndependent, p.EthHeads = local.Eth, local.EthVerified, local.EthCrossCheck, local.EthFinalityIndependent, local.EthHeads
		c.ethState = c.ethState.update(err, verified(local.EthVerified), incomplete, stale, id, at)
		p.EthState, p.EthError = c.ethState, c.ethState.Error
	}
	published := *p
	c.Store.PutSnapshot(store.Snapshot{Kind: store.KindPins, Key: "latest", Value: published})
	c.mu.Unlock()
	if chain == model.ChainGno && err == nil {
		// The discovery is usable only once this state is published.
		c.discoveryOnce.Do(func() { close(c.discoveryReady) })
	}
	if err != nil {
		c.log().Error(string(chain)+" collection failed", "err", redact.String(err.Error()))
	} else if stale != "" {
		c.log().Warn(string(chain)+" pin is stale", "reason", stale)
	} else if incomplete != "" {
		c.log().Warn(string(chain)+" collection incomplete", "missing", incomplete)
	}
	return published
}

// checkProgress (under mu) applies the progress bound: a pin that has not
// advanced for HeadStallAfter while the chain collects without error.
func (c *Collector) checkProgress(chain model.Chain, height uint64) string {
	if c.progress == nil {
		c.progress = map[model.Chain]*progressMark{}
	}
	now := c.now()
	m := c.progress[chain]
	if m == nil || height != m.height {
		c.progress[chain] = &progressMark{height: height, advance: now}
		return ""
	}
	if c.Freshness.HeadStallAfter > 0 && now.Sub(m.advance) > c.Freshness.HeadStallAfter {
		return fmt.Sprintf("the pin has stayed at %d since %s, past the %s progress bound", height, m.advance.UTC().Format(time.RFC3339), c.Freshness.HeadStallAfter)
	}
	return ""
}

// usableDiscovery returns the latest gno discovery when gno.land's latest
// collection succeeded, agreed and is the one the discovery came from; a
// discovery a failing or unverified gno.land left behind is stale and is
// reported as such (docs/design.md §7.11).
func (c *Collector) usableDiscovery() (GnoDiscovery, string) {
	disc, ok := c.discovery()
	if !ok {
		return GnoDiscovery{}, "no gno discovery yet"
	}
	c.mu.Lock()
	gs := c.gnoState
	c.mu.Unlock()
	switch {
	case !gs.OK:
		return GnoDiscovery{}, fmt.Sprintf("gno discovery %d is stale: gno.land's latest collection failed", disc.Collection)
	case !gs.Verified:
		return GnoDiscovery{}, fmt.Sprintf("gno discovery %d is stale: gno.land's latest collection is unverified", disc.Collection)
	case disc.Collection != gs.LastCollection:
		return GnoDiscovery{}, fmt.Sprintf("gno discovery %d is stale: gno.land's latest collection is %d", disc.Collection, gs.LastCollection)
	}
	return disc, ""
}

// discovery returns the latest gno discovery.
func (c *Collector) discovery() (GnoDiscovery, bool) {
	s, ok := c.Store.Snapshot(store.KindGnoDiscovery, "latest")
	if !ok {
		return GnoDiscovery{}, false
	}
	d, ok := s.Value.(GnoDiscovery)
	return d, ok
}

func verified(v model.Verification) bool {
	return v == model.VerificationCrossChecked || v == model.VerificationLightClient
}

func (s ChainState) update(err error, verified bool, incomplete, stale string, collection uint64, at time.Time) ChainState {
	if err != nil {
		s.OK = false
		s.Verified = false
		s.Complete = false
		s.Incomplete = ""
		s.Stale, s.StaleReason = false, ""
		s.Error = redact.String(err.Error())
		s.ConsecutiveFailures++
		return s
	}
	if !verified {
		// Read, but the header did not agree: the snapshots are not fresh.
		s.OK = true
		s.Verified = false
		s.Complete = incomplete == ""
		s.Incomplete = incomplete
		s.Stale, s.StaleReason = false, ""
		s.Error = ""
		s.ConsecutiveFailures = 0
		s.ConsecutiveUnverified++
		return s
	}
	if stale != "" {
		// Read and agreed, but an old head (design §7.11): not fresh; the
		// last fresh collection stays the reference.
		s.OK = true
		s.Verified = true
		s.Complete = incomplete == ""
		s.Incomplete = incomplete
		s.Stale, s.StaleReason = true, stale
		s.Error = ""
		s.ConsecutiveFailures = 0
		s.ConsecutiveUnverified = 0
		s.ConsecutiveStale++
		return s
	}
	// Read and agreed. A missing cross-chain dependency leaves the chain
	// fresh for its own subjects (readiness is per subject, design §7.11):
	// the route snapshots carry what they lack; the state says so.
	return ChainState{OK: true, Verified: true, Complete: incomplete == "", Incomplete: incomplete, LastSuccess: at, LastCollection: collection}
}

func (c *Collector) log() *slog.Logger {
	if c.Log == nil {
		return slog.Default()
	}
	return c.Log
}

// verification applies the fail-closed rule to a header cross-check.
func (c *Collector) verification(primary model.Verification, hasSecond bool, secondErr error, agree bool, mismatch string) (model.Verification, string) {
	switch {
	case !hasSecond && c.RequireCrossCheck:
		return model.VerificationUnverified, "no second provider configured; instance.require_cross_check is on"
	case !hasSecond:
		return primary, "no second provider configured"
	case secondErr != nil:
		return model.VerificationUnverified, "second provider unavailable: " + redact.String(secondErr.Error())
	case !agree:
		return model.VerificationUnverified, mismatch
	case primary == model.VerificationLightClient:
		return primary, "agreed"
	default:
		return model.VerificationCrossChecked, "agreed"
	}
}

func (c *Collector) collectGno(ctx context.Context, pins *Pins, collection uint64) (string, error) {
	pin, err := c.Gno.LatestPin(ctx)
	if err != nil {
		return "", err
	}
	pins.Gno = pin
	pins.GnoHeads = Heads{Primary: uint64(pin.Height), AgeSeconds: int64(c.now().Sub(pin.Time) / time.Second)}
	var otherErr error
	agree := false
	mismatch := ""
	if c.Gno2 != nil {
		var other gnoviews.Pin
		other, otherErr = c.Gno2.PinAt(ctx, pin.Height)
		if otherErr == nil {
			// Every security-critical header field is compared, not only the
			// hash (docs/design.md §3.3).
			var diff string
			agree, diff = pin.Agrees(other)
			if !agree {
				mismatch = fmt.Sprintf("header mismatch at %d: %s", pin.Height, diff)
			}
		}
		// The second provider's own head, kept independently of the
		// primary's choice (docs/design.md §7.11: freshness bounds).
		if head, err := c.Gno2.LatestPin(ctx); err == nil {
			pins.GnoHeads.Second = uint64(head.Height)
		} else if otherErr == nil {
			otherErr = err
		}
	}
	pins.GnoVerified, pins.GnoCrossCheck = c.verification(model.VerificationProvider, c.Gno2 != nil, otherErr, agree, mismatch)
	stale := staleHead(pins.GnoHeads, c.Freshness.MaxGnoHeadAge, c.Freshness.MaxGnoProviderLag)
	if stale != "" {
		pins.GnoCrossCheck = strings.TrimSuffix(pins.GnoCrossCheck+"; ", "; ") + "stale: " + stale
	}

	stamp := func(p *model.Provenance) {
		p.Verification = pins.GnoVerified
		p.Collection = collection
		p.StateVerification = model.StateNone
	}
	// Everything is staged and published as one bundle, with the discovery
	// record: nothing of a failed collection is published, and a reader
	// never sees this round's clients beside the previous round's routes.
	var bundle []store.Snapshot
	disc := GnoDiscovery{Collection: collection, Pin: pin, Clients: map[string]model.Client{}}
	clients, err := c.Gno.Clients(ctx, pin)
	if err != nil {
		return "", err
	}
	for _, cl := range clients {
		stamp(&cl.Provenance)
		bundle = append(bundle, store.Snapshot{Kind: store.KindGnoClients, Key: cl.ID, Value: cl, Provenance: cl.Provenance})
		disc.Clients[cl.ID] = cl
	}
	routes, err := c.Gno.Routes(ctx, pin)
	if err != nil {
		return "", err
	}
	for _, r := range routes {
		stamp(&r.Provenance)
		bundle = append(bundle, store.Snapshot{Kind: store.KindGnoRoutes, Key: r.Key, Value: r, Provenance: r.Provenance})
		disc.Routes = append(disc.Routes, r)
	}
	// The flag subjects are the route keys plus every client outside the
	// routes: a flag on a standalone client counts toward the global hold
	// (ADR §7.11); a route's current or replaced client is an alias of its
	// key and is read under the key only (docs/design.md §4.1).
	gov, err := c.Gno.Governance(ctx, flagSubjects(routes, clients), pin)
	if err != nil {
		return "", err
	}
	stamp(&gov.Provenance)
	bundle = append(bundle, store.Snapshot{Kind: store.KindGnoGovernance, Key: "ibcgov", Value: gov, Provenance: gov.Provenance})
	disc.Verified = pins.GnoVerified
	disc.Node, disc.ReadAt = gov.Provenance.Node, gov.Provenance.ReadAt
	bundle = append(bundle, store.Snapshot{Kind: store.KindGnoDiscovery, Key: "latest", Value: disc, Provenance: model.Provenance{Chain: model.ChainGno, Height: uint64(pin.Height), Collection: collection, Verification: pins.GnoVerified}})
	c.Store.PutSnapshots(bundle)
	return stale, nil
}

// flagSubjects is every route key plus every client id that is no route's
// current or replaced client, in order, without duplicates.
func flagSubjects(routes []model.Route, clients []model.Client) []string {
	alias := map[string]bool{}
	var out []string
	seen := map[string]bool{}
	for _, r := range routes {
		alias[r.Key], alias[r.Client] = true, true
		for _, old := range r.Replaced {
			alias[old] = true
		}
		if !seen[r.Key] {
			seen[r.Key] = true
			out = append(out, r.Key)
		}
	}
	for _, cl := range clients {
		if alias[cl.ID] || seen[cl.ID] {
			continue
		}
		seen[cl.ID] = true
		out = append(out, cl.ID)
	}
	return out
}

// collectEth reads the Ethereum views. It discovers the route on Ethereum
// itself and reads one immutable gno discovery for the inputs it cannot get
// there (the denoms' tokens, the relationship); on start-up, when none
// exists, it waits for this round's gno collection (gnoDone) within its own
// deadline. Without a usable discovery the collection is incomplete: the
// returned string says what it lacks (docs/design.md §7.11).
func (c *Collector) collectEth(ctx context.Context, pins *Pins, collection uint64) (incomplete, stale string, err error) {
	b, err := c.Eth.Pinned(ctx)
	if err != nil {
		return "", "", err
	}
	pins.Eth = b
	pins.EthHeads = Heads{Primary: b.Number, AgeSeconds: c.now().Unix() - int64(b.Timestamp)}
	primary := c.EthVerification
	if primary == "" {
		primary = model.VerificationProvider
	}
	var otherErr error
	agree := false
	mismatch := ""
	if c.Eth2 != nil {
		// Finality is established independently (docs/design.md §3.3): the
		// second provider's own finalized head must be at or past the block
		// the primary chose, and its header at that number must agree. In
		// the depth and latest modes no finality exists; the pin says so.
		var other ethrpc.Block
		other, otherErr = c.Eth2.BlockByNumber(ctx, b.Number)
		if otherErr == nil {
			agree = other.Hash == b.Hash
			if !agree {
				mismatch = fmt.Sprintf("header mismatch at %d: %s vs %s", b.Number, b.Hash, other.Hash)
			}
		}
		// The second provider's own head (its finalized head in the tag
		// mode), kept independently of the primary's choice: finality is
		// established by it (design §3.3), and the freshness bounds judge
		// the pin against it (§7.11).
		if otherErr == nil {
			var head ethrpc.Block
			head, otherErr = c.Eth2.Pinned(ctx)
			if otherErr == nil {
				pins.EthHeads.Second = head.Number
				if agree && b.Finality == "finalized" && head.Number < b.Number {
					agree = false
					mismatch = fmt.Sprintf("block %d is not finalized by the second provider (its finalized head is %d)", b.Number, head.Number)
				}
			}
		}
	}
	pins.EthVerified, pins.EthCrossCheck = c.verification(primary, c.Eth2 != nil, otherErr, agree, mismatch)
	if agree && c.Eth2 != nil && otherErr == nil && b.Finality == "finalized" {
		pins.EthCrossCheck = "agreed; finalized by the second provider"
		pins.EthFinalityIndependent = true
	}
	stale = staleHead(pins.EthHeads, c.Freshness.MaxEthHeadAge, c.Freshness.MaxEthProviderLag)
	if stale != "" {
		pins.EthCrossCheck = strings.TrimSuffix(pins.EthCrossCheck+"; ", "; ") + "stale: " + stale
	}

	stamp := func(p *model.Provenance) {
		p.Verification = pins.EthVerified
		p.Collection = collection
		p.StateVerification = model.StateNone
	}
	addrs := c.Eth.Addresses()
	gov, err := c.Eth.Governance(ctx, b)
	if err != nil {
		return "", "", err
	}
	stamp(&gov.Provenance)
	var bundle []store.Snapshot
	bundle = append(bundle, store.Snapshot{Kind: store.KindEthGovernance, Key: "ibcadmin", Value: gov, Provenance: gov.Provenance})

	// TODO(phase 2): resolve the route's verifier from ICS02ClientAdded /
	// ICS02ClientMigrated logs instead of the configured address.
	if addrs.LightClient != (common.Address{}) {
		lc, err := c.Eth.LightClient(ctx, addrs.LightClient, gov.GovClient, "route", b)
		if err != nil {
			return "", "", err
		}
		stamp(&lc.Provenance)
		bundle = append(bundle, store.Snapshot{Kind: store.KindEthClients, Key: lc.Address, Value: lc, Provenance: lc.Provenance})
	}

	// The chain's own subjects (governance, clients) are published whatever
	// happens to the route below: their alarms never wait on gno.land or on
	// the route read (readiness per subject, docs/design.md §7.11).
	partial := func(what string, err error) (string, string, error) {
		c.Store.PutSnapshots(bundle)
		return "route: " + what + ": " + redact.String(err.Error()), stale, nil
	}
	// The route is discovered on Ethereum: the governance client's route
	// key, its current client at the pin and its generations from
	// Ethereum's own history (review finding 7). gno.land's view of it is
	// compared separately below.
	d, err := c.Eth.Discover(ctx, gov.GovClient, b)
	if err != nil {
		return partial("discovery of "+gov.GovClient, err)
	}
	// One immutable gno discovery, taken now; on start-up, when none exists
	// yet, the first gno discovery is awaited for at most DiscoveryWait,
	// never the whole deadline.
	if _, missing := c.usableDiscovery(); missing != "" && c.Gno != nil {
		wait := c.DiscoveryWait
		if wait <= 0 {
			wait = 5 * time.Second
		}
		if dl, ok := ctx.Deadline(); ok {
			if left := time.Until(dl) / 2; left < wait {
				wait = left
			}
		}
		select {
		case <-c.discoveryReady:
		case <-time.After(wait):
		case <-ctx.Done():
		}
	}
	if ctx.Err() != nil {
		return partial("read", ctx.Err())
	}
	disc, missing := c.usableDiscovery()
	var missingInputs []string
	var tokens []ethviews.RouteToken
	var er model.EthRoute
	switch {
	case missing != "":
		missingInputs = append(missingInputs, missing+": the route's tokens and gno relationship are unknown")
	default:
		er.GnoDiscovery = disc.Collection
		gp := disc.Provenance()
		er.GnoProvenance = &gp
		if r, cp, found := matchGnoRoute(disc, d); found {
			er.GnoRoute, er.GnoCounterparty = r.Key, cp
			er.RelationshipMismatch = relationship(disc, r, cp, d)
			seen := map[common.Address]bool{}
			for _, dn := range r.Denoms {
				if !common.IsHexAddress(dn.Token) {
					continue
				}
				a := common.HexToAddress(dn.Token)
				if !seen[a] {
					seen[a] = true
					origin := ""
					switch dn.Kind {
					case "mint":
						origin = ethviews.OriginEthereum
					case "unescrow":
						origin = ethviews.OriginGno
					}
					tokens = append(tokens, ethviews.RouteToken{Address: a, Origin: origin})
				}
			}
		} else {
			er.RelationshipMismatch = fmt.Sprintf("no gno.land route names Ethereum's route %s (current client %s) as its counterparty", d.RouteKey, d.CurrentClient)
			missingInputs = append(missingInputs, "no gno route matches Ethereum's route: its tokens are unknown")
		}
	}
	pre := er
	if er, err = c.Eth.Route(ctx, d, tokens, b); err != nil {
		return partial(d.RouteKey, err)
	}
	er.GnoDiscovery, er.GnoProvenance, er.GnoRoute, er.GnoCounterparty, er.RelationshipMismatch, er.Incomplete = pre.GnoDiscovery, pre.GnoProvenance, pre.GnoRoute, pre.GnoCounterparty, pre.RelationshipMismatch, missingInputs
	stamp(&er.Provenance)
	bundle = append(bundle, store.Snapshot{Kind: store.KindEthRoutes, Key: er.RouteKey, Value: er, Provenance: er.Provenance})
	c.Store.PutSnapshots(bundle)
	return strings.Join(missingInputs, "; "), stale, nil
}

// matchGnoRoute finds the gno route whose client, or one of whose replaced
// clients, names one of the Ethereum route's clients as its counterparty.
func matchGnoRoute(disc GnoDiscovery, d ethviews.RouteDiscovery) (model.Route, string, bool) {
	ethIDs := map[string]bool{d.CurrentClient: true}
	for _, id := range d.Retired {
		ethIDs[id] = true
	}
	for _, r := range disc.Routes {
		cp := disc.Clients[r.Client].CounterpartyID
		if cp != "" && ethIDs[cp] {
			return r, cp, true
		}
		for _, old := range r.Replaced {
			if oc := disc.Clients[old].CounterpartyID; oc != "" && ethIDs[oc] {
				return r, cp, true
			}
		}
	}
	return model.Route{}, "", false
}

// relationship compares gno.land's view of the route (its client's
// counterparty, its replaced clients' counterparties) with Ethereum's own
// (currentClient, the migrations); "" when they agree.
func relationship(disc GnoDiscovery, r model.Route, cp string, d ethviews.RouteDiscovery) string {
	var diffs []string
	if cp != d.CurrentClient {
		diffs = append(diffs, fmt.Sprintf("gno.land's route client %s names %s as the current client; Ethereum's currentClient(%s) is %s", r.Client, cp, d.RouteKey, d.CurrentClient))
	}
	retired := map[string]bool{}
	for _, id := range d.Retired {
		retired[id] = true
	}
	for _, old := range r.Replaced {
		if oc := disc.Clients[old].CounterpartyID; oc != "" && !retired[oc] && oc != d.CurrentClient {
			diffs = append(diffs, fmt.Sprintf("gno.land replaced client %s (counterparty %s) has no migration on Ethereum", old, oc))
		}
	}
	return strings.Join(diffs, "; ")
}
