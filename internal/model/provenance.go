package model

import "time"

// Chain names one of the two chains a value was read from.
type Chain string

const (
	ChainGno      Chain = "gno"
	ChainEthereum Chain = "ethereum"
)

// Verification says how far a read was authenticated (docs/design.md §3.3).
type Verification string

const (
	// VerificationProvider: read from the primary node, not yet cross-checked.
	VerificationProvider Verification = "provider"
	// VerificationCrossChecked: the block hash agreed with the second provider.
	VerificationCrossChecked Verification = "cross-checked"
	// VerificationLightClient: the primary endpoint is a light client (Helios).
	VerificationLightClient Verification = "light-client"
	// VerificationUnverified: the cross-check disagreed; integrity checks skip it.
	VerificationUnverified Verification = "unverified"
)

// Provenance says where a value came from. Every value the API serves carries
// one; the console shows it next to the number.
type Provenance struct {
	Chain Chain `json:"chain"`
	// Height is the gno.land height or the Ethereum block number the value was
	// read at.
	Height uint64 `json:"height"`
	// BlockHash is the hash of that block when known (hex, 0x-prefixed on
	// Ethereum, upper-case hex on gno.land).
	BlockHash string `json:"block_hash,omitempty"`
	// Node is a label for the node answering (never its URL on a public
	// instance).
	Node         string       `json:"node"`
	ReadAt       time.Time    `json:"read_at"`
	Verification Verification `json:"verification"`
	// Finality is "finalized" (Ethereum, beacon or tag), "committed"
	// (gno.land) or "depth:N" (local networks without finality).
	Finality string `json:"finality,omitempty"`
}
