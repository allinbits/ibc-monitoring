// Package gnoviews reads the gno.land realms' views into typed models
// (docs/design.md §4.1). Every read is pinned to one height so a snapshot is
// consistent, and every result carries that height as provenance.
//
// Render output is untrusted text: JSON paths are parsed with json.Number so
// no number passes through a float, and Markdown paths are never parsed.
package gnoviews

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/gnorpc"
	"github.com/allinbits/ibc-monitoring/internal/model"
)

// Paths are the realms' pkgpaths. Always the proxies, never impl/vN.
type Paths struct {
	Core     string
	Transfer string
	IBCGov   string
}

// DefaultPaths are the gno-realms deployment paths.
var DefaultPaths = Paths{
	Core:     "gno.land/r/aib/ibc/core",
	Transfer: "gno.land/r/aib/ibc/apps/transfer",
	IBCGov:   "gno.land/r/aib/ibc/ibcgov",
}

// Views reads from one node.
type Views struct {
	rpc   *gnorpc.Client
	paths Paths
	now   func() time.Time
}

// New returns views over rpc.
func New(rpc *gnorpc.Client, paths Paths) *Views {
	if paths.Core == "" {
		paths = DefaultPaths
	}
	return &Views{rpc: rpc, paths: paths, now: time.Now}
}

// Pin is a height every read of one snapshot is pinned to.
type Pin struct {
	Height    int64
	BlockHash string
	Time      time.Time
}

// LatestPin reads the node's latest committed height and its hash. A node
// that is catching up is refused as a primary.
func (v *Views) LatestPin(ctx context.Context) (Pin, error) {
	st, err := v.rpc.Status(ctx)
	if err != nil {
		return Pin{}, err
	}
	if st.CatchingUp {
		return Pin{}, fmt.Errorf("node %s is catching up", v.rpc.Name())
	}
	return Pin{Height: st.LatestBlockHeight, BlockHash: st.LatestBlockHash, Time: st.LatestBlockTime}, nil
}

// PinAt reads the hash of a given height (for cross-checks).
func (v *Views) PinAt(ctx context.Context, height int64) (Pin, error) {
	h, err := v.rpc.Block(ctx, height)
	if err != nil {
		return Pin{}, err
	}
	return Pin{Height: h.Height, BlockHash: h.Hash, Time: h.Time}, nil
}

func (v *Views) provenance(p Pin) model.Provenance {
	return model.Provenance{
		Chain:        model.ChainGno,
		Height:       uint64(p.Height),
		BlockHash:    p.BlockHash,
		Node:         v.rpc.Name(),
		ReadAt:       v.now(),
		Verification: model.VerificationProvider,
		Finality:     "committed",
	}
}

// eval runs one qeval at the pin and returns its single result.
func (v *Views) eval(ctx context.Context, pkg, expr string, p Pin) (gnorpc.Value, error) {
	raw, _, err := v.rpc.QEval(ctx, pkg, expr, p.Height)
	if err != nil {
		return gnorpc.Value{}, fmt.Errorf("%s.%s: %w", shortPkg(pkg), expr, err)
	}
	val, err := gnorpc.One(raw)
	if err != nil {
		return gnorpc.Value{}, fmt.Errorf("%s.%s: %w", shortPkg(pkg), expr, err)
	}
	return val, nil
}

// evalN runs one qeval returning several results.
func (v *Views) evalN(ctx context.Context, pkg, expr string, p Pin) ([]gnorpc.Value, error) {
	raw, _, err := v.rpc.QEval(ctx, pkg, expr, p.Height)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", shortPkg(pkg), expr, err)
	}
	vals, err := gnorpc.Values(raw)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", shortPkg(pkg), expr, err)
	}
	return vals, nil
}

// renderJSON calls Render(path) and decodes its JSON answer into out, with
// numbers kept as tokens.
func (v *Views) renderJSON(ctx context.Context, pkg, path string, p Pin, out any) error {
	raw, _, err := v.rpc.QRender(ctx, pkg, path, p.Height)
	if err != nil {
		return fmt.Errorf("%s:%s: %w", shortPkg(pkg), path, err)
	}
	if strings.HasPrefix(strings.TrimSpace(raw), "{\"error\"") {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(raw), &e) == nil && e.Error != "" {
			return fmt.Errorf("%s:%s: %s", shortPkg(pkg), path, e.Error)
		}
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%s:%s: decode: %w", shortPkg(pkg), path, err)
	}
	return nil
}

func shortPkg(pkg string) string {
	if i := strings.LastIndexByte(pkg, '/'); i >= 0 {
		return pkg[i+1:]
	}
	return pkg
}

func q(s string) string { return strconv.Quote(s) }

// num reads a json.Number token as int64; "" is 0.
func num(n json.Number) (int64, error) {
	if n == "" {
		return 0, nil
	}
	return strconv.ParseInt(n.String(), 10, 64)
}

// unum reads a json.Number token as uint64.
func unum(n json.Number) (uint64, error) {
	if n == "" {
		return 0, nil
	}
	return strconv.ParseUint(n.String(), 10, 64)
}

// amount reads a json.Number token as an Amount (exact).
func amount(n json.Number) (model.Amount, error) {
	if n == "" {
		return model.Amount{}, nil
	}
	return model.ParseAmount(n.String())
}

// ustr reads a uint64 the realm emitted as a decimal string.
func ustr(s string) (uint64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseUint(s, 10, 64)
}
