package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/allinbits/ibc-monitoring/abi"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc/ethrpctest"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/redact"
)

func TestCodeHashAndRedaction(t *testing.T) {
	redact.Reset()
	t.Cleanup(redact.Reset)
	redact.Register("https://rpc.example/v3/SECRETTOKEN")
	srv := ethrpctest.New(31337, ethrpctest.Block{Number: 5, Timestamp: 1})
	t.Cleanup(srv.Close)
	addr := common.HexToAddress("0xcafe")
	code := []byte{0x60, 0x80, 0x60, 0x40}
	srv.Contract(addr, abi.MustLoad(abi.Voucher), code)
	c, err := ethrpc.Dial(context.Background(), srv.URL, "fixture", ethrpc.Finality{Mode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{Eth: c}
	j := w.CodeHash(context.Background(), CodeHashTarget{Subject: "decision 1/7 kind 7 via https://rpc.example/v3/SECRETTOKEN", Address: addr, Expected: crypto.Keccak256Hash(code)}, 5, model.Provenance{})
	if j.Status != "passed" || len(j.Checks) != 3 {
		t.Fatalf("job %+v", j)
	}
	if strings.Contains(j.Subject, "SECRETTOKEN") {
		t.Fatalf("subject leaks: %s", j.Subject)
	}
	// A mismatching pin fails.
	j = w.CodeHash(context.Background(), CodeHashTarget{Subject: "x", Address: addr, Expected: common.HexToHash("0x01")}, 5, model.Provenance{})
	if j.Status != "failed" {
		t.Fatalf("job %+v", j)
	}
	// A build job never records environment values.
	j = w.BuildVerify(context.Background(), "candidate", "72a41de", map[string]string{"ETH_RPC_URL": "https://rpc.example/v3/SECRETTOKEN", "CANDIDATES_ONLY": "true"})
	if strings.Contains(j.Verdict, "SECRETTOKEN") || strings.Contains(j.Verdict, "rpc.example") || !strings.Contains(j.Verdict, "ETH_RPC_URL") {
		t.Fatalf("verdict: %s", j.Verdict)
	}
	for _, job := range w.Jobs().([]Job) {
		if strings.Contains(job.Verdict+job.Log+job.Subject, "SECRETTOKEN") {
			t.Fatalf("stored job leaks: %+v", job)
		}
	}
}
