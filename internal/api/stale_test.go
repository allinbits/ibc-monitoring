package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

type pins struct {
	last map[model.Chain]uint64
}

func (p pins) Fresh(chain model.Chain, prov model.Provenance) bool {
	return prov.Collection == p.last[chain]
}

// Served values say whether they are from their chain's latest successful
// collection (docs/design.md §4.4, §7.11).
func TestStaleMarking(t *testing.T) {
	st := store.NewMemory(10)
	fresh := model.Client{Chain: model.ChainGno, ID: "fresh", Provenance: model.Provenance{Chain: model.ChainGno, Collection: 3}}
	stale := model.Client{Chain: model.ChainEthereum, ID: "stale", Provenance: model.Provenance{Chain: model.ChainEthereum, Collection: 2}}
	st.PutSnapshot(store.Snapshot{Kind: store.KindGnoClients, Key: "fresh", Value: fresh, Provenance: fresh.Provenance})
	st.PutSnapshot(store.Snapshot{Kind: store.KindEthClients, Key: "stale", Value: stale, Provenance: stale.Provenance})
	st.PutSnapshot(store.Snapshot{Kind: store.KindPins, Key: "latest", Value: pins{last: map[model.Chain]uint64{model.ChainGno: 3, model.ChainEthereum: 3}}})
	srv := httptest.NewServer((&Server{Store: st}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/clients")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Gno      []model.Client `json:"gno"`
		Ethereum []model.Client `json:"ethereum"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Gno) != 1 || out.Gno[0].Provenance.Stale || len(out.Ethereum) != 1 || !out.Ethereum[0].Provenance.Stale {
		t.Fatalf("stale marking: %+v %+v", out.Gno, out.Ethereum)
	}
	// Without any collection every value is stale.
	st2 := store.NewMemory(10)
	st2.PutSnapshot(store.Snapshot{Kind: store.KindGnoClients, Key: "fresh", Value: fresh, Provenance: fresh.Provenance})
	srv2 := httptest.NewServer((&Server{Store: st2}).Handler())
	defer srv2.Close()
	resp2, err := http.Get(srv2.URL + "/api/v1/clients")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.Gno[0].Provenance.Stale {
		t.Fatal("no collection must read as stale")
	}
}
