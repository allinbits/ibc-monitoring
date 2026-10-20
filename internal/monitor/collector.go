package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

	collection uint64
	gnoState   ChainState
	ethState   ChainState
}

// ChainState is one chain's collection readiness (docs/design.md §7.11).
type ChainState struct {
	// OK is true when the latest collection read the chain without error.
	OK bool `json:"ok"`
	// Verified is true when the latest collection's header was agreed (or
	// light-client verified); an unverified collection is not fresh.
	Verified bool `json:"verified"`
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
	GnoState      ChainState         `json:"gno_state"`
	Eth           ethrpc.Block       `json:"eth"`
	EthVerified   model.Verification `json:"eth_verification"`
	EthCrossCheck string             `json:"eth_cross_check,omitempty"`
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
func (p Pins) Fresh(chain model.Chain, prov model.Provenance) bool {
	st := p.State(chain)
	return st.OK && st.Verified && prov.Collection == st.LastCollection
}

// State returns one chain's state.
func (p Pins) State(chain model.Chain) ChainState {
	if chain == model.ChainEthereum {
		return p.EthState
	}
	return p.GnoState
}

// Collect reads every view once. The two chains are collected concurrently,
// each under its own deadline, so a failure or a stall on one chain neither
// prevents nor delays the other chain's snapshots; both states are reported
// in the pins snapshot. A collection that runs past Timeout is a failed
// collection for that chain.
func (c *Collector) Collect(ctx context.Context) Pins {
	c.collection++
	pins := Pins{Collection: c.collection, CollectedAt: time.Now()}
	var wg sync.WaitGroup
	run := func(chain string, collect func(context.Context) error, state *ChainState, verifiedOf func() model.Verification) {
		defer wg.Done()
		cctx, cancel := ctx, context.CancelFunc(func() {})
		if c.Timeout > 0 {
			cctx, cancel = context.WithTimeout(ctx, c.Timeout)
		}
		defer cancel()
		err := collect(cctx)
		if err != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			err = fmt.Errorf("collection timed out after %s: %w", c.Timeout, err)
		}
		*state = state.update(err, verified(verifiedOf()), c.collection, pins.CollectedAt)
		if err != nil {
			c.log().Error(chain+" collection failed", "err", state.Error)
		}
	}
	if c.Gno != nil {
		wg.Add(1)
		go run("gno", func(cctx context.Context) error { return c.collectGno(cctx, &pins) }, &c.gnoState, func() model.Verification { return pins.GnoVerified })
	}
	if c.Eth != nil {
		wg.Add(1)
		go run("ethereum", func(cctx context.Context) error { return c.collectEth(cctx, &pins) }, &c.ethState, func() model.Verification { return pins.EthVerified })
	}
	wg.Wait()
	pins.GnoState = c.gnoState
	pins.GnoError = c.gnoState.Error
	pins.EthState = c.ethState
	pins.EthError = c.ethState.Error
	c.Store.PutSnapshot(store.Snapshot{Kind: store.KindPins, Key: "latest", Value: pins})
	return pins
}

func verified(v model.Verification) bool {
	return v == model.VerificationCrossChecked || v == model.VerificationLightClient
}

func (s ChainState) update(err error, verified bool, collection uint64, at time.Time) ChainState {
	if err != nil {
		s.OK = false
		s.Verified = false
		s.Error = redact.String(err.Error())
		s.ConsecutiveFailures++
		return s
	}
	if !verified {
		// Read, but the header did not agree: the snapshots are not fresh.
		s.OK = true
		s.Verified = false
		s.Error = ""
		s.ConsecutiveFailures = 0
		s.ConsecutiveUnverified++
		return s
	}
	return ChainState{OK: true, Verified: true, LastSuccess: at, LastCollection: collection}
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

func (c *Collector) collectGno(ctx context.Context, pins *Pins) error {
	pin, err := c.Gno.LatestPin(ctx)
	if err != nil {
		return err
	}
	pins.Gno = pin
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
	}
	pins.GnoVerified, pins.GnoCrossCheck = c.verification(model.VerificationProvider, c.Gno2 != nil, otherErr, agree, mismatch)

	stamp := func(p *model.Provenance) {
		p.Verification = pins.GnoVerified
		p.Collection = c.collection
		p.StateVerification = model.StateNone
	}
	clients, err := c.Gno.Clients(ctx, pin)
	if err != nil {
		return err
	}
	for _, cl := range clients {
		stamp(&cl.Provenance)
		c.Store.PutSnapshot(store.Snapshot{Kind: store.KindGnoClients, Key: cl.ID, Value: cl, Provenance: cl.Provenance})
	}
	routes, err := c.Gno.Routes(ctx, pin)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(routes))
	for _, r := range routes {
		stamp(&r.Provenance)
		c.Store.PutSnapshot(store.Snapshot{Kind: store.KindGnoRoutes, Key: r.Key, Value: r, Provenance: r.Provenance})
		keys = append(keys, r.Key)
	}
	gov, err := c.Gno.Governance(ctx, keys, pin)
	if err != nil {
		return err
	}
	stamp(&gov.Provenance)
	c.Store.PutSnapshot(store.Snapshot{Kind: store.KindGnoGovernance, Key: "ibcgov", Value: gov, Provenance: gov.Provenance})
	return nil
}

