package ethrpc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// gethProofAnswer is what the fake node answers to eth_getProof, whatever
// was asked.
type gethProofAnswer struct {
	Address common.Address
	Slots   []common.Hash
}

func newProofServer(t *testing.T, a gethProofAnswer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "eth_getProof":
			storage := []map[string]any{}
			for _, s := range a.Slots {
				storage = append(storage, map[string]any{"key": s.Hex(), "value": "0x0", "proof": []string{}})
			}
			result = map[string]any{
				"address": a.Address.Hex(), "accountProof": []string{}, "balance": "0x0",
				"codeHash": common.Hash{}.Hex(), "nonce": "0x0", "storageHash": common.Hash{}.Hex(),
				"storageProof": storage,
			}
		default:
			result = nil
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv
}
