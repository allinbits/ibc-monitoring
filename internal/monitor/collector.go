package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethviews"
	"github.com/allinbits/ibc-monitoring/internal/gnoviews"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// Collector reads both chains into the store. Each collection pins one
// height per chain, cross-checks its hash against the second provider and
// stamps every snapshot with that verification (docs/design.md §3.2).
type Collector struct {
	Gno  *gnoviews.Views
	Gno2 *gnoviews.Views // optional second provider for the hash cross-check
	Eth  *ethviews.Views
	Eth2 *ethrpc.Client // optional second provider
	// EthVerification is the primary endpoint's level: "provider" or
	// "light-client" when it is a Helios endpoint.
	EthVerification model.Verification
	Store           store.Store
	Log             *slog.Logger
}

// Pins are the heights a collection was read at.
type Pins struct {
	Gno           gnoviews.Pin       `json:"gno"`
	GnoVerified   model.Verification `json:"gno_verification"`
	Eth           ethrpc.Block       `json:"eth"`
	EthVerified   model.Verification `json:"eth_verification"`
	CollectedAt   time.Time          `json:"collected_at"`
	GnoError      string             `json:"gno_error,omitempty"`
	EthError      string             `json:"eth_error,omitempty"`
	GnoCrossCheck string             `json:"gno_cross_check,omitempty"`
	EthCrossCheck string             `json:"eth_cross_check,omitempty"`
}

// Collect reads every view once. A failure on one chain does not prevent the
// other chain's snapshots; both errors are reported in the pins snapshot.
func (c *Collector) Collect(ctx context.Context) Pins {
	pins := Pins{CollectedAt: time.Now()}
	if c.Gno != nil {
		if err := c.collectGno(ctx, &pins); err != nil {
			pins.GnoError = err.Error()
			c.log().Error("gno collection failed", "err", err)
		}
	}
	if c.Eth != nil {
		if err := c.collectEth(ctx, &pins); err != nil {
			pins.EthError = err.Error()
			c.log().Error("ethereum collection failed", "err", err)
		}
	}
	c.Store.PutSnapshot(store.Snapshot{Kind: store.KindPins, Key: "latest", Value: pins})
	return pins
}

func (c *Collector) log() *slog.Logger {
	if c.Log == nil {
		return slog.Default()
	}
	return c.Log
}

func (c *Collector) collectGno(ctx context.Context, pins *Pins) error {
	pin, err := c.Gno.LatestPin(ctx)
	if err != nil {
		return err
	}
	pins.Gno = pin
	pins.GnoVerified = model.VerificationProvider
	if c.Gno2 != nil {
		other, err := c.Gno2.PinAt(ctx, pin.Height)
		switch {
		case err != nil:
			pins.GnoCrossCheck = "second provider unavailable: " + err.Error()
		case !strings.EqualFold(other.BlockHash, pin.BlockHash):
			pins.GnoVerified = model.VerificationUnverified
			pins.GnoCrossCheck = fmt.Sprintf("hash mismatch at %d: %s vs %s", pin.Height, pin.BlockHash, other.BlockHash)
		default:
			pins.GnoVerified = model.VerificationCrossChecked
			pins.GnoCrossCheck = "agreed"
		}
	}
	clients, err := c.Gno.Clients(ctx, pin)
	if err != nil {
		return err
	}
	for _, cl := range clients {
		cl.Provenance.Verification = pins.GnoVerified
		c.Store.PutSnapshot(store.Snapshot{Kind: store.KindGnoClients, Key: cl.ID, Value: cl, Provenance: cl.Provenance})
	}
	routes, err := c.Gno.Routes(ctx, pin)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(routes))
	for _, r := range routes {
		r.Provenance.Verification = pins.GnoVerified
		c.Store.PutSnapshot(store.Snapshot{Kind: store.KindGnoRoutes, Key: r.Key, Value: r, Provenance: r.Provenance})
		keys = append(keys, r.Key)
	}
	gov, err := c.Gno.Governance(ctx, keys, pin)
	if err != nil {
		return err
	}
	gov.Provenance.Verification = pins.GnoVerified
	c.Store.PutSnapshot(store.Snapshot{Kind: store.KindGnoGovernance, Key: "ibcgov", Value: gov, Provenance: gov.Provenance})
	return nil
}

func (c *Collector) collectEth(ctx context.Context, pins *Pins) error {
	b, err := c.Eth.Pinned(ctx)
	if err != nil {
		return err
	}
	pins.Eth = b
	pins.EthVerified = c.EthVerification
	if pins.EthVerified == "" {
		pins.EthVerified = model.VerificationProvider
	}
	if c.Eth2 != nil {
		other, err := c.Eth2.BlockByNumber(ctx, b.Number)
		switch {
		case err != nil:
			pins.EthCrossCheck = "second provider unavailable: " + err.Error()
		case other.Hash != b.Hash:
			pins.EthVerified = model.VerificationUnverified
			pins.EthCrossCheck = fmt.Sprintf("hash mismatch at %d: %s vs %s", b.Number, b.Hash, other.Hash)
		default:
			if pins.EthVerified == model.VerificationProvider {
				pins.EthVerified = model.VerificationCrossChecked
			}
			pins.EthCrossCheck = "agreed"
		}
	}
	addrs := c.Eth.Addresses()
	gov, err := c.Eth.Governance(ctx, b)
	if err != nil {
		return err
	}
	gov.Provenance.Verification = pins.EthVerified
	c.Store.PutSnapshot(store.Snapshot{Kind: store.KindEthGovernance, Key: "ibcadmin", Value: gov, Provenance: gov.Provenance})

	// The route's light client is the launch client, or whatever the route
	// moved to: read the current client's verifier through the router? The
	// router has no verifier getter per id in this layer yet, so the
	// configured light-client address is read (TODO phase 2: resolve from
	// ICS02ClientAdded/ICS02ClientMigrated logs).
	if addrs.LightClient != (common.Address{}) {
		lc, err := c.Eth.LightClient(ctx, addrs.LightClient, gov.GovClient, "route", b)
		if err != nil {
			return err
		}
		lc.Provenance.Verification = pins.EthVerified
		c.Store.PutSnapshot(store.Snapshot{Kind: store.KindEthClients, Key: lc.Address, Value: lc, Provenance: lc.Provenance})
	}

	// One Ethereum route per gno route: its current client id is the gno
	// client's counterparty; its retired ids the replaced clients'.
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
		for _, d := range r.Denoms {
			if common.IsHexAddress(d.Remote) {
				tokens = append(tokens, common.HexToAddress(d.Remote))
			}
		}
		er, err := c.Eth.Route(ctx, cur.CounterpartyID, retired, tokens, b)
		if err != nil {
			return fmt.Errorf("ethereum route %s: %w", cur.CounterpartyID, err)
		}
		er.Provenance.Verification = pins.EthVerified
		c.Store.PutSnapshot(store.Snapshot{Kind: store.KindEthRoutes, Key: r.Key, Value: er, Provenance: er.Provenance})
	}
	return nil
}
