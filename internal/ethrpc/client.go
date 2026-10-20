// Package ethrpc reads Ethereum state pinned to one block: eth_call at a
// block number, code, logs in bounded chunks and storage proofs. The block is
// the finalized one (beacon-verified or by tag) or, on networks without
// finality, a depth below the head; the caller records it as provenance.
package ethrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethclient/gethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/allinbits/ibc-monitoring/internal/redact"
)

// Finality says how the pinned block is chosen.
type Finality struct {
	// Mode: "tag" (eth_getBlockByNumber("finalized")), "depth" (latest − Depth,
	// for local networks without finality) or "latest" (tests only).
	Mode  string
	Depth uint64
}

// ParseFinality parses "tag", "latest" or "depth:N".
func ParseFinality(s string) (Finality, error) {
	switch {
	case s == "" || s == "tag" || s == "finalized" || s == "beacon":
		return Finality{Mode: "tag"}, nil
	case s == "latest":
		return Finality{Mode: "latest"}, nil
	case strings.HasPrefix(s, "depth:"):
		n, err := strconv.ParseUint(strings.TrimPrefix(s, "depth:"), 10, 64)
		if err != nil {
			return Finality{}, fmt.Errorf("finality %q: %w", s, err)
		}
		return Finality{Mode: "depth", Depth: n}, nil
	}
	return Finality{}, fmt.Errorf("finality %q: want tag, latest or depth:N", s)
}

// Client talks to one execution-layer JSON-RPC endpoint.
type Client struct {
	rpc      *rpc.Client
	eth      *ethclient.Client
	geth     *gethclient.Client
	name     string
	finality Finality
	// LogChunk bounds eth_getLogs ranges (blocks per request).
	LogChunk uint64
}

// Dial connects to url. name labels the node in provenance.
func Dial(ctx context.Context, url, name string, fin Finality) (*Client, error) {
	rc, err := rpc.DialContext(ctx, url)
	if err != nil {
		return nil, redactErr(fmt.Errorf("ethrpc %s: %w", name, err))
	}
	return &Client{rpc: rc, eth: ethclient.NewClient(rc), geth: gethclient.New(rc), name: name, finality: fin, LogChunk: 2000}, nil
}

// Name returns the provenance label.
func (c *Client) Name() string { return c.name }

// Close closes the connection.
func (c *Client) Close() { c.rpc.Close() }

// Block is the pinned block.
type Block struct {
	Number    uint64
	Hash      common.Hash
	StateRoot common.Hash
	Timestamp uint64
	// Finality is the mode that chose it: "finalized", "depth:N" or "latest".
	Finality string
}

type rawHeader struct {
	Number    hexutil.Uint64 `json:"number"`
	Hash      common.Hash    `json:"hash"`
	StateRoot common.Hash    `json:"stateRoot"`
	Timestamp hexutil.Uint64 `json:"timestamp"`
}

func (c *Client) header(ctx context.Context, tag string) (Block, error) {
	var h *rawHeader
	if err := c.rpc.CallContext(ctx, &h, "eth_getBlockByNumber", tag, false); err != nil {
		return Block{}, redactErr(fmt.Errorf("ethrpc %s: eth_getBlockByNumber %s: %w", c.name, tag, err))
	}
	if h == nil {
		return Block{}, fmt.Errorf("ethrpc %s: block %s not found", c.name, tag)
	}
	return Block{Number: uint64(h.Number), Hash: h.Hash, StateRoot: h.StateRoot, Timestamp: uint64(h.Timestamp)}, nil
}

// Pinned resolves the block every read of a snapshot is pinned to.
func (c *Client) Pinned(ctx context.Context) (Block, error) {
	switch c.finality.Mode {
	case "latest":
		b, err := c.header(ctx, "latest")
		b.Finality = "latest"
		return b, err
	case "depth":
		head, err := c.header(ctx, "latest")
		if err != nil {
			return Block{}, err
		}
		n := head.Number
		if n > c.finality.Depth {
			n -= c.finality.Depth
		} else {
			n = 0
		}
		b, err := c.BlockByNumber(ctx, n)
		b.Finality = "depth:" + strconv.FormatUint(c.finality.Depth, 10)
		return b, err
	default:
		b, err := c.header(ctx, "finalized")
		b.Finality = "finalized"
		return b, err
	}
}

// BlockByNumber reads a block's hash, state root and time.
func (c *Client) BlockByNumber(ctx context.Context, n uint64) (Block, error) {
	return c.header(ctx, hexutil.EncodeUint64(n))
}

