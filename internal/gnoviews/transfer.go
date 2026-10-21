package gnoviews

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/ibcpacket"
	"github.com/allinbits/ibc-monitoring/internal/model"
)

// Shapes of p/aib/ibc/ics20/controls/v0 reader.gno. Every amount is a decimal
// JSON string (a 256-bit value is lossy as a JSON number: gno-realms ADR
// 0005); heights, times and counts are bare JSON numbers. json.Number keeps
// the text of either, and amount parses it exactly.
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
	Decimals       json.Number            `json:"decimals"`
	Meter          meterJSON              `json:"meter"`
	RefundMeter    meterJSON              `json:"refund_meter"`
	Waiting        json.Number            `json:"waiting"`
	QueuedRefunds  json.Number            `json:"queued_refunds"`
	Totals         map[string]json.Number `json:"totals"`
}

// totalEscrowJSON is the transfer total_escrow/<denom> document; its amounts
// are decimal strings, exact.
type totalEscrowJSON struct {
	Denom   string      `json:"denom"`
	Amount  json.Number `json:"amount"`
	Clients []struct {
		Client string      `json:"client"`
		Amount json.Number `json:"amount"`
	} `json:"clients"`
}

// denomTraceJSON is denoms/ibc/<hash>: {base, path, denom}.
type denomTraceJSON struct {
	Base  string `json:"base"`
	Path  string `json:"path"`
	Denom string `json:"denom"`
}

// MeterLiveUnavailable is the reason the live level is not computed here.
const MeterLiveUnavailable = "no exact source: the realm's refill depends on the payouts-epoch reset, the fractional remainder and the stored rate and ceiling (design §4.1)"

// Routes reads every Ethereum route of the transfer realm with each denom's
// Ethereum token resolved (the remote address for an unescrow denom, the
// trace base for a minted one) and the stored meter levels; live levels are
// marked unavailable (no exact source, design §4.1).
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
	routeClients := append([]string{rj.Client}, rj.Replaced...)
	for _, dj := range rj.Denoms {
		d, err := parseDenom(dj)
		if err != nil {
			return r, fmt.Errorf("route %s denom %s: %w", rj.Route, dj.Denom, err)
		}
		switch d.Kind {
		case "unescrow":
			d.Token = strings.ToLower(d.Remote)
			lock, err := v.lock(ctx, rj.Route, d.Denom, p)
			if err != nil {
				return r, err
			}
			d.Lock = &lock
		case "mint":
			token, trace, err := v.denomTrace(ctx, d.Denom, routeClients, p)
			if err != nil {
				return r, fmt.Errorf("route %s denom %s: %w", rj.Route, dj.Denom, err)
			}
			d.Token, d.Trace = token, trace
			zero := model.AmountFromInt64(0)
			d.Lock = &zero
		}
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

// lock reads the route's escrow accounting of a denom from
// total_escrow/<denom> (exact: a decimal string on chain).
func (v *Views) lock(ctx context.Context, route, denom string, p Pin) (model.Amount, error) {
	var te totalEscrowJSON
	if err := v.renderJSON(ctx, v.paths.Transfer, "total_escrow/"+denom, p, &te); err != nil {
		return model.Amount{}, err
	}
	for _, c := range te.Clients {
		if c.Client != route {
			continue
		}
		return amount(c.Amount)
	}
	return model.AmountFromInt64(0), nil
}

// denomTrace resolves a minted denom (ibc/<hash>) to its Ethereum token: the
// trace base, which must be a hex address, over a path naming one of the
// route's clients (transfer/<client>).
func (v *Views) denomTrace(ctx context.Context, denom string, routeClients []string, p Pin) (token, path string, err error) {
	hash, ok := strings.CutPrefix(denom, "ibc/")
	if !ok {
		return "", "", fmt.Errorf("minted denom %q is not an ibc/<hash> voucher", denom)
	}
	var dt denomTraceJSON
	if err := v.renderJSON(ctx, v.paths.Transfer, "denoms/ibc/"+hash, p, &dt); err != nil {
		return "", "", err
	}
	if dt.Denom != denom {
		return "", "", fmt.Errorf("denom trace answers %q for %q", dt.Denom, denom)
	}
	base := strings.ToLower(dt.Base)
	if !isHexAddress(base) {
		return "", "", fmt.Errorf("denom trace base %q is not an Ethereum address", dt.Base)
	}
	client, ok := strings.CutPrefix(dt.Path, "transfer/")
	if !ok || strings.Contains(client, "/") {
		return "", "", fmt.Errorf("denom trace path %q is not a single transfer hop", dt.Path)
	}
	for _, c := range routeClients {
		if c == client {
			return base, dt.Path, nil
		}
	}
	return "", "", fmt.Errorf("denom trace path %q names a client outside the route", dt.Path)
}

func isHexAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
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
	// decimals is the governed decimals of the denom's voucher (0 for an
	// unescrowed denom, whose token is gno-native).
	if dj.Decimals != "" {
		dec, err := num(dj.Decimals)
		if err != nil {
			return d, fmt.Errorf("decimals: %w", err)
		}
		if dec < 0 || dec > 255 {
			return d, fmt.Errorf("decimals %d out of range", dec)
		}
		d.Decimals = int(dec)
	}
	totals := map[string]*model.Amount{
		"credited": &d.Totals.Credited, "owed": &d.Totals.Owed, "withheld": &d.Totals.Withheld,
		"swept": &d.Totals.Swept, "paid": &d.Totals.Paid, "fees": &d.Totals.Fees,
		"sweep_paid": &d.Totals.SweepPaid, "outstanding": &d.Totals.Outstanding,
		"returning": &d.Totals.Returning, "proven_owed": &d.Totals.ProvenOwed,
		"in_flight": &d.Totals.InFlight, "sent": &d.Totals.Sent, "send_fees": &d.Totals.SendFees,
		"refunded": &d.Totals.Refunded, "queued_refunds": &d.Totals.QueuedRefunds, "lost": &d.Totals.Lost,
		"recapitalized": &d.Totals.Recapitalized, "reduced": &d.Totals.Reduced, "accepted": &d.Totals.Accepted,
		"unrecovered": &d.Totals.Unrecovered, "unresolved": &d.Totals.Unresolved,
	}
	// Every total is a 256-bit value rendered exactly (no saturating int64
	// view since gno-realms ADR 0005); a key the realm no longer renders
	// (parked, gone with the minted refunds' parking) would read as zero.
	for k, dst := range totals {
		n, ok := dj.Totals[k]
		if !ok {
			return d, fmt.Errorf("totals.%s: missing", k)
		}
		if *dst, err = amount(n); err != nil {
			return d, fmt.Errorf("totals.%s: %w", k, err)
		}
	}
	return d, nil
}

