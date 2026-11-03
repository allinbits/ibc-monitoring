package gnoviews

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
)

func (v *Views) fillArc(c *model.Client, cj clientJSON) error {
	if len(cj.ClientState) == 0 || string(cj.ClientState) == "null" {
		return nil
	}
	var cs struct {
		ChainID        string     `json:"chain_id"`
		LatestHeight   heightJSON `json:"latest_height"`
		IsFrozen       bool       `json:"is_frozen"`
		TrustingPeriod string     `json:"trusting_period"`
		model.ArcClient
	}
	if err := json.Unmarshal(cj.ClientState, &cs); err != nil {
		return fmt.Errorf("client_state: %w", err)
	}
	c.ChainID, c.Frozen = cs.ChainID, cs.IsFrozen
	var err error
	if c.LatestHeight, err = unum(cs.LatestHeight.RevisionHeight); err != nil {
		return err
	}
	if c.LatestRevision, err = unum(cs.LatestHeight.RevisionNumber); err != nil {
		return err
	}
	seconds, err := ustr(cs.TrustingPeriod)
	if err != nil || seconds == 0 || seconds > math.MaxInt64/uint64(time.Second) {
		return fmt.Errorf("invalid Arc trusting_period %q", cs.TrustingPeriod)
	}
	c.TrustingPeriod = time.Duration(seconds) * time.Second
	// A missing consensus state leaves expiry unknown; never invent a head time.
	if len(cj.LastConsensusState) > 0 && string(cj.LastConsensusState) != "null" {
		var consensus struct {
			Timestamp           json.Number          `json:"timestamp"`
			BlockHash           string               `json:"block_hash"`
			StateRoot           string               `json:"state_root"`
			RegistryStorageRoot string               `json:"registry_storage_root"`
			NextValidators      []model.ArcValidator `json:"next_validators"`
			NextTotalPower      string               `json:"next_total_power"`
		}
		if err := json.Unmarshal(cj.LastConsensusState, &consensus); err != nil {
			return fmt.Errorf("last_consensus_state: %w", err)
		}
		ts, err := num(consensus.Timestamp)
		if err != nil || ts <= 0 {
			return fmt.Errorf("invalid Arc consensus timestamp %q", consensus.Timestamp)
		}
		c.HeadTime = time.Unix(ts, 0).UTC()
		c.Expiry = c.HeadTime.Add(c.TrustingPeriod)
		cs.BlockHash, cs.StateRoot = consensus.BlockHash, consensus.StateRoot
		cs.RegistryStorageRoot = consensus.RegistryStorageRoot
		cs.NextValidators, cs.NextTotalPower = consensus.NextValidators, consensus.NextTotalPower
		if _, err := ustr(cs.NextTotalPower); err != nil {
			return fmt.Errorf("next_total_power: %w", err)
		}
		for _, val := range cs.NextValidators {
			if _, err := ustr(val.Power); err != nil {
				return fmt.Errorf("validator power: %w", err)
			}
		}
	}
	if cs.NextValidators == nil {
		cs.NextValidators = []model.ArcValidator{}
	}
	c.Arc = &cs.ArcClient
	return nil
}
