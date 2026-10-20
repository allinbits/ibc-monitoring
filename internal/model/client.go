package model

import "time"

// ClientStatus is a light client's status as the chain reports it.
type ClientStatus string

const (
	ClientActive       ClientStatus = "Active"
	ClientExpired      ClientStatus = "Expired"
	ClientFrozen       ClientStatus = "Frozen"
	ClientUnknown      ClientStatus = "Unknown"
	ClientUnauthorized ClientStatus = "Unauthorized"
)

// ClientKind says which implementation a client is.
type ClientKind string

const (
	// ClientKindEthereum: gno.land's Ethereum light client (p/aib/ibc/lightclient/ethereum).
	ClientKindEthereum ClientKind = "ethereum"
	// ClientKindTendermint: a Tendermint client on gno.land.
	ClientKindTendermint ClientKind = "tendermint"
	// ClientKindGnoLightClient: the Solidity GnoLightClient on Ethereum.
	ClientKindGnoLightClient ClientKind = "gno-light-client"
)

// Client is one IBC light client on either chain (docs/design.md §5.1).
type Client struct {
	Chain Chain `json:"chain"`
	// ID is the client id on gno.land, or the router client id on Ethereum
	// ("" for a candidate verifier that is no router client yet).
	ID string `json:"id"`
	// Address is the verifier contract (Ethereum only).
	Address string       `json:"address,omitempty"`
	Kind    ClientKind   `json:"kind"`
	Status  ClientStatus `json:"status"`
	// Role tells the dashboard why this client is listed: "route", "enrolled"
	// (a kind 6 target), "candidate" (an open recovery's verifier) or "" on gno.land.
	Role string `json:"role,omitempty"`

	VerifierVersion string `json:"verifier_version,omitempty"`
	TrustGeneration uint64 `json:"trust_generation,omitempty"`
	CounterpartyID  string `json:"counterparty_client_id,omitempty"`
	ChainID         string `json:"chain_id,omitempty"`

	// LatestHeight is the head: a slot for an Ethereum client on gno.land, a
	// gno.land height for a GnoLightClient.
	LatestHeight   uint64 `json:"latest_height"`
	LatestRevision uint64 `json:"latest_revision,omitempty"`
	// LatestExecutionBlock is the execution block of the head (Ethereum
	// client on gno.land only).
	LatestExecutionBlock uint64 `json:"latest_execution_block,omitempty"`
	// HeadTime is the time of the latest trusted consensus state.
	HeadTime time.Time `json:"head_time"`
	// TrustingPeriod and Expiry are derived as docs/design.md §4.1 says.
	TrustingPeriod time.Duration `json:"trusting_period_ns"`
	Expiry         time.Time     `json:"expiry"`
	Frozen         bool          `json:"frozen"`

	RetiredForTransfer bool `json:"retired_for_transfer,omitempty"`
	RetiredForGov      bool `json:"retired_for_gov,omitempty"`

	Provenance Provenance `json:"provenance"`
}
