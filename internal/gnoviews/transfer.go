package gnoviews

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
)

// Shapes of p/aib/ibc/ics20/controls/v0 reader.gno. Every integer is a bare
// JSON number holding an int64 exactly; json.Number keeps the text.
type routeJSON struct {
	Route               string      `json:"route"`
	Client              string      `json:"client"`
	Replaced            []string    `json:"replaced"`
	Enrolled            json.Number `json:"enrolled"`
	Activated           json.Number `json:"activated"`
	Closed              bool        `json:"closed"`
	LossClosed          bool        `json:"loss_closed"`
	FeeCollector        string      `json:"fee_collector"`
	RefusedRecipients   []string    `json:"refused_recipients"`
	ProtectedRecipients []string    `json:"protected_recipients"`
	Unrecovered         json.Number `json:"unrecovered"`
	Unresolved          json.Number `json:"unresolved"`
	Denoms              []denomJSON `json:"denoms"`
}

type meterJSON struct {
	Level json.Number `json:"level"`
	At    json.Number `json:"at"`
	Epoch json.Number `json:"epoch"`
}

type denomJSON struct {
	Denom          string                 `json:"denom"`
	Kind           string                 `json:"kind"`
	CapFloor       json.Number            `json:"cap_floor"`
	LaunchCapFloor json.Number            `json:"launch_cap_floor"`
	SupplyCap      json.Number            `json:"supply_cap"`
	Fee            json.Number            `json:"fee"`
	FeeFloor       json.Number            `json:"fee_floor"`
	SendMax        json.Number            `json:"send_max"`
	SendMin        json.Number            `json:"send_min"`
	RefundCap      json.Number            `json:"refund_cap"`
	Remote         string                 `json:"remote"`
	Meter          meterJSON              `json:"meter"`
	RefundMeter    meterJSON              `json:"refund_meter"`
	Waiting        json.Number            `json:"waiting"`
	QueuedRefunds  json.Number            `json:"queued_refunds"`
	Totals         map[string]json.Number `json:"totals"`
}

type totalEscrowJSON struct {
	Denom   string      `json:"denom"`
	Amount  json.Number `json:"amount"`
	Clients []struct {
		Client string      `json:"client"`
		Amount json.Number `json:"amount"`
	} `json:"clients"`
}

