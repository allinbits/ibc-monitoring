package model

import "time"

// Meter is a leaky bucket as the realm stores it (level at the last refill)
// and, when computed, its live level (docs/design.md §4.1, Reader.Meters).
type Meter struct {
	Level Amount `json:"level"`
	// At is the unix time of the stored level.
	At    int64 `json:"at"`
	Epoch int64 `json:"epoch,omitempty"`
	// Live is the level refilled to the snapshot time, present only when an
	// exact source gave it (the realm's Reader.Meters); LiveSource is then
	// "reader". Otherwise Live is nil, LiveSource is "unavailable" and
	// LiveReason says why: Meter.Refill depends on the payouts-epoch reset,
	// the fractional remainder and the stored rate and ceiling, none of
	// which the JSON shows, so it is never recomputed here (design §4.1).
	Live       *Amount `json:"live,omitempty"`
	LiveSource string  `json:"live_source"`
	LiveReason string  `json:"live_reason,omitempty"`
}

// Totals mirrors the controls' per-denom totals.
type Totals struct {
	Credited      Amount `json:"credited"`
	Owed          Amount `json:"owed"`
	Withheld      Amount `json:"withheld"`
	Swept         Amount `json:"swept"`
	Paid          Amount `json:"paid"`
	Fees          Amount `json:"fees"`
	SweepPaid     Amount `json:"sweep_paid"`
	Outstanding   Amount `json:"outstanding"`
	Returning     Amount `json:"returning"`
	ProvenOwed    Amount `json:"proven_owed"`
	InFlight      Amount `json:"in_flight"`
	Sent          Amount `json:"sent"`
	SendFees      Amount `json:"send_fees"`
	Refunded      Amount `json:"refunded"`
	QueuedRefunds Amount `json:"queued_refunds"`
	Lost          Amount `json:"lost"`
	Recapitalized Amount `json:"recapitalized"`
	Reduced       Amount `json:"reduced"`
	Accepted      Amount `json:"accepted"`
	Unrecovered   Amount `json:"unrecovered"`
	Unresolved    Amount `json:"unresolved"`
}

// Denom is one denom enabled on a gno.land route.
type Denom struct {
	Denom          string `json:"denom"`
	Kind           string `json:"kind"` // "unescrow" | "mint"
	CapFloor       Amount `json:"cap_floor"`
	LaunchCapFloor Amount `json:"launch_cap_floor"`
	SupplyCap      Amount `json:"supply_cap"`
	Fee            Amount `json:"fee"`
	FeeFloor       Amount `json:"fee_floor"`
	SendMax        Amount `json:"send_max"`
	SendMin        Amount `json:"send_min"`
	RefundCap      Amount `json:"refund_cap"`
	Remote         string `json:"remote"`
	// Decimals is the governed decimals of the denom's voucher on gno.land
	// (fixed when the voucher is first minted); 0 for an unescrowed denom.
	Decimals int `json:"decimals"`
	// Token is the denom's ERC20 on Ethereum: Remote for an unescrow denom;
	// for a minted denom the base of its denom trace (denoms/ibc/<hash>),
	// since Remote is empty there by the realm's rule (design §4.1).
	Token string `json:"token"`
	// Trace is the minted denom's trace path (transfer/<client>), checked to
	// name a client of the route; empty for unescrow.
	Trace string `json:"trace,omitempty"`
	// AwaitingFirstReceive marks a minted denom configured on the route
	// that has never been received: no trace, no voucher, so no Token yet.
	AwaitingFirstReceive bool   `json:"awaiting_first_receive,omitempty"`
	Meter                Meter  `json:"meter"`
	RefundMeter          Meter  `json:"refund_meter"`
	Waiting              int64  `json:"waiting"`
	QueuedRefunds        int64  `json:"queued_refunds"`
	Totals               Totals `json:"totals"`
	// Lock is the realm's escrow accounting of the denom for this route
	// (total_escrow), exact; 0 for mint.
	Lock *Amount `json:"lock,omitempty"`
}

// Route is a gno.land Ethereum route as the transfer realm reports it.
type Route struct {
	Key                 string     `json:"route"`
	Client              string     `json:"client"`
	Replaced            []string   `json:"replaced"`
	Enrolled            time.Time  `json:"enrolled"`
	Activated           uint64     `json:"activated"`
	Closed              bool       `json:"closed"`
	LossClosed          bool       `json:"loss_closed"`
	FeeCollector        string     `json:"fee_collector"`
	RefusedRecipients   []string   `json:"refused_recipients"`
	ProtectedRecipients []string   `json:"protected_recipients"`
	Unrecovered         Amount     `json:"unrecovered"`
	Unresolved          Amount     `json:"unresolved"`
	Denoms              []Denom    `json:"denoms"`
	Provenance          Provenance `json:"provenance"`
}

