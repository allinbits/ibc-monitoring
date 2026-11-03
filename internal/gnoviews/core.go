package gnoviews

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
)

// TrustingPeriods is the gno Ethereum client's trusting window in sync
// committee periods (p/aib/ibc/lightclient/ethereum/v0, TrustingPeriods).
const TrustingPeriods = 8

// clientJSON is the core's Render("clients/<id>") shape. Numbers are kept as
// tokens: the core builds them from float64, so anything above 2^53 is
// already imprecise on the chain side and must not be made worse here.
type clientJSON struct {
	ID                 string          `json:"id"`
	Type               string          `json:"type"`
	Creator            string          `json:"creator"`
	Status             string          `json:"status"`
	Counterparty       string          `json:"counterparty_client_id"`
	ClientState        json.RawMessage `json:"client_state"`
	LastConsensusState json.RawMessage `json:"last_consensus_state"`
}

type heightJSON struct {
	RevisionNumber json.Number `json:"revision_number"`
	RevisionHeight json.Number `json:"revision_height"`
}

type ethClientState struct {
	ChainID                      string     `json:"chain_id"`
	GenesisTime                  string     `json:"genesis_time"`
	GenesisSlot                  string     `json:"genesis_slot"`
	SecondsPerSlot               string     `json:"seconds_per_slot"`
	SlotsPerEpoch                string     `json:"slots_per_epoch"`
	EpochsPerSyncCommitteePeriod string     `json:"epochs_per_sync_committee_period"`
	LatestHeight                 heightJSON `json:"latest_height"`
	LatestExecutionBlockNumber   string     `json:"latest_execution_block_number"`
	IsFrozen                     bool       `json:"is_frozen"`
	IBCContractAddress           string     `json:"ibc_contract_address"`
}

type ethConsensusState struct {
	Slot      json.Number `json:"slot"`
	Timestamp json.Number `json:"timestamp"`
}

type tmClientState struct {
	ChainID        string      `json:"chain_id"`
	LatestHeight   heightJSON  `json:"latest_height"`
	FrozenHeight   heightJSON  `json:"frozen_height"`
	TrustingPeriod json.Number `json:"trusting_period"` // float seconds
}

type tmConsensusState struct {
	Timestamp json.Number `json:"timestamp"`
}

type pageJSON struct {
	Items []json.RawMessage `json:"items"`
	Page  json.Number       `json:"page"`
	Total json.Number       `json:"total"` // the number of pages
}