func (c *Collector) collectEth(ctx context.Context, pins *Pins) error {
	b, err := c.Eth.Pinned(ctx)
	if err != nil {
		return err
	}
	pins.Eth = b
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
		if otherErr == nil && agree && b.Finality == "finalized" {
			var head ethrpc.Block
			head, otherErr = c.Eth2.Pinned(ctx)
			if otherErr == nil && head.Number < b.Number {
				agree = false
				mismatch = fmt.Sprintf("block %d is not finalized by the second provider (its finalized head is %d)", b.Number, head.Number)
			}
		}
	}
	pins.EthVerified, pins.EthCrossCheck = c.verification(primary, c.Eth2 != nil, otherErr, agree, mismatch)
	if agree && c.Eth2 != nil && otherErr == nil && b.Finality == "finalized" {
		pins.EthCrossCheck = "agreed; finalized by the second provider"
		pins.EthFinalityIndependent = true
	}

	stamp := func(p *model.Provenance) {
		p.Verification = pins.EthVerified
		p.Collection = c.collection
		p.StateVerification = model.StateNone
	}
	addrs := c.Eth.Addresses()
	gov, err := c.Eth.Governance(ctx, b)
	if err != nil {
		return err
	}
	stamp(&gov.Provenance)
	c.Store.PutSnapshot(store.Snapshot{Kind: store.KindEthGovernance, Key: "ibcadmin", Value: gov, Provenance: gov.Provenance})

	// TODO(phase 2): resolve the route's verifier from ICS02ClientAdded /
	// ICS02ClientMigrated logs instead of the configured address.
	if addrs.LightClient != (common.Address{}) {
		lc, err := c.Eth.LightClient(ctx, addrs.LightClient, gov.GovClient, "route", b)
		if err != nil {
			return err
		}
		stamp(&lc.Provenance)
		c.Store.PutSnapshot(store.Snapshot{Kind: store.KindEthClients, Key: lc.Address, Value: lc, Provenance: lc.Provenance})
	}

	// One Ethereum route per gno route: its current client id is the gno
	// client's counterparty; its retired ids the replaced clients'. The
	// tokens are each denom's resolved Ethereum token (remote or trace base).
	gnoClients := map[string]model.Client{}
	for _, s := range c.Store.Snapshots(store.KindGnoClients) {
		if cl, ok := s.Value.(model.Client); ok {
			gnoClients[cl.ID] = cl
		}
	}
	for _, s := range c.Store.Snapshots(store.KindGnoRoutes) {
		r, ok := s.Value.(model.Route)
		if !ok {
			continue
		}
		cur, ok := gnoClients[r.Client]
		if !ok || cur.CounterpartyID == "" {
			continue
		}
		var retired []string
		for _, old := range r.Replaced {
			if oc, ok := gnoClients[old]; ok && oc.CounterpartyID != "" {
				retired = append(retired, oc.CounterpartyID)
			}
		}
		var tokens []common.Address
		seen := map[common.Address]bool{}
		for _, d := range r.Denoms {
			if !common.IsHexAddress(d.Token) {
				continue
			}
			a := common.HexToAddress(d.Token)
			if !seen[a] {
				seen[a] = true
				tokens = append(tokens, a)
			}
		}
		er, err := c.Eth.Route(ctx, cur.CounterpartyID, retired, tokens, b)
		if err != nil {
			return fmt.Errorf("ethereum route %s: %w", cur.CounterpartyID, err)
		}
		stamp(&er.Provenance)
		c.Store.PutSnapshot(store.Snapshot{Kind: store.KindEthRoutes, Key: r.Key, Value: er, Provenance: er.Provenance})
	}
	return nil
}
