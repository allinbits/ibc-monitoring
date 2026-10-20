package ethviews

import (
	"context"
	"fmt"
	"math/big"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/model"
)

type limitsView struct {
	Set    bool
	Limits struct {
		MaxPerMessage *big.Int
		DayCap        *big.Int
		RefundCap     *big.Int
		SendMax       *big.Int
		SendMin       *big.Int
	}
	ReceiveLevel *big.Int
	RefundLevel  *big.Int
}

type incident struct {
	Kind        uint8
	Status      uint8
	Client      [32]byte
	SuspectFrom uint64
}

// SendRecord is GnoICS20Transfer.sendRecord(clientId, sequence).
type SendRecord struct {
	Commitment       [32]byte
	Sender           common.Address
	Token            common.Address
	Escrow           common.Address
	TimeoutTimestamp uint64
	SendBlock        uint64
	ProcessedBlock   uint64
	ProcessedIndex   uint64
	Burned           bool
	Outcome          uint8 // 0 pending 1 success 2 error ack 3 timeout
	Payment          uint8 // 0 none 1 paid 2 queued 3 claimed 4 parked 5 rejected
	Amount           *big.Int
	Refunded         *big.Int
	Settlement       uint8
	Open             bool
	Proven           uint8 // 0 none 1 delivered 2 refundable 3 collision
	Held             bool
	ProvenBy         common.Address
	ProvenGen        uint64
}

// RouteDiscovery is what Ethereum itself says about a route: its key, its
// current client at the pin, and its client generations from its own
// history (docs/design.md §4.2). The gno relationship is compared
// separately by the collector.
type RouteDiscovery struct {
	RouteKey      string
	CurrentClient string
	Migrations    []model.EthMigration
	// Retired are the clients the migrations replaced (the current one
	// excluded), oldest first.
	Retired []string
}

// Discover resolves the route of a client id on Ethereum: `routeKey(id)`,
// `currentClient(routeKey)` at the pin, and the GnoRouteMigrated history of
// that route from HistoryFrom to the pin.
func (v *Views) Discover(ctx context.Context, clientID string, b ethrpc.Block) (RouteDiscovery, error) {
	x := v.addrs.Transfer
	var d RouteDiscovery
	var err error
	if d.RouteKey, err = one[string](ctx, v, x, v.xfer, "routeKey", b, clientID); err != nil {
		return d, err
	}
	if d.CurrentClient, err = one[string](ctx, v, x, v.xfer, "currentClient", b, d.RouteKey); err != nil {
		return d, err
	}
	ev, ok := v.xfer.Events["GnoRouteMigrated"]
	if !ok {
		return d, fmt.Errorf("transfer ABI has no GnoRouteMigrated event")
	}
	logs, err := v.rpc.Logs(ctx, x, [][]common.Hash{{ev.ID}}, v.HistoryFrom, b.Number)
	if err != nil {
		return d, err
	}
	seen := map[string]bool{}
	for _, l := range logs {
		vals, err := ev.Inputs.Unpack(l.Data)
		if err != nil || len(vals) != 3 {
			return d, fmt.Errorf("GnoRouteMigrated at block %d: undecodable", l.BlockNumber)
		}
		old, _ := vals[0].(string)
		next, _ := vals[1].(string)
		key, _ := vals[2].(string)
		if key != d.RouteKey {
			continue
		}
		d.Migrations = append(d.Migrations, model.EthMigration{Old: old, New: next, Block: l.BlockNumber})
		if old != d.CurrentClient && !seen[old] {
			seen[old] = true
			d.Retired = append(d.Retired, old)
		}
	}
	return d, nil
}

