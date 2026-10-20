// Package api serves the read-only HTTP API the console reads and the
// embedded console itself. No endpoint changes chain state; the only writes
// are the operator's "run again" of a worker job, which a public instance
// refuses (docs/design.md §10.2).
package api

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

//go:embed all:webdist
var webdist embed.FS

// Info describes the instance to the console.
type Info struct {
	Version     string    `json:"version"`
	Public      bool      `json:"public"`
	StartedAt   time.Time `json:"started_at"`
	GnoNode     string    `json:"gno_node"`
	EthNode     string    `json:"eth_node"`
	EthFinality string    `json:"eth_finality"`
	// AutoFlagRequested is the configuration ("off" | "dry-run" | "on");
	// AutoFlagEffective is what runs: "off", "dry-run", "on", or
	// "unavailable" while the sink is not built (design §8.4). The console
	// shows the effective status and never claims a sink that does not run.
	AutoFlagRequested string `json:"autoflag_requested"`
	AutoFlagEffective string `json:"autoflag_effective"`
	Worker            bool   `json:"worker"`
}

// Server is the HTTP API.
type Server struct {
	Store store.Store
	Info  Info
	// Jobs lists the worker's jobs (nil when the worker is off).
	Jobs func() any
	mux  *http.ServeMux
}

// Handler returns the HTTP handler: /api/v1/* and the console.
func (s *Server) Handler() http.Handler {
	if s.mux != nil {
		return s.mux
	}
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/health", s.health)
	m.HandleFunc("GET /api/v1/info", func(w http.ResponseWriter, _ *http.Request) { s.writeJSON(w, s.Info) })
	m.HandleFunc("GET /api/v1/overview", s.overview)
	m.HandleFunc("GET /api/v1/clients", s.clients)
	m.HandleFunc("GET /api/v1/routes", s.routes)
	m.HandleFunc("GET /api/v1/governance", s.governance)
	m.HandleFunc("GET /api/v1/checks", s.checks)
	m.HandleFunc("GET /api/v1/checks/{name}/history", s.checkHistory)
	m.HandleFunc("GET /api/v1/alarms", s.alarms)
	m.HandleFunc("GET /api/v1/alarms/events", s.alarmEvents)
	m.HandleFunc("GET /api/v1/worker/jobs", s.jobs)
	m.Handle("/", s.console())
	s.mux = m
	return m
}

// writeJSON encodes v; on a public instance every response goes through the
// central filter first (docs/design.md §10.2).
func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	if s.Info.Public {
		v = publicFilter(v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	pins, ok := s.Store.Snapshot(store.KindPins, "latest")
	status := "starting"
	if ok {
		status = "ok"
		// The collector reports each chain's state in its pins; the health
		// answer says "degraded" without the error text.
		if b, err := json.Marshal(pins.Value); err == nil && (strings.Contains(string(b), `"gno_error":"`) || strings.Contains(string(b), `"eth_error":"`)) {
			status = "degraded"
		}
	}
	s.writeJSON(w, map[string]any{"status": status, "time": time.Now().UTC()})
}

// Overview is the console's landing data.
type Overview struct {
	Info   Info                `json:"info"`
	Pins   any                 `json:"pins"`
	Counts map[string]int      `json:"counts"`
	Alarms []model.Alarm       `json:"alarms"`
	Checks []model.CheckResult `json:"checks"`
}

func (s *Server) overview(w http.ResponseWriter, _ *http.Request) {
	pins, _ := s.Store.Snapshot(store.KindPins, "latest")
	o := Overview{
		Info: s.Info,
		Pins: pins.Value,
		Counts: map[string]int{
			"gno_clients": len(s.Store.Snapshots(store.KindGnoClients)),
			"eth_clients": len(s.Store.Snapshots(store.KindEthClients)),
			"routes":      len(s.Store.Snapshots(store.KindGnoRoutes)),
		},
		Alarms: s.Store.Alarms(false),
		Checks: s.Store.LatestResults(),
	}
	s.writeJSON(w, o)
}

// freshness answers whether a snapshot of a chain is from that chain's
// latest successful, header-agreed collection (docs/design.md §7.11); the
// collector stores the rule in the pins snapshot under this interface.
type freshness interface {
	Fresh(chain model.Chain, prov model.Provenance) bool
}

func (s *Server) fresh() freshness {
	pins, ok := s.Store.Snapshot(store.KindPins, "latest")
	if !ok {
		return nil
	}
	f, _ := pins.Value.(freshness)
	return f
}

// mark returns the snapshot's value with its provenance's Stale flag set
// when it is not fresh. Values are copied; the store is untouched.
func (s *Server) mark(sn store.Snapshot) any {
	f := s.fresh()
	stale := f == nil || !f.Fresh(sn.Provenance.Chain, sn.Provenance)
	switch v := sn.Value.(type) {
	case model.Client:
		v.Provenance.Stale = stale
		return v
	case model.Route:
		v.Provenance.Stale = stale
		return v
	case model.EthRoute:
		v.Provenance.Stale = stale
		return v
	case model.GnoGovernance:
		v.Provenance.Stale = stale
		return v
	case model.EthGovernance:
		v.Provenance.Stale = stale
		return v
	}
	return sn.Value
}

func (s *Server) values(snaps []store.Snapshot) []any {
	out := make([]any, 0, len(snaps))
	for _, sn := range snaps {
		out = append(out, s.mark(sn))
	}
	return out
}

func (s *Server) clients(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, map[string]any{
		"gno":      s.values(s.Store.Snapshots(store.KindGnoClients)),
		"ethereum": s.values(s.Store.Snapshots(store.KindEthClients)),
	})
}

