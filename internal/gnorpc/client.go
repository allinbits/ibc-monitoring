// Package gnorpc is a minimal tm2 JSON-RPC client for the gno.land queries the
// monitor needs: status, block, block results and abci_query (vm/qeval,
// vm/qrender) pinned to a height. It has no dependency on the gno module.
package gnorpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/redact"
)

// Client talks to one tm2 RPC endpoint over HTTP.
type Client struct {
	url  string
	name string
	http *http.Client
	id   atomic.Int64
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client (timeouts, transport).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithName sets the label used in provenance (never the URL).
func WithName(name string) Option { return func(c *Client) { c.name = name } }

// New returns a client for url.
func New(url string, opts ...Option) *Client {
	c := &Client{url: url, name: "gno", http: &http.Client{Timeout: 30 * time.Second}}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name returns the provenance label.
func (c *Client) Name() string { return c.name }

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: strconv.FormatInt(c.id.Add(1), 10), Method: method, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return redact.Errorf("gnorpc %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return redact.Errorf("gnorpc %s: http %d: %s", method, resp.StatusCode, truncate(raw))
	}
	var r rpcResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("gnorpc %s: decode: %w", method, err)
	}
	if r.Error != nil {
		return redact.Errorf("gnorpc %s: %s (%s)", method, r.Error.Message, r.Error.Data)
	}
	return json.Unmarshal(r.Result, out)
}

func truncate(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}

// Status is the subset of the node's status the monitor uses.
type Status struct {
	ChainID           string
	LatestBlockHeight int64
	LatestBlockHash   string
	LatestBlockTime   time.Time
	CatchingUp        bool
}

// Status queries the node's status.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var raw struct {
		NodeInfo struct {
			Network string `json:"network"`
		} `json:"node_info"`
		SyncInfo struct {
			LatestBlockHash   string    `json:"latest_block_hash"`
			LatestBlockHeight string    `json:"latest_block_height"`
			LatestBlockTime   time.Time `json:"latest_block_time"`
			CatchingUp        bool      `json:"catching_up"`
		} `json:"sync_info"`
	}
	if err := c.call(ctx, "status", map[string]any{}, &raw); err != nil {
		return Status{}, err
	}
	h, err := strconv.ParseInt(raw.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil {
		return Status{}, fmt.Errorf("status: height %q: %w", raw.SyncInfo.LatestBlockHeight, err)
	}
	return Status{
		ChainID:           raw.NodeInfo.Network,
		LatestBlockHeight: h,
		LatestBlockHash:   decodeHash(raw.SyncInfo.LatestBlockHash),
		LatestBlockTime:   raw.SyncInfo.LatestBlockTime,
		CatchingUp:        raw.SyncInfo.CatchingUp,
	}, nil
}

// Header is the subset of a block header the monitor uses.
type Header struct {
	ChainID            string
	Height             int64
	Time               time.Time
	Hash               string
	AppHash            string
	ValidatorsHash     string
	NextValidatorsHash string
}

// Block fetches the header of a block (height 0 means latest).
func (c *Client) Block(ctx context.Context, height int64) (Header, error) {
	params := map[string]any{}
	if height > 0 {
		params["height"] = strconv.FormatInt(height, 10)
	}
	var raw struct {
		BlockMeta struct {
			BlockID struct {
				Hash string `json:"hash"`
			} `json:"block_id"`
			Header struct {
				ChainID            string    `json:"chain_id"`
				Height             string    `json:"height"`
				Time               time.Time `json:"time"`
				AppHash            string    `json:"app_hash"`
				ValidatorsHash     string    `json:"validators_hash"`
				NextValidatorsHash string    `json:"next_validators_hash"`
			} `json:"header"`
		} `json:"block_meta"`
	}
	if err := c.call(ctx, "block", params, &raw); err != nil {
		return Header{}, err
	}
	h, err := strconv.ParseInt(raw.BlockMeta.Header.Height, 10, 64)
	if err != nil {
		return Header{}, fmt.Errorf("block: height %q: %w", raw.BlockMeta.Header.Height, err)
	}
	return Header{
		ChainID:            raw.BlockMeta.Header.ChainID,
		Height:             h,
		Time:               raw.BlockMeta.Header.Time,
		Hash:               decodeHash(raw.BlockMeta.BlockID.Hash),
		AppHash:            decodeHash(raw.BlockMeta.Header.AppHash),
		ValidatorsHash:     decodeHash(raw.BlockMeta.Header.ValidatorsHash),
		NextValidatorsHash: decodeHash(raw.BlockMeta.Header.NextValidatorsHash),
	}, nil
}

// decodeHash turns tm2's amino JSON bytes (base64) into upper-case hex; a
// value that is already hex is returned unchanged.
func decodeHash(s string) string {
	if s == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return s
	}
	const hexdigits = "0123456789ABCDEF"
	out := make([]byte, 0, 2*len(b))
	for _, x := range b {
		out = append(out, hexdigits[x>>4], hexdigits[x&0xf])
	}
	return string(out)
}

