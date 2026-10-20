package gnoviews

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/allinbits/ibc-monitoring/internal/model"
)

// SeatCount and GuardianSeats mirror ibcgov (guardians.gno).
const (
	SeatCount     = 6
	GuardianSeats = 4
)

type decisionJSON struct {
	ID        json.Number `json:"id"`
	Kind      string      `json:"kind"`
	Text      string      `json:"text"`
	State     string      `json:"state"`
	Made      json.Number `json:"made"`
	Recorded  json.Number `json:"recorded"`
	Due       json.Number `json:"due"`
	Applied   json.Number `json:"applied"`
	Cancelled json.Number `json:"cancelled"`
	Note      string      `json:"note"`
}

type dispatchJSON struct {
	Epoch      json.Number `json:"epoch"`
	Seq        json.Number `json:"seq"`
	Kind       json.Number `json:"kind"`
	Decision   json.Number `json:"decision"`
	Body       string      `json:"body"`
	Status     json.Number `json:"status"`
	Redispatch bool        `json:"redispatch"`
	InFlight   bool        `json:"in_flight"`
	Transport  json.Number `json:"transport"`
	Sends      json.Number `json:"sends"`
	Event      string      `json:"event"`
}

// Governance reads ibcgov's state: governor, seats, hold, flags of the given
// routes (and route-less client ids), the channel's epoch, the full ledger and
// every dispatch of the current epoch.
func (v *Views) Governance(ctx context.Context, routes []string, p Pin) (model.GnoGovernance, error) {
	g := model.GnoGovernance{Provenance: v.provenance(p), Flags: []model.GnoFlag{}, Dispatches: []model.GnoDispatch{}}
	pkg := v.paths.IBCGov

	val, err := v.eval(ctx, pkg, "Governor()", p)
	if err != nil {
		return g, err
	}
	g.Governor = val.Raw
	if val, err = v.eval(ctx, pkg, "Yielded()", p); err != nil {
		return g, err
	}
	if g.Yielded, err = val.Int(); err != nil {
		return g, err
	}
	if val, err = v.eval(ctx, pkg, "GovDAOGoverns()", p); err != nil {
		return g, err
	}
	if g.GovDAOGoverns, err = val.Bool(); err != nil {
		return g, err
	}

	for i := 0; i < SeatCount; i++ {
		val, err := v.eval(ctx, pkg, fmt.Sprintf("Seat(%d)", i), p)
		if err != nil {
			return g, err
		}
		role := "guardian"
		if i >= GuardianSeats {
			role = "watch"
		}
		g.Seats = append(g.Seats, model.Seat{Index: i, Role: role, Address: addressOf(val.Raw)})
	}

	if val, err = v.eval(ctx, pkg, "Held()", p); err != nil {
		return g, err
	}
	if g.Hold.Held, err = val.Bool(); err != nil {
		return g, err
	}
	if val, err = v.eval(ctx, pkg, "HoldSince()", p); err != nil {
		return g, err
	}
	if g.Hold.Since, err = val.Int(); err != nil {
		return g, err
	}

	for _, r := range routes {
		flags, err := v.Flags(ctx, r, p)
		if err != nil {
			return g, err
		}
		g.Flags = append(g.Flags, flags...)
	}

	if val, err = v.eval(ctx, pkg, "Epoch()", p); err != nil {
		return g, err
	}
	epoch, err := val.Uint()
	if err != nil {
		return g, err
	}
	g.Epoch = uint32(epoch)
	if val, err = v.eval(ctx, pkg, "EpochClient()", p); err != nil {
		return g, err
	}
	if g.EpochClient, err = val.String(); err != nil {
		return g, err
	}
	if val, err = v.eval(ctx, pkg, fmt.Sprintf("LastSeq(%d)", g.Epoch), p); err != nil {
		return g, err
	}
	lastSeq, err := val.Uint()
	if err != nil {
		return g, err
	}
	g.LastSeq = model.Seq(lastSeq)

	if g.Decisions, err = v.Decisions(ctx, p); err != nil {
		return g, err
	}
	for seq := uint64(1); seq <= lastSeq; seq++ {
		d, ok, err := v.Dispatch(ctx, g.Epoch, seq, p)
		if err != nil {
			return g, err
		}
		if ok {
			g.Dispatches = append(g.Dispatches, d)
		}
	}
	return g, nil
}

