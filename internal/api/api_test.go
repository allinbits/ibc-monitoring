package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	st.PutAlarm(model.Alarm{Key: "a", State: model.AlarmRaised, Title: "<script>alert(1)</script>"})
	srv := httptest.NewServer((&Server{Store: st, Info: Info{Version: "test", Public: true, StartedAt: time.Now()}}).Handler())
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
	if code != 200 || !strings.Contains(body, `"public":true`) {
		t.Fatalf("info %d %s", code, body)
	}
	code, body = get("/api/v1/worker/jobs")
	if code != 200 || strings.TrimSpace(body) != "[]" {
		t.Fatalf("jobs %d %s", code, body)
	}
	code, _ = get("/api/v1/nope")
	if code != 404 {
		t.Fatalf("unknown api path %d", code)
	}
	// With no console build embedded, the root explains itself.
	code, body = get("/")
	if code != 200 || !strings.Contains(body, "not built") {
		t.Fatalf("root %d %s", code, body)
	}
}
