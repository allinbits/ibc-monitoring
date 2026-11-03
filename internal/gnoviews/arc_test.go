package gnoviews

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
)

func arcRender(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/arc-client.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestArcClient(t *testing.T) {
	v, pin, srv := devnet(t)
	render := arcRender(t)
	srv.Set("vm/qrender|"+DefaultPaths.Core+":clients/arc-1", render)
	srv.Set("vm/qrender|"+DefaultPaths.Core+":clients?page=1&limit=100", `{"items":[`+render+`],"page":1,"total":1}`)
	for query, answer := range map[string]string{
		`VerifierVersion("arc-1")`:                `("arc/v0" string)`,
		`ClientTrustGeneration("arc-1")`:          `(2 uint64)`,
		`IsClientRetiredFor("arc-1", "transfer")`: `(false bool)`,
		`IsClientRetiredFor("arc-1", "gov")`:      `(false bool)`,
	} {
		srv.Set("vm/qeval|"+DefaultPaths.Core+"."+query, answer)
	}
	clients, err := v.Clients(context.Background(), pin)
	if err != nil || len(clients) != 1 {
		t.Fatalf("clients: %+v %v", clients, err)
	}
	c := clients[0]
	if c.Kind != model.ClientKindArc || c.ID != "arc-1" || c.Status != model.ClientActive || c.ChainID != "5042002" || c.LatestHeight != 12345678 || c.LatestExecutionBlock != 0 || c.VerifierVersion != "arc/v0" || c.CounterpartyID != "client-0" {
		t.Fatalf("client: %+v", c)
	}
	if !c.HeadTime.Equal(time.Unix(1792600000, 0)) || c.TrustingPeriod != 14*24*time.Hour || !c.Expiry.Equal(c.HeadTime.Add(c.TrustingPeriod)) {
		t.Fatalf("expiry: %+v", c)
	}
	if c.Arc == nil || c.Arc.NextTotalPower != "9007199254740993" || len(c.Arc.NextValidators) != 1 || c.Arc.NextValidators[0].Power != c.Arc.NextTotalPower || c.Arc.RegistryStorageRoot != "FhcY" || c.Arc.RegistryAddress != "AQID" || c.Arc.StateRoot != "ExQV" || c.Arc.BlockHash != "EBES" {
		t.Fatalf("arc: %+v", c.Arc)
	}
	// uint64 heights and times may also be quoted in renders.
	var cj clientJSON
	if err := json.Unmarshal([]byte(strings.ReplaceAll(render, `1792600000`, `"1792600000"`)), &cj); err != nil {
		t.Fatal(err)
	}
	var quoted model.Client
	if err := v.fillArc(&quoted, cj); err != nil || !quoted.Expiry.Equal(c.Expiry) {
		t.Fatalf("quoted timestamp: %+v %v", quoted, err)
	}
}

func TestArcInvalidState(t *testing.T) {
	v := &Views{}
	render := arcRender(t)
	for _, pair := range [][2]string{
		{`"1209600"`, `"18446744073709551615"`},
		{`"12345678"`, `"18446744073709551616"`},
		{`1792600000`, `-1`},
		{`"9007199254740993"`, `"18446744073709551616"`},
	} {
		var cj clientJSON
		if err := json.Unmarshal([]byte(strings.ReplaceAll(render, pair[0], pair[1])), &cj); err != nil {
			t.Fatal(err)
		}
		if err := v.fillArc(&model.Client{}, cj); err == nil {
			t.Fatalf("accepted invalid %s", pair[1])
		}
	}
	var cj clientJSON
	if err := json.Unmarshal([]byte(render), &cj); err != nil {
		t.Fatal(err)
	}
	cj.LastConsensusState = json.RawMessage(`null`)
	var c model.Client
	if err := v.fillArc(&c, cj); err != nil || !c.Expiry.IsZero() || c.Arc == nil || c.Arc.NextValidators == nil {
		t.Fatalf("missing consensus: %+v %v", c, err)
	}
}
