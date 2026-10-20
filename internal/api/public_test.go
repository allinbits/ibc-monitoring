package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

// A private instance keeps the operator's diagnostics.
func TestPrivateKeepsDiagnostics(t *testing.T) {
	st := store.NewMemory(10)
	st.PutSnapshot(store.Snapshot{Kind: store.KindPins, Key: "latest", Value: map[string]any{"gno_error": "Post \"http://127.0.0.1:1\": connection refused"}})
	st.PutResult(model.CheckResult{Check: "c", Status: model.CheckSkipped, Reason: "every client snapshot is stale", Findings: []model.Finding{}})
	srv := httptest.NewServer((&Server{Store: st, Info: Info{Version: "test", StartedAt: time.Now(), GnoNode: "own-gno-node"}}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/overview")
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
	body := sb.String()
	for _, want := range []string{"connection refused", "own-gno-node", "every client snapshot is stale"} {
		if !strings.Contains(body, want) {
			t.Fatalf("private overview lost %q: %s", want, body)
		}
	}
}