// BlockByHash reads a block by hash (for cross-checks).
func (c *Client) BlockByHash(ctx context.Context, h common.Hash) (Block, error) {
	var raw *rawHeader
	if err := c.rpc.CallContext(ctx, &raw, "eth_getBlockByHash", h, false); err != nil {
		return Block{}, redactErr(fmt.Errorf("ethrpc %s: eth_getBlockByHash: %w", c.name, err))
	}
	if raw == nil {
		return Block{}, fmt.Errorf("ethrpc %s: block %s not found", c.name, h)
	}
	return Block{Number: uint64(raw.Number), Hash: raw.Hash, StateRoot: raw.StateRoot, Timestamp: uint64(raw.Timestamp)}, nil
}

// ChainID reads eth_chainId.
func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	return c.eth.ChainID(ctx)
}

// ErrRevert is an eth_call revert, with the decoded reason when the data is a
// standard Error(string), else the raw data.
type ErrRevert struct {
	Method string
	Reason string
	Data   []byte
}

func (e *ErrRevert) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s reverted: %s", e.Method, e.Reason)
	}
	return fmt.Sprintf("%s reverted: 0x%x", e.Method, e.Data)
}

// Call runs an eth_call of abi.method(args...) on to at block and unpacks the
// outputs.
func (c *Client) Call(ctx context.Context, to common.Address, a *gethabi.ABI, method string, block uint64, args ...any) ([]any, error) {
	m, ok := a.Methods[method]
	if !ok {
		return nil, fmt.Errorf("ethrpc: no method %s", method)
	}
	input, err := a.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("ethrpc %s: pack: %w", method, err)
	}
	var out hexutil.Bytes
	call := map[string]any{"to": to, "data": hexutil.Bytes(input)}
	if err := c.rpc.CallContext(ctx, &out, "eth_call", call, hexutil.EncodeUint64(block)); err != nil {
		var de rpc.DataError
		if errors.As(err, &de) {
			if data, ok := de.ErrorData().(string); ok {
				b, _ := hexutil.Decode(data)
				reason, _ := gethabi.UnpackRevert(b)
				return nil, &ErrRevert{Method: method, Reason: reason, Data: b}
			}
		}
		return nil, redactErr(fmt.Errorf("ethrpc %s: %s: %w", c.name, method, err))
	}
	if len(out) == 0 && len(m.Outputs) > 0 {
		return nil, fmt.Errorf("ethrpc %s: %s at %s returned no data (no code at the address?)", c.name, method, to)
	}
	vals, err := m.Outputs.Unpack(out)
	if err != nil {
		return nil, fmt.Errorf("ethrpc %s: unpack: %w", method, err)
	}
	return vals, nil
}

// Code reads the code at an address, pinned.
func (c *Client) Code(ctx context.Context, addr common.Address, block uint64) ([]byte, error) {
	return c.eth.CodeAt(ctx, addr, new(big.Int).SetUint64(block))
}

// CodeHash is keccak256 of the code at an address (what EXTCODEHASH gives for
// an account with code; zero bytes hash for none).
func (c *Client) CodeHash(ctx context.Context, addr common.Address, block uint64) (common.Hash, int, error) {
	code, err := c.Code(ctx, addr, block)
	if err != nil {
		return common.Hash{}, 0, err
	}
	return crypto.Keccak256Hash(code), len(code), nil
}

// Logs reads logs of addr matching topics over [from, to], in chunks.
func (c *Client) Logs(ctx context.Context, addr common.Address, topics [][]common.Hash, from, to uint64) ([]types.Log, error) {
	var out []types.Log
	chunk := c.LogChunk
	if chunk == 0 {
		chunk = 2000
	}
	for start := from; start <= to; start += chunk {
		end := start + chunk - 1
		if end > to {
			end = to
		}
		logs, err := c.eth.FilterLogs(ctx, filterQuery(addr, topics, start, end))
		if err != nil {
			return nil, redactErr(fmt.Errorf("ethrpc %s: eth_getLogs %d-%d: %w", c.name, start, end, err))
		}
		out = append(out, logs...)
	}
	return out, nil
}

// redactErr strips secrets (URLs with tokens) from an RPC error before it
// leaves this package (docs/design.md §10.2).
func redactErr(err error) error { return redact.Error(err) }

// Now is the wall clock used for provenance; replaceable in tests.
var Now = time.Now
