package model

// Seat is one of the six seats of a chain (0–3 guardians, 4–5 watch keys).
type Seat struct {
	Index   int    `json:"index"`
	Role    string `json:"role"` // "guardian" | "watch"
	Address string `json:"address"`
}

// GnoFlag is a standing flag on gno.land, parsed from ibcgov.Flags().
type GnoFlag struct {
	Route    string   `json:"route"`
	Function string   `json:"function"` // sends | credits | payouts | refunds
	Seat     int      `json:"seat"`
	At       int64    `json:"at"`
	Naming   []string `json:"naming,omitempty"`
}

// GnoHold is the global gno.land hold.
type GnoHold struct {
	Held  bool  `json:"held"`
	Since int64 `json:"since,omitempty"`
	// GuardianSeats is how many distinct guardian seats have a standing
	// flag across every subject read (routes and standalone clients); the
	// hold stands at two. Explained is whether that accounts for Held.
	GuardianSeats int  `json:"guardian_seats"`
	Explained     bool `json:"explained"`
	// EndsAt is known only from the HoldEnding event (docs/design.md §15).
	EndsAt int64 `json:"ends_at,omitempty"`
}

// GnoDecision is one entry of ibcgov's ledger (Render("decisions")).
type GnoDecision struct {
	ID        Seq    `json:"id"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	State     string `json:"state"` // pending | applied | cancelled | spent
	Made      int64  `json:"made"`
	Recorded  int64  `json:"recorded"`
	Due       int64  `json:"due"`
	Applied   int64  `json:"applied"`
	Cancelled int64  `json:"cancelled"`
	Note      string `json:"note"`
}

// GnoDispatch is one Ethereum-bound dispatch (Render("dispatch/<e>/<seq>")).
type GnoDispatch struct {
	Epoch      uint32 `json:"epoch"`
	Seq        Seq    `json:"seq"`
	Kind       uint8  `json:"kind"`
	Decision   Seq    `json:"decision"`
	Body       string `json:"body"` // hex envelope
	Status     uint8  `json:"status"`
	Redispatch bool   `json:"redispatch"`
	InFlight   bool   `json:"in_flight"`
	Transport  Seq    `json:"transport"`
	Sends      int64  `json:"sends"`
	Event      string `json:"event"`
}

// GnoGovernance is the gno.land side of the governance dashboard.
type GnoGovernance struct {
	Governor      string        `json:"governor"`
	Yielded       int64         `json:"yielded"`
	GovDAOGoverns bool          `json:"govdao_governs"`
	Seats         []Seat        `json:"seats"`
	Hold          GnoHold       `json:"hold"`
	Flags         []GnoFlag     `json:"flags"`
	Epoch         uint32        `json:"epoch"`
	EpochClient   string        `json:"epoch_client"`
	LastSeq       Seq           `json:"last_seq"`
	Decisions     []GnoDecision `json:"decisions"`
	Dispatches    []GnoDispatch `json:"dispatches"`
	Provenance    Provenance    `json:"provenance"`
}

// EthDecision is IBCAdmin.decision(epoch, seq) with derived fields.
type EthDecision struct {
	Epoch         uint32 `json:"epoch"`
	Seq           Seq    `json:"seq"`
	Hash          string `json:"hash"`
	Kind          uint8  `json:"kind"`
	State         uint8  `json:"state"` // 0 none 1 buffered 2 pending 3 executed 4 spent 5 cancelled
	StateName     string `json:"state_name"`
	Bad           bool   `json:"bad"`
	RecordedTime  int64  `json:"recorded_time"`
	RecordedBlock uint64 `json:"recorded_block"`
	// MatureAt is recordedTime + the kind's delay (not stored on chain).
	MatureAt   int64  `json:"mature_at,omitempty"`
	Params     string `json:"params,omitempty"` // hex, while stored
	Tombstoned bool   `json:"tombstoned"`
}

// EthRecovery is IBCAdmin's emergency recovery state.
type EthRecovery struct {
	Delay           uint64   `json:"delay"`
	Threshold       uint8    `json:"threshold"`
	CancelThreshold uint8    `json:"cancel_threshold"`
	Silence         uint64   `json:"silence"`
	HoldPeriod      uint64   `json:"hold_period"`
	Enabled         bool     `json:"enabled"`
	Proposal        string   `json:"proposal"` // hash, zero when none
	OpenedAt        uint64   `json:"opened_at"`
	ExecutableAt    uint64   `json:"executable_at,omitempty"`
	CancelVotes     uint8    `json:"cancel_votes"`
	Approved        []string `json:"approved"` // per guardian seat, the proposal hash approved
}

// EthGovernance is the Ethereum side of the governance dashboard.
type EthGovernance struct {
	Epoch          uint32        `json:"epoch"`
	LastSeq        Seq           `json:"last_seq"`
	GovClient      string        `json:"gov_client"`
	GnoChainID     string        `json:"gno_chain_id"`
	LastRecordTime int64         `json:"last_record_time"`
	SetupDone      bool          `json:"setup_done"`
	Keys           []string      `json:"keys"`
	Flags          []EthFlag     `json:"flags"`
	Hold           *EthHold      `json:"hold,omitempty"`
	Holds          []EthHold     `json:"holds"`
	ConflictBlock  uint64        `json:"conflict_block"`
	Exec           string        `json:"exec"`
	ExecCodeHash   string        `json:"exec_code_hash"`
	Decisions      []EthDecision `json:"decisions"`
	// Buffered lists the sequences above LastSeq that hold a buffered decision.
	Buffered   []Seq       `json:"buffered"`
	Recovery   EthRecovery `json:"recovery"`
	Provenance Provenance  `json:"provenance"`
}

// DecisionStateName maps IBCAdmin's state byte to a name.
func DecisionStateName(s uint8) string {
	switch s {
	case 1:
		return "buffered"
	case 2:
		return "pending"
	case 3:
		return "executed"
	case 4:
		return "spent"
	case 5:
		return "cancelled"
	default:
		return "none"
	}
}

// KindName maps a governance kind to the ADR's name.
func KindName(k uint8) string {
	names := [...]string{"", "cancel", "unpause", "seats", "limits", "voucher", "client", "upgrade", "role", "hand-over", "route-replacement", "route-settings", "settle", "reopen"}
	if int(k) < len(names) {
		return names[k]
	}
	return "unknown"
}