// parseMeter reads the stored level and marks the live level unavailable.
func parseMeter(mj meterJSON) (model.Meter, error) {
	m := model.Meter{LiveSource: "unavailable", LiveReason: MeterLiveUnavailable}
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

// packetJSON is SendJSON's packet: the full packet with hex payload values.
type packetJSON struct {
	Sequence          json.Number `json:"sequence"`
	SourceClient      string      `json:"source_client"`
	DestinationClient string      `json:"destination_client"`
	TimeoutTimestamp  json.Number `json:"timeout_timestamp"`
	Payloads          []struct {
		SourcePort      string `json:"source_port"`
		DestinationPort string `json:"destination_port"`
		Version         string `json:"version"`
		Encoding        string `json:"encoding"`
		Value           string `json:"value"`
	} `json:"payloads"`
}

type sendJSON struct {
	Key             string      `json:"key"`
	Packet          packetJSON  `json:"packet"`
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

// SendRecord is one entry of routes/<route>/sends with the complete packet,
// payloads included, and the record's commitment checked against it: a
// record whose commitment is not CommitPacket(packet) is not a usable
// witness (CommitmentOK false), whatever else it says.
type SendRecord struct {
	Key          string
	Packet       ibcpacket.Packet
	Commitment   [32]byte
	CommitmentOK bool
	IsTransfer   bool
	Sender       string
	Receiver     string
	Denom        string
	Kind         string
	Amount       model.Amount
	Fee          model.Amount
	SentHeight   int64
	SentAt       int64
	Outcome      string
	Processed    int64
	ProcessedAt  int64
	Payment      string
	Refunded     model.Amount
	Settlement   int64
	Reservation  model.Amount
	Proven       int64
	ProvenAt     uint64
	ProvenBy     string
	ProvenGen    uint64
	Incident     string
}

func parseSend(raw json.RawMessage) (SendRecord, error) {
	var sj sendJSON
	if err := json.Unmarshal(raw, &sj); err != nil {
		return SendRecord{}, err
	}
	rec := SendRecord{Key: sj.Key, Sender: sj.Sender, Receiver: sj.Receiver, Denom: sj.Denom, Kind: sj.Kind, Outcome: sj.Outcome, Payment: sj.Payment, ProvenBy: sj.ProvenBy, Incident: sj.Incident}
	var err error
	if rec.Packet.Sequence, err = unum(sj.Packet.Sequence); err != nil {
		return rec, err
	}
	rec.Packet.SourceClient, rec.Packet.DestinationClient = sj.Packet.SourceClient, sj.Packet.DestinationClient
	if rec.Packet.TimeoutTimestamp, err = unum(sj.Packet.TimeoutTimestamp); err != nil {
		return rec, err
	}
	for _, pl := range sj.Packet.Payloads {
		val, err := hex.DecodeString(pl.Value)
		if err != nil {
			return rec, fmt.Errorf("payload value: %w", err)
		}
		rec.Packet.Payloads = append(rec.Packet.Payloads, ibcpacket.Payload{SourcePort: pl.SourcePort, DestinationPort: pl.DestinationPort, Version: pl.Version, Encoding: pl.Encoding, Value: val})
	}
	c, err := hex.DecodeString(sj.Commitment)
	if err != nil || len(c) != 32 {
		return rec, fmt.Errorf("commitment %q: not 32 hex bytes", sj.Commitment)
	}
	copy(rec.Commitment[:], c)
	rec.CommitmentOK = ibcpacket.Commit(rec.Packet) == rec.Commitment
	rec.IsTransfer = ibcpacket.IsTransfer(rec.Packet)
	for _, f := range []struct {
		dst *model.Amount
		src json.Number
	}{{&rec.Amount, sj.Amount}, {&rec.Fee, sj.Fee}, {&rec.Refunded, sj.Refunded}, {&rec.Reservation, sj.Reservation}} {
		if *f.dst, err = amount(f.src); err != nil {
			return rec, err
		}
	}
	for _, f := range []struct {
		dst *int64
		src json.Number
	}{{&rec.SentHeight, sj.SentHeight}, {&rec.SentAt, sj.SentAt}, {&rec.Processed, sj.ProcessedHeight}, {&rec.ProcessedAt, sj.ProcessedAt}, {&rec.Settlement, sj.Settlement}, {&rec.Proven, sj.Proven}} {
		if *f.dst, err = num(f.src); err != nil {
			return rec, err
		}
	}
	if rec.ProvenAt, err = unum(sj.ProvenAt); err != nil {
		return rec, err
	}
	if rec.ProvenGen, err = unum(sj.ProvenGen); err != nil {
		return rec, err
	}
	return rec, nil
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
			rec, err := parseSend(e)
			if err != nil {
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
