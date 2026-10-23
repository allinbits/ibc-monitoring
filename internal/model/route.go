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
	// DisplayDecimals is the precision every amount of this denom is shown
	// in: the governed voucher decimals for a minted denom, the native
	// denom's precision for an unescrowed one (ugnot: 6, one GNOT being
	// 10^6 ugnot), and 0 (base units) for a native denom of unknown
	// precision. The config's decimals of an unescrowed denom is 0 and
	// never a display precision.
	DisplayDecimals int `json:"display_decimals"`
	// DayCap is the bridge meter's ceiling and daily refill, the realm's
	// Denom.DayCap(lock): the cap floor for a minted denom, max(cap floor,
	// lock/10) for an unescrowed one. Exact; derived from the config and
	// the lock read at the same height.
	DayCap Amount `json:"day_cap"`
	// Supply is the gno voucher's total supply of a minted denom
	// (voucher/<denom>, exact); nil for an unescrowed denom or a minted one
	// awaiting its first receive.
	Supply *Amount `json:"supply,omitempty"`
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
	Key                 string    `json:"route"`
	Client              string    `json:"client"`
	Replaced            []string  `json:"replaced"`
	Enrolled            time.Time `json:"enrolled"`
	Activated           uint64    `json:"activated"`
	Closed              bool      `json:"closed"`
	LossClosed          bool      `json:"loss_closed"`
	FeeCollector        string    `json:"fee_collector"`
	RefusedRecipients   []string  `json:"refused_recipients"`
	ProtectedRecipients []string  `json:"protected_recipients"`
	// Unrecovered and Unresolved are the realm's route-wide sums of the
	// denoms' totals in base units: a sum across denoms of different
	// precisions, meaningful as zero or not; the per-denom totals carry
	// the amounts.
	Unrecovered Amount  `json:"unrecovered"`
	Unresolved  Amount  `json:"unresolved"`
	Denoms      []Denom `json:"denoms"`
	// The route's records, every page (routes/<route>/{claims,sweeps,
	// sends,losses}), in the realm's storage-key order.
	Claims     []Claim    `json:"claims"`
	Sweeps     []Sweep    `json:"sweeps"`
	Sends      []Send     `json:"sends"`
	Losses     []Loss     `json:"losses"`
	Provenance Provenance `json:"provenance"`
}

// Claim is a receive credited on gno.land and paid out through the bridge
// meter (routes/<route>/claims). Amount already has reductions subtracted;
// the remainder is Amount − Paid.
type Claim struct {
	Key     string `json:"key"`
	Denom   string `json:"denom"`
	To      string `json:"to"`
	Amount  Amount `json:"amount"`
	Paid    Amount `json:"paid"`
	Reduced Amount `json:"reduced"`
	Fee     Amount `json:"fee"`
	State   string `json:"state"` // open | withheld | forged | done
	By      string `json:"by"`    // packet | vote
	At      int64  `json:"at"`    // unix seconds
	Height  int64  `json:"height"`
	Seq     int64  `json:"seq"`
}

// Sweep is a sweep entry (routes/<route>/sweeps): an amount moved out of the
// claim queue to be paid by decision.
type Sweep struct {
	Key     string `json:"key"`
	Denom   string `json:"denom"`
	To      string `json:"to"`
	Amount  Amount `json:"amount"`
	Reduced Amount `json:"reduced"`
	// Reason is the realm's reason text ("reason" in the realm's JSON; the
	// API's public filter blanks every "reason" key).
	Reason    string  `json:"sweep_reason"`
	Height    int64   `json:"height"`
	Voided    bool    `json:"voided"`
	Paid      bool    `json:"paid"`
	PaidTo    string  `json:"paid_to"`
	FromClaim bool    `json:"from_claim,omitempty"`
	ClaimPaid *Amount `json:"claim_paid,omitempty"`
}

// Send is a gno.land send record (routes/<route>/sends) without its payload
// bytes: what the transfer sent and what its acknowledgement, timeout or
// settlement attached. CommitmentOK says whether the stored commitment is
// the commitment of the stored packet.
type Send struct {
	Key               string `json:"key"`
	Sequence          Seq    `json:"sequence"`
	SourceClient      string `json:"source_client"`
	DestinationClient string `json:"destination_client"`
	// TimeoutTimestamp is the packet's timeout, unix seconds.
	TimeoutTimestamp uint64 `json:"timeout_timestamp"`
	CommitmentOK     bool   `json:"commitment_ok"`
	Sender           string `json:"sender"`
	Receiver         string `json:"receiver"`
	Denom            string `json:"denom"`
	Kind             string `json:"kind"`
	Amount           Amount `json:"amount"`
	Fee              Amount `json:"fee"`
	SentHeight       int64  `json:"sent_height"`
	SentAt           int64  `json:"sent_at"`
	// Outcome: "" (pending), success, error (error acknowledgement), timeout.
	Outcome         string `json:"outcome"`
	ProcessedHeight int64  `json:"processed_height"`
	ProcessedAt     int64  `json:"processed_at"`
	// Payment of the refund an error or a timeout owes: "", paid, queued,
	// claimed, rejected, reconciled.
	Payment     string `json:"payment"`
	Refunded    Amount `json:"refunded"`
	Settlement  int64  `json:"settlement"`
	Reservation Amount `json:"reservation"`
	Proven      int64  `json:"proven"`
	ProvenAt    uint64 `json:"proven_at"`
	ProvenBy    string `json:"proven_by"`
	ProvenGen   uint64 `json:"proven_gen"`
	Incident    string `json:"incident"`
}

// Loss is an entry of the route's loss ledger (routes/<route>/losses).
type Loss struct {
	Key       string `json:"key"`
	Direction string `json:"direction"`
	Denom     string `json:"denom"`
	Amount    Amount `json:"amount"`
	Chain     string `json:"chain"`
	Incident  string `json:"incident"`
	Cause     string `json:"cause"`
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
	// ReceiveLevel and RefundLevel are leaky-bucket levels of what was
	// USED: they drain at the cap per day (LeakyBucket.level), unlike a gno
	// meter's level, which is what is still available. ReceiveAvailable
	// and RefundAvailable are cap − level, floored at 0 (the contract's
	// LeakyBucket.available), all at the pinned block's time.
	ReceiveLevel     Amount `json:"receive_level"`
	RefundLevel      Amount `json:"refund_level"`
	ReceiveAvailable Amount `json:"receive_available"`
	RefundAvailable  Amount `json:"refund_available"`
	// Origin says which chain the token is native to: "ethereum" for an
	// ERC20 escrowed on Ethereum (a minted denom on gno.land), "gno" for
	// the GnoVoucher of a gno.land-native denom; "" when no gno route
	// names the token. Decimals is the token's own decimals().
	Origin   string `json:"origin,omitempty"`
	Decimals *int   `json:"decimals,omitempty"`
	// EscrowBalance is the route escrow's balance of an Ethereum-native
	// token; VoucherSupply the totalSupply of a gno-native voucher. Exact,
	// at the pinned block.
	EscrowBalance *Amount `json:"escrow_balance,omitempty"`
	VoucherSupply *Amount `json:"voucher_supply,omitempty"`
	Reserved      Amount  `json:"reserved"`
	Losses        Amount  `json:"losses"`
	Deficit       Amount  `json:"accepted_deficit"`
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
