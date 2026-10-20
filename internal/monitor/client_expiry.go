package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// ClientExpiryConfig holds the liveness thresholds (ADR §9: GnoLightClient
// updated at least daily, alarm after 3 days without one; the gno Ethereum
// client updated daily, alarm after 24 h).
type ClientExpiryConfig struct {
	// WarnBefore raises a warning when the expiry is closer than this.
	WarnBefore time.Duration
	// CriticalBefore raises a critical alarm when closer than this.
	CriticalBefore time.Duration
	// StaleAfter per client kind: a head older than this alarms (no update).
	StaleAfter map[model.ClientKind]time.Duration
}

// DefaultClientExpiryConfig is the ADR's cadence.
func DefaultClientExpiryConfig() ClientExpiryConfig {
	return ClientExpiryConfig{
		WarnBefore:     72 * time.Hour,
		CriticalBefore: 12 * time.Hour,
		StaleAfter: map[model.ClientKind]time.Duration{
			model.ClientKindGnoLightClient: 72 * time.Hour,
			model.ClientKindEthereum:       24 * time.Hour,
			model.ClientKindTendermint:     72 * time.Hour,
		},
	}
}

// ClientExpiry is the liveness check of every light client on both chains
// (docs/design.md §7.6, client_expiry). Alarm only; never a flag.
type ClientExpiry struct {
	Config ClientExpiryConfig
}

func (ClientExpiry) Name() string { return "client_expiry" }

func (c ClientExpiry) Run(_ context.Context, st store.Store, now time.Time) model.CheckResult {
	started := now
	pins, ok := latestPins(st)
	if !ok {
		return Skip(c.Name(), started, "no collection yet")
	}
	var clients []model.Client
	var prov []model.Provenance
	var stale []string
	for _, kind := range []string{store.KindGnoClients, store.KindEthClients} {
		for _, s := range st.Snapshots(kind) {
			cl, ok := s.Value.(model.Client)
			if !ok {
				continue
			}
			// Only subjects from their chain's latest successful collection
			// are evaluated (docs/design.md §7.11); the rest are stale.
			if !pins.Fresh(cl.Chain, s.Provenance) {
				stale = append(stale, fmt.Sprintf("%s/%s", cl.Chain, clientLabel(cl)))
				continue
			}
			clients = append(clients, cl)
			prov = append(prov, s.Provenance)
		}
	}
	if len(clients) == 0 {
		if len(stale) > 0 {
			return Skip(c.Name(), started, "every client snapshot is stale: "+collectionErrors(pins))
		}
		return Skip(c.Name(), started, "no client snapshot yet")
	}
	var findings []model.Finding
	for _, cl := range clients {
		findings = append(findings, c.evaluate(cl, now))
	}
	r := Done(c.Name(), started, findings, prov)
	if len(stale) > 0 {
		r.Reason = fmt.Sprintf("%d stale subject(s) skipped (%s): %s", len(stale), strings.Join(stale, ", "), collectionErrors(pins))
	}
	return r
}

// latestPins reads the latest collection's pins.
func latestPins(st store.Store) (Pins, bool) {
	s, ok := st.Snapshot(store.KindPins, "latest")
	if !ok {
		return Pins{}, false
	}
	p, ok := s.Value.(Pins)
	return p, ok
}

func collectionErrors(p Pins) string {
	var parts []string
	if !p.GnoState.OK {
		parts = append(parts, "gno: "+p.GnoState.Error)
	}
	if !p.EthState.OK {
		parts = append(parts, "ethereum: "+p.EthState.Error)
	}
	if len(parts) == 0 {
		return "collection ok"
	}
	return strings.Join(parts, "; ")
}

func (c ClientExpiry) evaluate(cl model.Client, now time.Time) model.Finding {
	subject := fmt.Sprintf("%s/%s", cl.Chain, clientLabel(cl))
	key := "client_expiry/" + subject
	f := model.Finding{Subject: subject, Severity: model.SeverityInfo, Provenance: []model.Provenance{cl.Provenance}, Values: map[string]string{
		"status":     string(cl.Status),
		"head_time":  cl.HeadTime.UTC().Format(time.RFC3339),
		"expiry":     cl.Expiry.UTC().Format(time.RFC3339),
		"head":       fmt.Sprintf("%d", cl.LatestHeight),
		"trusting_s": fmt.Sprintf("%d", int64(cl.TrustingPeriod.Seconds())),
	}}
	if cl.Provenance.Verification == model.VerificationUnverified {
		f.Values["unverified"] = "true"
	}
	switch {
	case cl.Status == model.ClientFrozen || cl.Frozen:
		f.Severity, f.AlarmKey = model.SeverityCritical, key
		f.Title = fmt.Sprintf("client %s is frozen", subject)
		f.Detail = "a frozen client proves nothing; recovery is a route replacement or an in-place recovery (ADR §7.12)"
		return f
	case cl.Status == model.ClientExpired || (!cl.Expiry.IsZero() && !now.Before(cl.Expiry)):
		f.Severity, f.AlarmKey = model.SeverityCritical, key
		f.Title = fmt.Sprintf("client %s has expired", subject)
		f.Detail = "no update within the trusting period; no proof can be verified until it is recovered (ADR §7.12)"
		return f
	case cl.Status != "" && cl.Status != model.ClientActive:
		f.Severity, f.AlarmKey = model.SeverityWarning, key
		f.Title = fmt.Sprintf("client %s is %s", subject, strings.ToLower(string(cl.Status)))
		return f
	}
	if cl.Expiry.IsZero() {
		f.Title = fmt.Sprintf("client %s: expiry unknown", subject)
		f.Severity, f.AlarmKey = model.SeverityWarning, key
		f.Detail = "the client state does not give a head time; cannot derive the expiry"
		return f
	}
	left := cl.Expiry.Sub(now)
	f.Values["time_left_s"] = fmt.Sprintf("%d", int64(left.Seconds()))
	stale := c.Config.StaleAfter[cl.Kind]
	age := now.Sub(cl.HeadTime)
	f.Values["head_age_s"] = fmt.Sprintf("%d", int64(age.Seconds()))
	switch {
	case c.Config.CriticalBefore > 0 && left <= c.Config.CriticalBefore:
		f.Severity, f.AlarmKey = model.SeverityCritical, key
		f.Title = fmt.Sprintf("client %s expires in %s", subject, left.Round(time.Minute))
		f.Detail = "update the client now; after the expiry every proof fails and the route needs a recovery"
	case c.Config.WarnBefore > 0 && left <= c.Config.WarnBefore:
		f.Severity, f.AlarmKey = model.SeverityWarning, key
		f.Title = fmt.Sprintf("client %s expires in %s", subject, left.Round(time.Minute))
		f.Detail = "the relayer should update it at least daily (ADR §9)"
	case stale > 0 && age > stale:
		f.Severity, f.AlarmKey = model.SeverityWarning, key
		f.Title = fmt.Sprintf("client %s not updated for %s", subject, age.Round(time.Hour))
		f.Detail = fmt.Sprintf("the head is older than the %s update alarm (ADR §9)", stale)
	default:
		f.Title = fmt.Sprintf("client %s active, expires in %s", subject, left.Round(time.Hour))
	}
	return f
}

func clientLabel(cl model.Client) string {
	if cl.ID != "" {
		return cl.ID
	}
	return cl.Address
}