// EthFlag is a standing flag on Ethereum (zero values: not standing).
type EthFlag struct {
	Seat        int    `json:"seat"`
	Key         string `json:"key"`
	BlockNumber uint64 `json:"block_number"`
	Time        int64  `json:"time"`
}

// EthHold is a hold, or a hold-history interval, on Ethereum.
type EthHold struct {
	Start  int64 `json:"start"`
	End    int64 `json:"end,omitempty"`
	Merged bool  `json:"merged,omitempty"`
}

// EthLimits is a token's limits on an Ethereum route with the live bucket
// levels (docs/design.md §4.2, limits()).
type EthLimits struct {
	Token         string `json:"token"`
	Set           bool   `json:"set"`
	MaxPerMessage Amount `json:"max_per_message"`
	DayCap        Amount `json:"day_cap"`
	RefundCap     Amount `json:"refund_cap"`
	SendMax       Amount `json:"send_max"`
	SendMin       Amount `json:"send_min"`
	ReceiveLevel  Amount `json:"receive_level"`
	RefundLevel   Amount `json:"refund_level"`
	Reserved      Amount `json:"reserved"`
	Losses        Amount `json:"losses"`
	Deficit       Amount `json:"accepted_deficit"`
}

// EthIncident is one entry of the transfer app's incidents(clientId).
type EthIncident struct {
	Kind        uint8  `json:"kind"`   // 1 replacement, 2 late loss
	Status      uint8  `json:"status"` // 1 open, 2 reopening, 3 resolved
	ClientHash  string `json:"client_hash"`
	SuspectFrom uint64 `json:"suspect_from"`
}

// EthClientRecon is clientRecon(clientId).
type EthClientRecon struct {
	ClientID     string `json:"client_id"`
	Open         uint64 `json:"open"`
	Incident     uint32 `json:"incident"`
	SuspectStart uint64 `json:"suspect_start"`
	SuspectOpen  uint64 `json:"suspect_open"`
	Processed    uint64 `json:"processed"`
	Retired      bool   `json:"retired"`
}

// EthRoute is the Ethereum side of a route, keyed by the current client id.
type EthRoute struct {
	RouteKey        string           `json:"route_key"`
	CurrentClient   string           `json:"current_client"`
	Escrow          string           `json:"escrow"`
	Closed          bool             `json:"closed"`
	Reopening       bool             `json:"reopening"`
	Paused          bool             `json:"paused"`
	RefundsHeld     bool             `json:"refunds_held"`
	RecoveryBlocked bool             `json:"recovery_blocked"`
	RouteFlags      []EthFlag        `json:"route_flags"`
	RefundsFlags    []EthFlag        `json:"refunds_flags"`
	RouteHold       *EthHold         `json:"route_hold,omitempty"`
	RefundsHold     *EthHold         `json:"refunds_hold,omitempty"`
	GuardianKeys    []string         `json:"guardian_keys"`
	QuarantineSecs  uint64           `json:"quarantine_seconds"`
	QuarantineEnd   uint64           `json:"quarantine_end"`
	Limits          []EthLimits      `json:"limits"`
	Incidents       []EthIncident    `json:"incidents"`
	ResolvedUpTo    uint64           `json:"resolved_up_to"`
	PendingIncident uint64           `json:"pending_incidents"`
	Recon           []EthClientRecon `json:"client_recon"`
	// Migrations are the route's client generations from Ethereum's own
	// history (GnoRouteMigrated), oldest first.
	Migrations []EthMigration `json:"migrations"`
	// GnoRoute is the gno.land route matched to this one and GnoCounterparty
	// the client gno.land's route client names as its counterparty; the
	// relationship is a separate cross-chain comparison (design §4.2) and
	// RelationshipMismatch says how it fails. GnoDiscovery is the gno
	// collection those inputs came from (0: none); Incomplete names what the
	// snapshot lacks without it.
	GnoRoute             string   `json:"gno_route,omitempty"`
	GnoCounterparty      string   `json:"gno_counterparty,omitempty"`
	RelationshipMismatch string   `json:"relationship_mismatch,omitempty"`
	GnoDiscovery         uint64   `json:"gno_discovery"`
	Incomplete           []string `json:"incomplete,omitempty"`
	// GnoProvenance is the gno discovery's provenance (nil without one):
	// the cross-chain inputs carry their own.
	GnoProvenance *Provenance `json:"gno_provenance,omitempty"`
	Provenance    Provenance  `json:"provenance"`
}

// EthMigration is one GnoRouteMigrated{old, new, routeKey} of a route.
type EthMigration struct {
	Old   string `json:"old"`
	New   string `json:"new"`
	Block uint64 `json:"block"`
}