// Route reads the Ethereum side of a discovered route; tokens are the
// ERC20s to read limits for (the gno denoms' resolved tokens).
func (v *Views) Route(ctx context.Context, d RouteDiscovery, tokens []common.Address, b ethrpc.Block) (model.EthRoute, error) {
	x := v.addrs.Transfer
	clientID, retired := d.CurrentClient, d.Retired
	r := model.EthRoute{RouteKey: d.RouteKey, CurrentClient: clientID, Migrations: d.Migrations, Provenance: v.provenance(b), RouteFlags: []model.EthFlag{}, RefundsFlags: []model.EthFlag{}, Limits: []model.EthLimits{}, Incidents: []model.EthIncident{}, Recon: []model.EthClientRecon{}}
	if r.Migrations == nil {
		r.Migrations = []model.EthMigration{}
	}
	escrow, err := one[common.Address](ctx, v, x, v.xfer, "getEscrow", b, r.RouteKey)
	if err != nil {
		return r, err
	}
	r.Escrow = escrow.Hex()
	bools := []struct {
		dst    *bool
		method string
	}{{&r.Closed, "routeClosed"}, {&r.Reopening, "routeReopening"}, {&r.Paused, "routePaused"}, {&r.RefundsHeld, "refundsHeld"}, {&r.RecoveryBlocked, "recoveryBlocked"}}
	for _, f := range bools {
		if *f.dst, err = one[bool](ctx, v, x, v.xfer, f.method, b, clientID); err != nil {
			return r, err
		}
	}
	keys, err := one[[6]common.Address](ctx, v, x, v.xfer, "guardianKeys", b)
	if err != nil {
		return r, err
	}
	for _, k := range keys {
		r.GuardianKeys = append(r.GuardianKeys, k.Hex())
	}
	out, err := v.call(ctx, x, v.xfer, "routeFlags", b, clientID)
	if err != nil {
		return r, err
	}
	if r.RouteFlags, err = toFlags(out[0], keys); err != nil {
		return r, err
	}
	out, err = v.call(ctx, x, v.xfer, "refundsFlags", b, clientID)
	if err != nil {
		return r, err
	}
	if r.RefundsFlags, err = toFlags(out[0], keys); err != nil {
		return r, err
	}
	out, err = v.call(ctx, x, v.xfer, "routeHold", b, clientID)
	if err != nil {
		return r, err
	}
	if r.RouteHold, err = toHold(out[0]); err != nil {
		return r, err
	}
	out, err = v.call(ctx, x, v.xfer, "refundsHold", b, clientID)
	if err != nil {
		return r, err
	}
	if r.RefundsHold, err = toHold(out[0]); err != nil {
		return r, err
	}
	if r.QuarantineSecs, err = one[uint64](ctx, v, x, v.xfer, "quarantineSeconds", b, clientID); err != nil {
		return r, err
	}
	qe, err := one[*big.Int](ctx, v, x, v.xfer, "quarantineEnd", b, clientID)
	if err != nil {
		return r, err
	}
	if qe.IsUint64() {
		r.QuarantineEnd = qe.Uint64()
	}
	for _, t := range tokens {
		l, err := v.Limits(ctx, clientID, t, b)
		if err != nil {
			return r, err
		}
		r.Limits = append(r.Limits, l)
	}
	out, err = v.call(ctx, x, v.xfer, "incidents", b, clientID)
	if err != nil {
		return r, err
	}
	if len(out) != 3 {
		return r, fmt.Errorf("incidents: %d outputs", len(out))
	}
	incs, ok := gethabi.ConvertType(out[0], new([]incident)).(*[]incident)
	if !ok {
		return r, fmt.Errorf("incidents: unexpected type %T", out[0])
	}
	for _, inc := range *incs {
		r.Incidents = append(r.Incidents, model.EthIncident{Kind: inc.Kind, Status: inc.Status, ClientHash: common.Hash(inc.Client).Hex(), SuspectFrom: inc.SuspectFrom})
	}
	r.ResolvedUpTo, _ = out[1].(uint64)
	r.PendingIncident, _ = out[2].(uint64)
	for _, id := range append([]string{clientID}, retired...) {
		rec, err := v.ClientRecon(ctx, id, b)
		if err != nil {
			return r, err
		}
		r.Recon = append(r.Recon, rec)
	}
	return r, nil
}