// ABCIResult is an abci_query answer: the data and the height it was
// evaluated at.
type ABCIResult struct {
	Data   []byte
	Height int64
	Log    string
}

// ErrABCI is returned when the application refused a query.
type ErrABCI struct {
	Path    string
	Message string
	Log     string
}

func (e *ErrABCI) Error() string { return fmt.Sprintf("abci_query %s: %s", e.Path, e.Message) }

// ABCIQuery runs an abci_query at a height (0 = latest). The returned height
// is the one the node evaluated the query at, which the caller records as
// provenance.
func (c *Client) ABCIQuery(ctx context.Context, path string, data []byte, height int64) (ABCIResult, error) {
	params := map[string]any{
		"path":   path,
		"data":   base64.StdEncoding.EncodeToString(data),
		"height": strconv.FormatInt(height, 10),
		"prove":  false,
	}
	var raw struct {
		Response struct {
			ResponseBase struct {
				Error json.RawMessage `json:"Error"`
				Data  string          `json:"Data"`
				Log   string          `json:"Log"`
				Info  string          `json:"Info"`
			} `json:"ResponseBase"`
			Height string `json:"Height"`
		} `json:"response"`
	}
	if err := c.call(ctx, "abci_query", params, &raw); err != nil {
		return ABCIResult{}, err
	}
	rb := raw.Response.ResponseBase
	if len(rb.Error) > 0 && string(rb.Error) != "null" {
		return ABCIResult{}, &ErrABCI{Path: path, Message: string(rb.Error), Log: rb.Log}
	}
	var out ABCIResult
	if rb.Data != "" {
		b, err := base64.StdEncoding.DecodeString(rb.Data)
		if err != nil {
			return ABCIResult{}, fmt.Errorf("abci_query %s: data: %w", path, err)
		}
		out.Data = b
	}
	out.Log = rb.Log
	if raw.Response.Height != "" {
		h, err := strconv.ParseInt(raw.Response.Height, 10, 64)
		if err != nil {
			return ABCIResult{}, fmt.Errorf("abci_query %s: height %q: %w", path, raw.Response.Height, err)
		}
		out.Height = h
	}
	return out, nil
}

// QEval evaluates expr in pkgPath (vm/qeval) and returns the raw typed-value
// text (see Values).
func (c *Client) QEval(ctx context.Context, pkgPath, expr string, height int64) (string, int64, error) {
	if pkgPath == "" || expr == "" {
		return "", 0, errors.New("qeval: empty pkgpath or expression")
	}
	r, err := c.ABCIQuery(ctx, "vm/qeval", []byte(pkgPath+"."+expr), height)
	if err != nil {
		return "", 0, err
	}
	return string(r.Data), r.Height, nil
}

// QRender calls Render(path) of pkgPath (vm/qrender). The output is Markdown
// or JSON depending on the path; the caller knows which. It is untrusted text.
func (c *Client) QRender(ctx context.Context, pkgPath, path string, height int64) (string, int64, error) {
	if pkgPath == "" {
		return "", 0, errors.New("qrender: empty pkgpath")
	}
	r, err := c.ABCIQuery(ctx, "vm/qrender", []byte(pkgPath+":"+path), height)
	if err != nil {
		return "", 0, err
	}
	return string(r.Data), r.Height, nil
}

// Event is one event of a transaction's results.
type Event struct {
	Type    string
	PkgPath string
	Attrs   map[string]string
}

// BlockResults returns the events of every successful transaction of a block.
// Failed transactions are skipped.
func (c *Client) BlockResults(ctx context.Context, height int64) ([]Event, error) {
	var raw struct {
		Results struct {
			DeliverTxs []struct {
				ResponseBase struct {
					Error  json.RawMessage `json:"Error"`
					Events []struct {
						Type    string `json:"type"`
						PkgPath string `json:"pkg_path"`
						Attrs   []struct {
							Key   string `json:"key"`
							Value string `json:"value"`
						} `json:"attrs"`
					} `json:"Events"`
				} `json:"ResponseBase"`
			} `json:"deliver_tx"`
		} `json:"results"`
	}
	if err := c.call(ctx, "block_results", map[string]any{"height": strconv.FormatInt(height, 10)}, &raw); err != nil {
		return nil, err
	}
	var out []Event
	for _, tx := range raw.Results.DeliverTxs {
		if len(tx.ResponseBase.Error) > 0 && string(tx.ResponseBase.Error) != "null" {
			continue
		}
		for _, ev := range tx.ResponseBase.Events {
			attrs := make(map[string]string, len(ev.Attrs))
			for _, a := range ev.Attrs {
				attrs[a.Key] = a.Value
			}
			out = append(out, Event{Type: ev.Type, PkgPath: ev.PkgPath, Attrs: attrs})
		}
	}
	return out, nil
}