// Routes reads every Ethereum route of the transfer realm, with the lock of
// each unescrow denom and the live meter level computed from the refill rule
// (the realm only stores the level at the last refill; docs/design.md §4.1).
func (v *Views) Routes(ctx context.Context, p Pin) ([]model.Route, error) {
	var rjs []routeJSON
	if err := v.renderJSON(ctx, v.paths.Transfer, "routes", p, &rjs); err != nil {
		return nil, err
	}
	out := make([]model.Route, 0, len(rjs))
	for _, rj := range rjs {
		r, err := v.route(ctx, rj, p)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Route reads one route by key.
func (v *Views) Route(ctx context.Context, key string, p Pin) (model.Route, error) {
	var rj *routeJSON
	if err := v.renderJSON(ctx, v.paths.Transfer, "routes/"+key, p, &rj); err != nil {
		return model.Route{}, err
	}
	if rj == nil {
		return model.Route{}, fmt.Errorf("route %q not found", key)
	}
	return v.route(ctx, *rj, p)
}

func (v *Views) route(ctx context.Context, rj routeJSON, p Pin) (model.Route, error) {
	r := model.Route{
		Key:                 rj.Route,
		Client:              rj.Client,
		Replaced:            nonNil(rj.Replaced),
		Closed:              rj.Closed,
		LossClosed:          rj.LossClosed,
		FeeCollector:        rj.FeeCollector,
		RefusedRecipients:   nonNil(rj.RefusedRecipients),
		ProtectedRecipients: nonNil(rj.ProtectedRecipients),
		Provenance:          v.provenance(p),
	}
	enrolled, err := num(rj.Enrolled)
	if err != nil {
		return r, fmt.Errorf("route %s enrolled: %w", rj.Route, err)
	}
	if enrolled > 0 {
		r.Enrolled = time.Unix(enrolled, 0).UTC()
	}
	if r.Activated, err = unum(rj.Activated); err != nil {
		return r, fmt.Errorf("route %s activated: %w", rj.Route, err)
	}
	if r.Unrecovered, err = amount(rj.Unrecovered); err != nil {
		return r, err
	}
	if r.Unresolved, err = amount(rj.Unresolved); err != nil {
		return r, err
	}
	for _, dj := range rj.Denoms {
		d, err := parseDenom(dj)
		if err != nil {
			return r, fmt.Errorf("route %s denom %s: %w", rj.Route, dj.Denom, err)
		}
		if d.Kind == "unescrow" {
			lock, err := v.lock(ctx, rj.Route, d.Denom, p)
			if err != nil {
				return r, err
			}
			d.Lock = &lock
		} else {
			zero := model.AmountFromInt64(0)
			d.Lock = &zero
		}
		fillLiveMeters(&d, p.Time)
		r.Denoms = append(r.Denoms, d)
	}
	if r.Denoms == nil {
		r.Denoms = []model.Denom{}
	}
	return r, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// lock reads the route's escrow of a denom from total_escrow/<denom>.
func (v *Views) lock(ctx context.Context, route, denom string, p Pin) (model.Amount, error) {
	var te totalEscrowJSON
	if err := v.renderJSON(ctx, v.paths.Transfer, "total_escrow/"+denom, p, &te); err != nil {
		return model.Amount{}, err
	}
	for _, c := range te.Clients {
		if c.Client == route {
			return amount(c.Amount)
		}
	}
	return model.AmountFromInt64(0), nil
}

func parseDenom(dj denomJSON) (model.Denom, error) {
	d := model.Denom{Denom: dj.Denom, Kind: dj.Kind, Remote: dj.Remote}
	fields := []struct {
		dst *model.Amount
		src json.Number
	}{
		{&d.CapFloor, dj.CapFloor}, {&d.LaunchCapFloor, dj.LaunchCapFloor}, {&d.SupplyCap, dj.SupplyCap},
		{&d.Fee, dj.Fee}, {&d.FeeFloor, dj.FeeFloor}, {&d.SendMax, dj.SendMax}, {&d.SendMin, dj.SendMin},
		{&d.RefundCap, dj.RefundCap},
	}
	var err error
	for _, f := range fields {
		if *f.dst, err = amount(f.src); err != nil {
			return d, err
		}
	}
	if d.Meter, err = parseMeter(dj.Meter); err != nil {
		return d, err
	}
	if d.RefundMeter, err = parseMeter(dj.RefundMeter); err != nil {
		return d, err
	}
	if d.Waiting, err = num(dj.Waiting); err != nil {
		return d, err
	}
	if d.QueuedRefunds, err = num(dj.QueuedRefunds); err != nil {
		return d, err
	}
	totals := map[string]*model.Amount{
		"credited": &d.Totals.Credited, "owed": &d.Totals.Owed, "withheld": &d.Totals.Withheld,
		"swept": &d.Totals.Swept, "paid": &d.Totals.Paid, "fees": &d.Totals.Fees,
		"sweep_paid": &d.Totals.SweepPaid, "outstanding": &d.Totals.Outstanding,
		"returning": &d.Totals.Returning, "parked": &d.Totals.Parked, "proven_owed": &d.Totals.ProvenOwed,
		"in_flight": &d.Totals.InFlight, "sent": &d.Totals.Sent, "send_fees": &d.Totals.SendFees,
		"refunded": &d.Totals.Refunded, "queued_refunds": &d.Totals.QueuedRefunds, "lost": &d.Totals.Lost,
		"recapitalized": &d.Totals.Recapitalized, "reduced": &d.Totals.Reduced, "accepted": &d.Totals.Accepted,
		"unrecovered": &d.Totals.Unrecovered, "unresolved": &d.Totals.Unresolved,
	}
	for k, dst := range totals {
		if *dst, err = amount(dj.Totals[k]); err != nil {
			return d, fmt.Errorf("totals.%s: %w", k, err)
		}
	}
	return d, nil
}

func parseMeter(mj meterJSON) (model.Meter, error) {
	var m model.Meter
	var err error
	if m.Level, err = amount(mj.Level); err != nil {
		return m, err
	}
	if m.At, err = num(mj.At); err != nil {
		return m, err
	}
	if m.Epoch, err = num(mj.Epoch); err != nil {
		return m, err
	}
	return m, nil
}

// DayCap is the bridge meter's cap: max(CapFloor, lock/10) for an unescrow
// denom, CapFloor for a minted one (controls/v0 route.gno DayCap).
func DayCap(d model.Denom) model.Amount {
	if d.Kind != "unescrow" || d.Lock == nil {
		return d.CapFloor
	}
	tenth := new(big.Int).Quo(d.Lock.Int(), big.NewInt(10))
	if tenth.Cmp(d.CapFloor.Int()) > 0 {
		return model.NewAmount(tenth)
	}
	return d.CapFloor
}

// LiveLevel refills a stored meter level to now: the bucket refills at
// cap/86400 per second with ceiling cap (ADR §7.1), and the realm's stored
// level is what it held at `at`. It never backdates a cap change: the stored
// level stays and only the ceiling is the cap in force now.
func LiveLevel(level model.Amount, at int64, now time.Time, cap model.Amount) model.Amount {
	elapsed := now.Unix() - at
	if elapsed <= 0 {
		return capAt(level, cap)
	}
	refill := new(big.Int).Mul(cap.Int(), big.NewInt(elapsed))
	refill.Quo(refill, big.NewInt(86400))
	live := new(big.Int).Add(level.Int(), refill)
	return capAt(model.NewAmount(live), cap)
}

func capAt(level, cap model.Amount) model.Amount {
	if level.Cmp(cap) > 0 {
		return cap
	}
	return level
}

// fillLiveMeters computes the live bridge and refund levels client-side and
// labels them so the console shows the source. When the realm exposes
// Reader.Meters (docs/design.md §15), the reader value replaces this.
func fillLiveMeters(d *model.Denom, now time.Time) {
	cap := DayCap(*d)
	live := LiveLevel(d.Meter.Level, d.Meter.At, now, cap)
	d.Meter.Live = &live
	d.Meter.LiveSource = "client"
	d.Meter.Cap = &cap
	rcap := d.RefundCap
	rlive := LiveLevel(d.RefundMeter.Level, d.RefundMeter.At, now, rcap)
	d.RefundMeter.Live = &rlive
	d.RefundMeter.LiveSource = "client"
	d.RefundMeter.Cap = &rcap
}

// Page is one cursor page of a controls listing (claims, sweeps, sends,
// losses): at most 100 entries and the cursor of the next page ("" at the end).
type Page struct {
	Entries []json.RawMessage `json:"entries"`
	Next    string            `json:"next"`
}

// RoutePage reads one page of routes/<route>/<kind>?from=<cursor>, kind one
// of "claims", "sweeps", "sends", "losses". Entries are returned raw for the
// caller to decode (SendRecord, Claim, ...).
func (v *Views) RoutePage(ctx context.Context, route, kind, cursor string, p Pin) (Page, error) {
	path := fmt.Sprintf("routes/%s/%s", route, kind)
	if cursor != "" {
		path += "?from=" + cursor
	}
	var pg Page
	if err := v.renderJSON(ctx, v.paths.Transfer, path, p, &pg); err != nil {
		return Page{}, err
	}
	return pg, nil
}

// SendRecord is one entry of routes/<route>/sends (SendJSON).
type SendRecord struct {
	Key    string `json:"key"`
	Packet struct {
		Sequence          json.Number `json:"sequence"`
		SourceClient      string      `json:"source_client"`
		DestinationClient string      `json:"destination_client"`
		TimeoutTimestamp  json.Number `json:"timeout_timestamp"`
	} `json:"packet"`
	Commitment      string      `json:"commitment"`
	Sender          string      `json:"sender"`
	Receiver        string      `json:"receiver"`
	Denom           string      `json:"denom"`
	Kind            string      `json:"kind"`
	Amount          json.Number `json:"amount"`
	Fee             json.Number `json:"fee"`
	SentHeight      json.Number `json:"sent_height"`
	SentAt          json.Number `json:"sent_at"`
	Outcome         string      `json:"outcome"`
	ProcessedHeight json.Number `json:"processed_height"`
	ProcessedAt     json.Number `json:"processed_at"`
	Payment         string      `json:"payment"`
	Refunded        json.Number `json:"refunded"`
	Settlement      json.Number `json:"settlement"`
	Reservation     json.Number `json:"reservation"`
	Proven          json.Number `json:"proven"`
	ProvenAt        json.Number `json:"proven_at"`
	ProvenBy        string      `json:"proven_by"`
	ProvenGen       json.Number `json:"proven_gen"`
	Incident        string      `json:"incident"`
}

// Sends walks every page of a route's send records.
func (v *Views) Sends(ctx context.Context, route string, p Pin) ([]SendRecord, error) {
	var out []SendRecord
	cursor := ""
	for {
		pg, err := v.RoutePage(ctx, route, "sends", cursor, p)
		if err != nil {
			return nil, err
		}
		for _, e := range pg.Entries {
			var rec SendRecord
			if err := json.Unmarshal(e, &rec); err != nil {
				return nil, fmt.Errorf("route %s send record: %w", route, err)
			}
			out = append(out, rec)
		}
		if pg.Next == "" || len(pg.Entries) == 0 {
			return out, nil
		}
		cursor = pg.Next
	}
}
