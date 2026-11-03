package ethrpc

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc/ethrpctest"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

func TestArcFinalizedTag(t *testing.T) {
	srv := ethrpctest.New(5042002, ethrpctest.Block{Number: 101, Timestamp: 1792600000})
	defer srv.Close()
	srv.AddBlock(ethrpctest.Block{Number: 102, Timestamp: 1792600001})
	srv.SetFinalized(101)
	c, err := Dial(context.Background(), srv.URL, "arc", Finality{Mode: "tag"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	b, err := c.Pinned(context.Background())
	if err != nil || b.Number != 101 || b.Finality != "finalized" {
		t.Fatalf("pin: %+v %v", b, err)
	}
	// The finalized-tag path does not call beacon or proof methods.
	for _, call := range srv.Calls {
		if call != "eth_getBlockByNumber" {
			t.Fatalf("unexpected %s", call)
		}
	}
}

func TestLogChunkWindows(t *testing.T) {
	for _, span := range [][3]uint64{{100, 106, 3}, {math.MaxUint64 - 2, math.MaxUint64, 2}, {3, 2, 1000}} {
		var windows [][2]uint64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				ID     json.RawMessage   `json:"id"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				return
			}
			var arg struct {
				From string `json:"fromBlock"`
				To   string `json:"toBlock"`
			}
			if err := json.Unmarshal(req.Params[0], &arg); err != nil {
				t.Error(err)
				return
			}
			from, err := hexutil.DecodeUint64(arg.From)
			if err != nil {
				t.Error(err)
				return
			}
			to, err := hexutil.DecodeUint64(arg.To)
			if err != nil {
				t.Error(err)
				return
			}
			windows = append(windows, [2]uint64{from, to})
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": []any{}}); err != nil {
				t.Error(err)
			}
		}))
		c, err := Dial(context.Background(), srv.URL, "logs", Finality{Mode: "tag"})
		if err != nil {
			t.Fatal(err)
		}
		c.LogChunk = span[2]
		_, err = c.Logs(context.Background(), common.Address{}, nil, span[0], span[1])
		c.Close()
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		var want [][2]uint64
		switch span[0] {
		case 100:
			want = [][2]uint64{{100, 102}, {103, 105}, {106, 106}}
		case math.MaxUint64 - 2:
			want = [][2]uint64{{math.MaxUint64 - 2, math.MaxUint64 - 1}, {math.MaxUint64, math.MaxUint64}}
		}
		if !reflect.DeepEqual(windows, want) {
			t.Fatalf("windows: %v want %v", windows, want)
		}
	}
}
