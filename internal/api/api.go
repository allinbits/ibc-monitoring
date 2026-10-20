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
	AutoFlag    string    `json:"autoflag"` // "off" | "dry-run" | "on"
	Worker      bool      `json:"worker"`
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
	m.HandleFunc("GET /api/v1/info", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.Info) })
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	_, ok := s.Store.Snapshot(store.KindPins, "latest")
	status := "starting"
	if ok {
		status = "ok"
	}
	writeJSON(w, map[string]any{"status": status, "time": time.Now().UTC()})
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
	writeJSON(w, o)
}

func values(snaps []store.Snapshot) []any {
	out := make([]any, 0, len(snaps))
	for _, sn := range snaps {
		out = append(out, sn.Value)
	}
	return out
}

func (s *Server) clients(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"gno":      values(s.Store.Snapshots(store.KindGnoClients)),
		"ethereum": values(s.Store.Snapshots(store.KindEthClients)),
	})
}

// RouteView pairs the two chains' views of one route.
type RouteView struct {
	Key      string `json:"key"`
	Gno      any    `json:"gno"`
	Ethereum any    `json:"ethereum"`
}

func (s *Server) routes(w http.ResponseWriter, _ *http.Request) {
	var out []RouteView
	for _, sn := range s.Store.Snapshots(store.KindGnoRoutes) {
		rv := RouteView{Key: sn.Key, Gno: sn.Value}
		if es, ok := s.Store.Snapshot(store.KindEthRoutes, sn.Key); ok {
			rv.Ethereum = es.Value
		}
		out = append(out, rv)
	}
	if out == nil {
		out = []RouteView{}
	}
	writeJSON(w, out)
}

func (s *Server) governance(w http.ResponseWriter, _ *http.Request) {
	g, _ := s.Store.Snapshot(store.KindGnoGovernance, "ibcgov")
	e, _ := s.Store.Snapshot(store.KindEthGovernance, "ibcadmin")
	writeJSON(w, map[string]any{"gno": g.Value, "ethereum": e.Value})
}

func (s *Server) checks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.Store.LatestResults())
}

func (s *Server) checkHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	writeJSON(w, s.Store.ResultHistory(r.PathValue("name"), limit))
}

func (s *Server) alarms(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	writeJSON(w, s.Store.Alarms(all))
}

func (s *Server) alarmEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	writeJSON(w, s.Store.AlarmEvents(limit))
}

func (s *Server) jobs(w http.ResponseWriter, _ *http.Request) {
	if s.Jobs == nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, s.Jobs())
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
