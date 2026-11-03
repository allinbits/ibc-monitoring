package ethviews

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestHistoryLogsScansOnlyNewFinalizedBlocks(t *testing.T) {
	v, b, srv := fixture(t)
	ctx := context.Background()
	topic := common.HexToHash("0xabcd")
	topics := [][]common.Hash{{topic}}
	v.HistoryFrom = b.Number - 249
	v.rpc.LogChunk = 100
	for _, n := range []uint64{b.Number - 200, b.Number, b.Number + 30} {
		srv.AddLog(types.Log{Address: xferAddr, Topics: []common.Hash{topic}, BlockNumber: n})
	}
	getLogs := func() int {
		n := 0
		for _, c := range srv.Calls {
			if c == "eth_getLogs" {
				n++
			}
		}
		return n
	}
	scan := func(pin uint64, finality string, wantLogs, wantCalls int) {
		t.Helper()
		before := getLogs()
		p := b
		p.Number, p.Finality = pin, finality
		logs, err := v.historyLogs(ctx, xferAddr, topics, p)
		if err != nil {
			t.Fatal(err)
		}
		if len(logs) != wantLogs || getLogs()-before != wantCalls {
			t.Fatalf("pin %d %s: %d logs in %d eth_getLogs, want %d in %d", pin, finality, len(logs), getLogs()-before, wantLogs, wantCalls)
		}
	}
	scan(b.Number, "finalized", 2, 3)     // 250 blocks, 100 a request
	scan(b.Number+50, "finalized", 3, 1)  // only the 50 new blocks
	scan(b.Number+50, "finalized", 3, 0)  // nothing new: no request at all (a 0-block range is skipped)
	scan(b.Number-100, "finalized", 1, 0) // a pin behind the scan: from the cache
	scan(b.Number+50, "latest", 3, 3)     // not final: the whole history again
}
