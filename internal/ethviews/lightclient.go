package ethviews

import (
	"context"
	"fmt"
	"time"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/model"
)

// clientState is IGnoLightClientMsgs.ClientState, abi.encode'd by
// getClientState(): there is no latestHeight() or trustingPeriod() getter.
type clientState struct {
	ChainId        string
	RevisionNumber uint64
	LatestHeight   uint64
	TrustLevel     struct {
		Numerator   uint64
		Denominator uint64
	}
	TrustingPeriod uint64
	MaxClockDrift  uint64
	IsFrozen       bool
}

var clientStateArgs gethabi.Arguments

func init() {
	t, err := gethabi.NewType("tuple", "ClientState", []gethabi.ArgumentMarshaling{
		{Name: "chainId", Type: "string"},
		{Name: "revisionNumber", Type: "uint64"},
		{Name: "latestHeight", Type: "uint64"},
		{Name: "trustLevel", Type: "tuple", Components: []gethabi.ArgumentMarshaling{
			{Name: "numerator", Type: "uint64"},
			{Name: "denominator", Type: "uint64"},
		}},
		{Name: "trustingPeriod", Type: "uint64"},
		{Name: "maxClockDrift", Type: "uint64"},
		{Name: "isFrozen", Type: "bool"},
	})
	if err != nil {
		panic(err)
	}
	clientStateArgs = gethabi.Arguments{{Type: t}}
}

// EncodeClientState packs a ClientState as getClientState() returns it
// (used by tests and fixtures).
func EncodeClientState(chainID string, revision, height, trustNum, trustDen, trustingPeriod, maxClockDrift uint64, frozen bool) ([]byte, error) {
	cs := clientState{ChainId: chainID, RevisionNumber: revision, LatestHeight: height, TrustingPeriod: trustingPeriod, MaxClockDrift: maxClockDrift, IsFrozen: frozen}
	cs.TrustLevel.Numerator = trustNum
	cs.TrustLevel.Denominator = trustDen
	return clientStateArgs.Pack(cs)
}

func decodeClientState(b []byte) (clientState, error) {
	out, err := clientStateArgs.Unpack(b)
	if err != nil {
		return clientState{}, fmt.Errorf("client state: %w", err)
	}
	cs, ok := gethabi.ConvertType(out[0], new(clientState)).(*clientState)
	if !ok {
		return clientState{}, fmt.Errorf("client state: unexpected type %T", out[0])
	}
	return *cs, nil
}

type consensusState struct {
	Timestamp          uint64
	AppHash            [32]byte
	NextValidatorsHash [32]byte
}

// LightClient reads a GnoLightClient (the route's, an enrolled target or a
// recovery candidate) with its derived expiry. routerID is the router client
// id it serves ("" for a candidate); role labels it for the dashboard.
func (v *Views) LightClient(ctx context.Context, addr common.Address, routerID, role string, b ethrpc.Block) (model.Client, error) {
	c := model.Client{
		Chain:      model.ChainEthereum,
		ID:         routerID,
		Address:    addr.Hex(),
		Kind:       model.ClientKindGnoLightClient,
		Role:       role,
		Provenance: v.provenance(b),
	}
	raw, err := one[[]byte](ctx, v, addr, v.lc, "getClientState", b)
	if err != nil {
		// A FrozenLightClient (a retired id) reverts here and answers isFrozen.
		if frozen, ferr := one[bool](ctx, v, addr, v.lc, "isFrozen", b); ferr == nil && frozen {
			c.Status = model.ClientFrozen
			c.Frozen = true
			return c, nil
		}
		return c, fmt.Errorf("light client %s: %w", addr.Hex(), err)
	}
	cs, err := decodeClientState(raw)
	if err != nil {
		return c, fmt.Errorf("light client %s: %w", addr.Hex(), err)
	}
	c.ChainID = cs.ChainId
	c.LatestHeight = cs.LatestHeight
	c.LatestRevision = cs.RevisionNumber
	c.TrustingPeriod = time.Duration(cs.TrustingPeriod) * time.Second
	c.Frozen = cs.IsFrozen

	out, err := v.call(ctx, addr, v.lc, "getConsensusState", b, cs.LatestHeight)
	if err != nil {
		return c, fmt.Errorf("light client %s: %w", addr.Hex(), err)
	}
	st, ok := gethabi.ConvertType(out[0], new(consensusState)).(*consensusState)
	if !ok {
		return c, fmt.Errorf("light client %s: consensus state type %T", addr.Hex(), out[0])
	}
	// The timestamp is in nanoseconds.
	c.HeadTime = time.Unix(0, int64(st.Timestamp)).UTC()
	c.Expiry = c.HeadTime.Add(c.TrustingPeriod)

	frozen, err := one[bool](ctx, v, addr, v.lc, "isFrozen", b)
	if err != nil {
		return c, err
	}
	expired, err := one[bool](ctx, v, addr, v.lc, "isExpired", b)
	if err != nil {
		return c, err
	}
	switch {
	case frozen:
		c.Status = model.ClientFrozen
		c.Frozen = true
	case expired:
		c.Status = model.ClientExpired
	default:
		c.Status = model.ClientActive
	}
	return c, nil
}

// Transition is the light client's last live validator-set transition.
type Transition struct {
	ID   uint64
	Time uint64
}

// LastTransition reads lastTransition().
func (v *Views) LastTransition(ctx context.Context, addr common.Address, b ethrpc.Block) (Transition, error) {
	out, err := v.call(ctx, addr, v.lc, "lastTransition", b)
	if err != nil {
		return Transition{}, err
	}
	if len(out) != 2 {
		return Transition{}, fmt.Errorf("lastTransition: %d outputs", len(out))
	}
	id, _ := out[0].(uint64)
	t, _ := out[1].(uint64)
	return Transition{ID: id, Time: t}, nil
}
