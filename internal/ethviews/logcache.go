package ethviews

import (
	"context"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
)

// logCache keeps the history scans (HistoryFrom to the pin) of finalized
// blocks, so that each collection reads only the blocks after the last one
// it scanned instead of the whole history again: on a chain with fast
// blocks and a provider that serves short eth_getLogs ranges (Arc and
// drpc's free plan: about 100 blocks a request) the full scan outgrows the
// collection deadline within hours of a deployment.
type logCache struct {
	mu    sync.Mutex
	scans map[string]*logScan
}

// logScan is one (address, topics) scan of [from, to], all finalized.
type logScan struct {
	from, to uint64
	logs     []types.Log
}

func logKey(addr common.Address, topics [][]common.Hash) string {
	var b strings.Builder
	b.WriteString(addr.Hex())
	for _, alts := range topics {
		b.WriteByte('|')
		for _, t := range alts {
			b.WriteString(t.Hex())
			b.WriteByte(',')
		}
	}
	return b.String()
}

// historyLogs reads addr's logs matching topics from HistoryFrom to the pin
// b. A finalized pin's history cannot change, so it is cached and extended;
// any other pin ("latest", "depth:N") is read in full every time.
func (v *Views) historyLogs(ctx context.Context, addr common.Address, topics [][]common.Hash, b ethrpc.Block) ([]types.Log, error) {
	from := v.HistoryFrom
	if b.Finality != "finalized" || b.Number < from {
		return v.rpc.Logs(ctx, addr, topics, from, b.Number)
	}
	key := logKey(addr, topics)
	v.logs.mu.Lock()
	if v.logs.scans == nil {
		v.logs.scans = map[string]*logScan{}
	}
	s := v.logs.scans[key]
	if s == nil || s.from != from {
		s = &logScan{from: from, to: from - 1}
		v.logs.scans[key] = s
	}
	start, logs := s.to+1, s.logs
	v.logs.mu.Unlock()

	if b.Number < start {
		// A pin behind the scan (another provider's head): the cached logs
		// up to it.
		var out []types.Log
		for _, l := range logs {
			if l.BlockNumber <= b.Number {
				out = append(out, l)
			}
		}
		return out, nil
	}
	// Chunk by chunk, each kept as it arrives: a backlog longer than one
	// collection's deadline is worked off over several collections instead
	// of failing every one of them from the start again.
	chunk := v.rpc.LogChunk
	if chunk == 0 {
		chunk = 2000
	}
	for {
		end := b.Number
		if chunk-1 < end-start {
			end = start + chunk - 1
		}
		fresh, err := v.rpc.Logs(ctx, addr, topics, start, end)
		if err != nil {
			return nil, err
		}
		v.logs.mu.Lock()
		if s.to != start-1 { // a concurrent scan moved it: start over from it
			v.logs.mu.Unlock()
			return v.historyLogs(ctx, addr, topics, b)
		}
		s.logs = append(s.logs[:len(s.logs):len(s.logs)], fresh...)
		s.to = end
		logs = s.logs
		v.logs.mu.Unlock()
		if end == b.Number {
			return logs, nil
		}
		start = end + 1
	}
}