// Limits reads limits(clientId, token) with the live bucket levels, plus the
// token's reservation, losses and accepted deficit.
func (v *Views) Limits(ctx context.Context, clientID string, token common.Address, b ethrpc.Block) (model.EthLimits, error) {
	x := v.addrs.Transfer
	l := model.EthLimits{Token: token.Hex()}
	out, err := v.call(ctx, x, v.xfer, "limits", b, clientID, token)
	if err != nil {
		return l, err
	}
	lv, ok := gethabi.ConvertType(out[0], new(limitsView)).(*limitsView)
	if !ok {
		return l, fmt.Errorf("limits: unexpected type %T", out[0])
	}
	l.Set = lv.Set
	l.MaxPerMessage = model.NewAmount(lv.Limits.MaxPerMessage)
	l.DayCap = model.NewAmount(lv.Limits.DayCap)
	l.RefundCap = model.NewAmount(lv.Limits.RefundCap)
	l.SendMax = model.NewAmount(lv.Limits.SendMax)
	l.SendMin = model.NewAmount(lv.Limits.SendMin)
	l.ReceiveLevel = model.NewAmount(lv.ReceiveLevel)
	l.RefundLevel = model.NewAmount(lv.RefundLevel)
	for _, f := range []struct {
		dst    *model.Amount
		method string
	}{{&l.Reserved, "reserved"}, {&l.Losses, "losses"}, {&l.Deficit, "acceptedDeficit"}} {
		val, err := one[*big.Int](ctx, v, x, v.xfer, f.method, b, clientID, token)
		if err != nil {
			return l, err
		}
		*f.dst = model.NewAmount(val)
	}
	return l, nil
}

// ClientRecon reads clientRecon(clientId) and isRetired(clientId).
func (v *Views) ClientRecon(ctx context.Context, clientID string, b ethrpc.Block) (model.EthClientRecon, error) {
	x := v.addrs.Transfer
	rec := model.EthClientRecon{ClientID: clientID}
	out, err := v.call(ctx, x, v.xfer, "clientRecon", b, clientID)
	if err != nil {
		return rec, err
	}
	if len(out) != 5 {
		return rec, fmt.Errorf("clientRecon: %d outputs", len(out))
	}
	rec.Open, _ = out[0].(uint64)
	rec.Incident, _ = out[1].(uint32)
	rec.SuspectStart, _ = out[2].(uint64)
	rec.SuspectOpen, _ = out[3].(uint64)
	rec.Processed, _ = out[4].(uint64)
	if rec.Retired, err = one[bool](ctx, v, x, v.xfer, "isRetired", b, clientID); err != nil {
		return rec, err
	}
	return rec, nil
}

// SendRecord reads the permanently retained send record of (clientId, seq).
func (v *Views) SendRecord(ctx context.Context, clientID string, seq uint64, b ethrpc.Block) (SendRecord, error) {
	out, err := v.call(ctx, v.addrs.Transfer, v.xfer, "sendRecord", b, clientID, seq)
	if err != nil {
		return SendRecord{}, err
	}
	rec, ok := gethabi.ConvertType(out[0], new(SendRecord)).(*SendRecord)
	if !ok {
		return SendRecord{}, fmt.Errorf("sendRecord: unexpected type %T", out[0])
	}
	return *rec, nil
}

// VoucherSupply reads an ERC20's totalSupply at the pinned block.
func (v *Views) VoucherSupply(ctx context.Context, token common.Address, b ethrpc.Block) (model.Amount, error) {
	s, err := one[*big.Int](ctx, v, token, v.erc20, "totalSupply", b)
	if err != nil {
		return model.Amount{}, err
	}
	return model.NewAmount(s), nil
}

// BalanceOf reads an ERC20 balance at the pinned block.
func (v *Views) BalanceOf(ctx context.Context, token, owner common.Address, b ethrpc.Block) (model.Amount, error) {
	s, err := one[*big.Int](ctx, v, token, v.erc20, "balanceOf", b, owner)
	if err != nil {
		return model.Amount{}, err
	}
	return model.NewAmount(s), nil
}

// GnoBusy reads the transfer app's reentrancy lock view (repair-critical).
func (v *Views) GnoBusy(ctx context.Context, b ethrpc.Block) (bool, error) {
	return one[bool](ctx, v, v.addrs.Transfer, v.xfer, "gnoBusy", b)
}

// Amounts of a send record as model values.
func (s SendRecord) Amounts() (amount, refunded model.Amount) {
	return amountOf(s.Amount), amountOf(s.Refunded)
}