// addressOf strips the type suffix qeval gives an address value: an address
// renders as its bech32 text, e.g. `(g1... address)` or `("g1..." string)`.
func addressOf(raw string) string {
	s := strings.TrimSpace(raw)
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

// Decisions reads the whole ledger (Render("decisions"), oldest first).
func (v *Views) Decisions(ctx context.Context, p Pin) ([]model.GnoDecision, error) {
	var djs []decisionJSON
	if err := v.renderJSON(ctx, v.paths.IBCGov, "decisions", p, &djs); err != nil {
		return nil, err
	}
	out := make([]model.GnoDecision, 0, len(djs))
	for _, dj := range djs {
		d := model.GnoDecision{Kind: dj.Kind, Text: dj.Text, State: dj.State, Note: dj.Note}
		id, err := unum(dj.ID)
		if err != nil {
			return nil, fmt.Errorf("decision %s: %w", dj.ID, err)
		}
		d.ID = model.Seq(id)
		for _, f := range []struct {
			dst *int64
			src json.Number
		}{{&d.Made, dj.Made}, {&d.Recorded, dj.Recorded}, {&d.Due, dj.Due}, {&d.Applied, dj.Applied}, {&d.Cancelled, dj.Cancelled}} {
			if *f.dst, err = num(f.src); err != nil {
				return nil, fmt.Errorf("decision %s: %w", dj.ID, err)
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// Dispatch reads Render("dispatch/<e>/<seq>"); ok is false when absent.
func (v *Views) Dispatch(ctx context.Context, epoch uint32, seq uint64, p Pin) (model.GnoDispatch, bool, error) {
	var dj *dispatchJSON
	if err := v.renderJSON(ctx, v.paths.IBCGov, fmt.Sprintf("dispatch/%d/%d", epoch, seq), p, &dj); err != nil {
		return model.GnoDispatch{}, false, err
	}
	if dj == nil {
		return model.GnoDispatch{}, false, nil
	}
	d := model.GnoDispatch{Body: dj.Body, Redispatch: dj.Redispatch, InFlight: dj.InFlight, Event: dj.Event}
	e, err := unum(dj.Epoch)
	if err != nil {
		return d, false, err
	}
	d.Epoch = uint32(e)
	sq, err := unum(dj.Seq)
	if err != nil {
		return d, false, err
	}
	d.Seq = model.Seq(sq)
	k, err := unum(dj.Kind)
	if err != nil {
		return d, false, err
	}
	d.Kind = uint8(k)
	dec, err := unum(dj.Decision)
	if err != nil {
		return d, false, err
	}
	d.Decision = model.Seq(dec)
	st, err := unum(dj.Status)
	if err != nil {
		return d, false, err
	}
	d.Status = uint8(st)
	tr, err := unum(dj.Transport)
	if err != nil {
		return d, false, err
	}
	d.Transport = model.Seq(tr)
	if d.Sends, err = num(dj.Sends); err != nil {
		return d, false, err
	}
	return d, true, nil
}

// flagLine is `<fn> seat <n> at <unix>[ naming k1,k2]` (ibcgov.Flags).
var flagLine = regexp.MustCompile(`^(\w+) seat (\d+) at (\d+)(?: naming (.*))?$`)

// Flags reads the standing flags under a route key or a client id with no
// route (which still counts toward the global hold).
func (v *Views) Flags(ctx context.Context, routeOrClient string, p Pin) ([]model.GnoFlag, error) {
	val, err := v.eval(ctx, v.paths.IBCGov, "Flags("+q(routeOrClient)+")", p)
	if err != nil {
		return nil, err
	}
	lines, err := val.Strings()
	if err != nil {
		return nil, fmt.Errorf("Flags(%s): %w", routeOrClient, err)
	}
	out := make([]model.GnoFlag, 0, len(lines))
	for _, l := range lines {
		m := flagLine.FindStringSubmatch(l)
		if m == nil {
			return nil, fmt.Errorf("Flags(%s): cannot parse %q", routeOrClient, l)
		}
		seat, _ := strconv.Atoi(m[2])
		at, _ := strconv.ParseInt(m[3], 10, 64)
		f := model.GnoFlag{Route: routeOrClient, Function: m[1], Seat: seat, At: at}
		if m[4] != "" {
			f.Naming = strings.Split(m[4], ",")
		}
		out = append(out, f)
	}
	return out, nil
}

// PayoutsEpoch reads the route's payouts epoch and its start (for Reader.Meters).
func (v *Views) PayoutsEpoch(ctx context.Context, route string, p Pin) (epoch, at int64, err error) {
	vals, err := v.evalN(ctx, v.paths.IBCGov, "PayoutsEpoch("+q(route)+")", p)
	if err != nil {
		return 0, 0, err
	}
	if len(vals) != 2 {
		return 0, 0, fmt.Errorf("PayoutsEpoch(%s): expected 2 results, got %d", route, len(vals))
	}
	if epoch, err = vals[0].Int(); err != nil {
		return 0, 0, err
	}
	if at, err = vals[1].Int(); err != nil {
		return 0, 0, err
	}
	return epoch, at, nil
}