// Clients lists every IBC client on gno.land with status, head, derived
// expiry, verifier version, trust generation, counterparty and retirement.
func (v *Views) Clients(ctx context.Context, p Pin) ([]model.Client, error) {
	var ids []string
	for page := 1; ; page++ {
		var pg pageJSON
		if err := v.renderJSON(ctx, v.paths.Core, fmt.Sprintf("clients?page=%d&limit=100", page), p, &pg); err != nil {
			return nil, err
		}
		for _, it := range pg.Items {
			var c clientJSON
			if err := json.Unmarshal(it, &c); err != nil {
				return nil, fmt.Errorf("core clients page %d: %w", page, err)
			}
			ids = append(ids, c.ID)
		}
		total, _ := num(pg.Total)
		if int64(page) >= total || len(pg.Items) == 0 {
			break
		}
	}
	out := make([]model.Client, 0, len(ids))
	for _, id := range ids {
		c, err := v.Client(ctx, id, p)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// Client reads one client.
func (v *Views) Client(ctx context.Context, id string, p Pin) (model.Client, error) {
	var cj clientJSON
	if err := v.renderJSON(ctx, v.paths.Core, "clients/"+id, p, &cj); err != nil {
		return model.Client{}, err
	}
	c := model.Client{
		Chain:          model.ChainGno,
		ID:             cj.ID,
		Status:         model.ClientStatus(cj.Status),
		CounterpartyID: cj.Counterparty,
		Provenance:     v.provenance(p),
	}
	switch cj.Type {
	case "ethereum":
		c.Kind = model.ClientKindEthereum
		if err := v.fillEthereum(&c, cj); err != nil {
			return model.Client{}, fmt.Errorf("client %s: %w", id, err)
		}
	case "arc":
		c.Kind = model.ClientKindArc
		if err := v.fillArc(&c, cj); err != nil {
			return model.Client{}, fmt.Errorf("client %s: %w", id, err)
		}
	case "tendermint":
		c.Kind = model.ClientKindTendermint
		if err := v.fillTendermint(&c, cj); err != nil {
			return model.Client{}, fmt.Errorf("client %s: %w", id, err)
		}
	default:
		c.Kind = model.ClientKind(cj.Type)
	}
	if val, err := v.eval(ctx, v.paths.Core, "VerifierVersion("+q(id)+")", p); err == nil {
		c.VerifierVersion, _ = val.String()
	}
	if val, err := v.eval(ctx, v.paths.Core, "ClientTrustGeneration("+q(id)+")", p); err == nil {
		c.TrustGeneration, _ = val.Uint()
	}
	if val, err := v.eval(ctx, v.paths.Core, "IsClientRetiredFor("+q(id)+", \"transfer\")", p); err == nil {
		c.RetiredForTransfer, _ = val.Bool()
	}
	if val, err := v.eval(ctx, v.paths.Core, "IsClientRetiredFor("+q(id)+", \"gov\")", p); err == nil {
		c.RetiredForGov, _ = val.Bool()
	}
	return c, nil
}

func (v *Views) fillEthereum(c *model.Client, cj clientJSON) error {
	if len(cj.ClientState) == 0 {
		return nil
	}
	var cs ethClientState
	if err := json.Unmarshal(cj.ClientState, &cs); err != nil {
		return fmt.Errorf("client_state: %w", err)
	}
	c.ChainID = cs.ChainID
	c.Frozen = cs.IsFrozen
	var err error
	if c.LatestHeight, err = unum(cs.LatestHeight.RevisionHeight); err != nil {
		return err
	}
	if c.LatestRevision, err = unum(cs.LatestHeight.RevisionNumber); err != nil {
		return err
	}
	if c.LatestExecutionBlock, err = ustr(cs.LatestExecutionBlockNumber); err != nil {
		return err
	}
	genesisTime, err := ustr(cs.GenesisTime)
	if err != nil {
		return err
	}
	genesisSlot, err := ustr(cs.GenesisSlot)
	if err != nil {
		return err
	}
	secondsPerSlot, err := ustr(cs.SecondsPerSlot)
	if err != nil {
		return err
	}
	slotsPerEpoch, err := ustr(cs.SlotsPerEpoch)
	if err != nil {
		return err
	}
	epochsPerPeriod, err := ustr(cs.EpochsPerSyncCommitteePeriod)
	if err != nil {
		return err
	}
	periodSlots := TrustingPeriods * epochsPerPeriod * slotsPerEpoch
	c.TrustingPeriod = time.Duration(periodSlots*secondsPerSlot) * time.Second
	if secondsPerSlot > 0 && c.LatestHeight >= genesisSlot {
		headUnix := genesisTime + (c.LatestHeight-genesisSlot)*secondsPerSlot
		c.HeadTime = time.Unix(int64(headUnix), 0).UTC()
		c.Expiry = ethExpiry(genesisTime, genesisSlot, secondsPerSlot, c.LatestHeight, periodSlots)
	}
	// The head time shown is the stored consensus state's timestamp; the
	// expiry stays on the slot clock, which is what the client judges by.
	if len(cj.LastConsensusState) > 0 {
		var ccs ethConsensusState
		if err := json.Unmarshal(cj.LastConsensusState, &ccs); err == nil && ccs.Timestamp != "" {
			if ts, err := num(ccs.Timestamp); err == nil && ts > 0 {
				c.HeadTime = time.Unix(ts, 0).UTC()
			}
		}
	}
	return nil
}

// ethExpiry is the first time at which the gno Ethereum client reports
// Expired: its expired() holds once currentSlot > latestSlot + window, with
// currentSlot = (now − genesisTime)/secondsPerSlot + genesisSlot, i.e. from
// the start of slot latestSlot + window + 1. It is a time on the slot clock,
// not the stored consensus timestamp plus the window: the two differ
// whenever the timestamp is not the latest slot's own start.
func ethExpiry(genesisTime, genesisSlot, secondsPerSlot, latestSlot, windowSlots uint64) time.Time {
	return time.Unix(int64(genesisTime+(latestSlot-genesisSlot+windowSlots+1)*secondsPerSlot), 0).UTC()
}

func (v *Views) fillTendermint(c *model.Client, cj clientJSON) error {
	if len(cj.ClientState) == 0 {
		return nil
	}
	var cs tmClientState
	if err := json.Unmarshal(cj.ClientState, &cs); err != nil {
		return fmt.Errorf("client_state: %w", err)
	}
	c.ChainID = cs.ChainID
	var err error
	if c.LatestHeight, err = unum(cs.LatestHeight.RevisionHeight); err != nil {
		return err
	}
	if c.LatestRevision, err = unum(cs.LatestHeight.RevisionNumber); err != nil {
		return err
	}
	if fh, err := unum(cs.FrozenHeight.RevisionHeight); err == nil && fh > 0 {
		c.Frozen = true
	}
	// trusting_period is a float number of seconds (RenderDuration).
	if cs.TrustingPeriod != "" {
		f, err := cs.TrustingPeriod.Float64()
		if err != nil {
			return fmt.Errorf("trusting_period: %w", err)
		}
		c.TrustingPeriod = time.Duration(math.Round(f * float64(time.Second)))
	}
	if len(cj.LastConsensusState) > 0 {
		var ccs tmConsensusState
		if err := json.Unmarshal(cj.LastConsensusState, &ccs); err == nil {
			if ts, err := num(ccs.Timestamp); err == nil && ts > 0 {
				c.HeadTime = time.Unix(ts, 0).UTC()
				c.Expiry = c.HeadTime.Add(c.TrustingPeriod)
			}
		}
	}
	return nil
}

// ClientStatus reads one client's status.
func (v *Views) ClientStatus(ctx context.Context, id string, p Pin) (model.ClientStatus, error) {
	val, err := v.eval(ctx, v.paths.Core, "ClientStatus("+q(id)+")", p)
	if err != nil {
		return "", err
	}
	s, err := val.String()
	return model.ClientStatus(s), err
}
