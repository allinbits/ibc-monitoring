package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

func TestAPI(t *testing.T) {
	st := store.NewMemory(10)
	cl := model.Client{Chain: model.ChainGno, ID: "ethereum-1", Status: model.ClientActive}
	st.PutSnapshot(store.Snapshot{Kind: store.KindGnoClients, Key: cl.ID, Value: cl})
	r := model.Route{Key: "ethereum-1", Denoms: []model.Denom{{Denom: "ugnot", CapFloor: model.MustAmount("340282366920938463463374607431768211455")}}}
	st.PutSnapshot(store.Snapshot{Kind: store.KindGnoRoutes, Key: r.Key, Value: r})
	st.PutSnapshot(store.Snapshot{Kind: store.KindEthRoutes, Key: r.Key, Value: model.EthRoute{RouteKey: "client-0"}})
	st.PutResult(model.CheckResult{Check: "client_expiry", Status: model.CheckOK, Findings: []model.Finding{}})
	st.PutAlarm(model.Alarm{Key: "a", State: model.AlarmRaised, Title: "<script>alert(1)</script>", Detail: "node own-gno-node said connection refused"})
	jobs := func() any {
		return []map[string]any{{"id": 1, "kind": "codehash", "subject": "decision 1/7", "status": "passed", "verdict": "matched via https://rpc.example/v3/TOKENTOKENTOKENTOKEN", "log": "Post \"http://127.0.0.1:1\"", "checks": []any{map[string]any{"name": "n", "note": "secret-ish"}}}}
	}
	srv := httptest.NewServer((&Server{Store: st, Info: Info{Version: "test", Public: true, StartedAt: time.Now(), GnoNode: "own-gno-node", EthNode: "own-eth-node", AutoFlagRequested: "dry-run", AutoFlagEffective: "unavailable"}, Jobs: jobs}).Handler())
	defer srv.Close()

	get := func(path string) (int, string) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return resp.StatusCode, sb.String()
	}
	code, body := get("/api/v1/health")
	if code != 200 || !strings.Contains(body, "starting") {
		t.Fatalf("health %d %s", code, body)
	}
	code, body = get("/api/v1/routes")
	if code != 200 {
		t.Fatalf("routes %d", code)
	}
	var routes []RouteView
	if err := json.Unmarshal([]byte(body), &routes); err != nil || len(routes) != 1 || routes[0].Ethereum == nil {
		t.Fatalf("routes %s %v", body, err)
	}
	// Amounts are decimal strings, never numbers.
	if !strings.Contains(body, `"cap_floor":"340282366920938463463374607431768211455"`) {
		t.Fatalf("amount encoding: %s", body)
	}
	code, body = get("/api/v1/clients")
	if code != 200 || !strings.Contains(body, `"ethereum-1"`) {
		t.Fatalf("clients %d %s", code, body)
	}
	code, body = get("/api/v1/alarms")
	// Untrusted text is HTML-escaped in the JSON encoding too.
	if code != 200 || strings.Contains(body, "<script>") {
		t.Fatalf("alarms %d %s", code, body)
	}
	code, body = get("/api/v1/checks/client_expiry/history?limit=5")
	if code != 200 || !strings.Contains(body, `"client_expiry"`) {
		t.Fatalf("history %d %s", code, body)
	}
	code, body = get("/api/v1/info")
	if code != 200 || !strings.Contains(body, `"public":true`) || !strings.Contains(body, `"autoflag_effective":"unavailable"`) || !strings.Contains(body, `"autoflag_requested":"dry-run"`) {
		t.Fatalf("info %d %s", code, body)
	}
	// Public mode: the node label and any error text never reach a visitor,
	// on any endpoint (the filter is central).
	if strings.Contains(body, "own-gno-node") {
		t.Fatalf("public info leaks the node label: %s", body)
	}
	st.PutSnapshot(store.Snapshot{Kind: store.KindPins, Key: "latest", Value: map[string]any{
		"gno_error":       `Post "http://127.0.0.1:1": connection refused`,
		"big_id":          uint64(9007199254740993),
		"gno_cross_check": "second provider unavailable: dial tcp 10.0.0.9:26657",
		"gno_state":       map[string]any{"ok": false, "error": "secret-ish text", "consecutive_failures": 3},
	}})
	for _, path := range []string{"/api/v1/overview", "/api/v1/alarms", "/api/v1/alarms/events", "/api/v1/checks", "/api/v1/clients", "/api/v1/routes", "/api/v1/governance", "/api/v1/worker/jobs"} {
		code, body = get(path)
		if code != 200 {
			t.Fatalf("%s: %d", path, code)
		}
		for _, leak := range []string{"127.0.0.1", "connection refused", "secret-ish", "10.0.0.9", "own-gno-node", "consecutive_failures", "TOKENTOKEN"} {
			if strings.Contains(body, leak) {
				t.Fatalf("%s leaks %q: %s", path, leak, body)
			}
		}
	}
	_, body = get("/api/v1/overview")
	if !strings.Contains(body, `"gno_error":"unavailable"`) || !strings.Contains(body, `"error":"unavailable"`) {
		t.Fatalf("overview must say unavailable: %s", body)
	}
	code, body = get("/api/v1/health")
	if code != 200 || !strings.Contains(body, "degraded") {
		t.Fatalf("health %d %s", code, body)
	}
	// The public filter never passes a number through a float64.
	_, body = get("/api/v1/overview")
	if !strings.Contains(body, "9007199254740993") {
		t.Fatalf("precision lost in the public filter: %s", body)
	}
	code, body = get("/api/v1/worker/jobs")
	if code != 200 || !strings.Contains(body, `"passed":true`) || strings.Contains(body, "verdict") || strings.Contains(body, "TOKEN") || strings.Contains(body, "checks") {
		t.Fatalf("public jobs %d %s", code, body)
	}
	code, _ = get("/api/v1/nope")
	if code != 404 {
		t.Fatalf("unknown api path %d", code)
	}
	// The root serves the console when it is built in, else a notice; either
	// way it is never a 404 (SPA fallback).
	code, body = get("/")
	served := strings.Contains(body, "not built") || strings.Contains(body, `<div id="root">`)
	if code != 200 || !served {
		t.Fatalf("root %d %s", code, body)
	}
	if code, _ = get("/clients"); code != 200 {
		t.Fatalf("spa fallback %d", code)
	}
}