// RouteView pairs the two chains' views of one route, with the cross-chain
// relationship as the route_relationship check last evaluated it (design
// §4.2): a derived value with two dependencies, never a field cached at
// collection.
type RouteView struct {
	Key          string            `json:"key"`
	Gno          any               `json:"gno"`
	Ethereum     any               `json:"ethereum"`
	Relationship *RelationshipView `json:"relationship,omitempty"`
}

// RelationshipView is the route_relationship check's latest verdict on one
// Ethereum route.
type RelationshipView struct {
	// Deferred is true when the latest evaluation could not judge the
	// route (a bundle was not fresh); Mismatch is the verdict otherwise.
	Deferred    bool               `json:"deferred"`
	Mismatch    bool               `json:"mismatch"`
	Severity    model.Severity     `json:"severity,omitempty"`
	Title       string             `json:"title,omitempty"`
	Detail      string             `json:"detail,omitempty"`
	EvaluatedAt time.Time          `json:"evaluated_at"`
	Provenance  []model.Provenance `json:"provenance"`
}

// relationship reads the latest route_relationship result for routeKey.
func (s *Server) relationship(routeKey string) *RelationshipView {
	r, ok := s.Store.LatestResult("route_relationship")
	if !ok {
		return nil
	}
	v := &RelationshipView{EvaluatedAt: r.FinishedAt, Provenance: []model.Provenance{}}
	for _, f := range r.Findings {
		if f.Subject != routeKey {
			continue
		}
		v.Mismatch, v.Severity, v.Title, v.Detail = f.AlarmKey != "", f.Severity, f.Title, f.Detail
		// A copy: the store hands out shallow copies, so marking the
		// finding's own slice would rewrite stored evidence under readers.
		v.Provenance = append([]model.Provenance(nil), f.Provenance...)
		if fr := s.fresh(); fr != nil {
			for i := range v.Provenance {
				v.Provenance[i].Stale = !fr.Fresh(v.Provenance[i].Chain, v.Provenance[i])
			}
		}
		return v
	}
	v.Deferred = true
	return v
}

func (s *Server) routes(w http.ResponseWriter, _ *http.Request) {
	var out []RouteView
	// Ethereum routes are keyed by Ethereum's own route key and name the
	// gno route they were matched to; an unmatched one is listed alone.
	eth := s.Store.Snapshots(store.KindEthRoutes)
	matched := map[string]bool{}
	for _, sn := range s.Store.Snapshots(store.KindGnoRoutes) {
		rv := RouteView{Key: sn.Key, Gno: s.mark(sn)}
		for _, es := range eth {
			if er, ok := es.Value.(model.EthRoute); ok && (er.GnoRoute == sn.Key || es.Key == sn.Key) {
				rv.Ethereum = s.mark(es)
				rv.Relationship = s.relationship(es.Key)
				matched[es.Key] = true
				break
			}
		}
		out = append(out, rv)
	}
	for _, es := range eth {
		if !matched[es.Key] {
			out = append(out, RouteView{Key: es.Key, Ethereum: s.mark(es), Relationship: s.relationship(es.Key)})
		}
	}
	if out == nil {
		out = []RouteView{}
	}
	s.writeJSON(w, out)
}

func (s *Server) governance(w http.ResponseWriter, _ *http.Request) {
	g, gok := s.Store.Snapshot(store.KindGnoGovernance, "ibcgov")
	e, eok := s.Store.Snapshot(store.KindEthGovernance, "ibcadmin")
	out := map[string]any{"gno": nil, "ethereum": nil}
	if gok {
		out["gno"] = s.mark(g)
	}
	if eok {
		out["ethereum"] = s.mark(e)
	}
	s.writeJSON(w, out)
}

func (s *Server) checks(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, s.Store.LatestResults())
}

func (s *Server) checkHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	s.writeJSON(w, s.Store.ResultHistory(r.PathValue("name"), limit))
}

func (s *Server) alarms(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	s.writeJSON(w, s.Store.Alarms(all))
}

func (s *Server) alarmEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	s.writeJSON(w, s.Store.AlarmEvents(limit))
}

func (s *Server) jobs(w http.ResponseWriter, _ *http.Request) {
	if s.Jobs == nil {
		s.writeJSON(w, []any{})
		return
	}
	if s.Info.Public {
		s.writeJSON(w, publicJobs(s.Jobs()))
		return
	}
	s.writeJSON(w, s.Jobs())
}

// console serves the embedded build with an SPA fallback to index.html. With
// no build present (a backend-only checkout) it answers a short notice.
func (s *Server) console() http.Handler {
	sub, err := fs.Sub(webdist, "webdist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := sub.Open(p); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		if f, err := sub.Open("index.html"); err == nil {
			_ = f.Close()
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ibcmon: the console is not built into this binary (run `make build-web`); the API is under /api/v1/\n"))
	})
}