// Freshness marking of the relationship view never writes into the stored
// finding: concurrent readers of /routes and /checks, with results being
// stored meanwhile, race on nothing (tenth round, finding 2; run under
// -race).
func TestRoutesAndChecksConcurrently(t *testing.T) {
	st := store.NewMemory(10)
	st.PutSnapshot(store.Snapshot{Kind: store.KindEthRoutes, Key: "client-0", Value: model.EthRoute{RouteKey: "client-0", GnoRoute: "ethereum-1"}, Provenance: model.Provenance{Chain: model.ChainEthereum, Collection: 1}})
	st.PutSnapshot(store.Snapshot{Kind: store.KindGnoRoutes, Key: "ethereum-1", Value: model.Route{Key: "ethereum-1"}, Provenance: model.Provenance{Chain: model.ChainGno, Collection: 1}})
	result := func(i int) model.CheckResult {
		return model.CheckResult{Check: "route_relationship", Status: model.CheckOK, FinishedAt: time.Now(), Findings: []model.Finding{{
			Subject: "client-0", Severity: model.SeverityInfo, Title: "agree",
			Provenance: []model.Provenance{{Chain: model.ChainEthereum, Collection: uint64(i)}, {Chain: model.ChainGno, Collection: uint64(i)}},
		}}}
	}
	st.PutResult(result(1))
	srv := httptest.NewServer((&Server{Store: st, Info: Info{Version: "test", StartedAt: time.Now()}}).Handler())
	defer srv.Close()
	get := func(path string) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				get("/api/v1/routes")
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				get("/api/v1/checks")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 2; i < 60; i++ {
			st.PutResult(result(i))
		}
	}()
	wg.Wait()
	// The stored finding's provenance was never marked stale in place.
	r, _ := st.LatestResult("route_relationship")
	for _, p := range r.Findings[0].Provenance {
		if p.Stale {
			t.Fatal("stored evidence was marked stale by a reader")
		}
	}
}
