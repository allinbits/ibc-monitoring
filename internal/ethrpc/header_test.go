package ethrpc

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc/ethrpctest"
)

// The pinned block's hash is recomputed from the header's fields, so a node
// cannot pair a genuine hash with an invented state root (review finding 1).
func TestHeaderHashIsBoundToFields(t *testing.T) {
	honest := ethrpctest.New(31337, ethrpctest.Block{Number: 10, StateRoot: common.HexToHash("0x22"), Timestamp: 1000})
	t.Cleanup(honest.Close)
	c, err := Dial(context.Background(), honest.URL, "honest", Finality{Mode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Pinned(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Hash != honest.Head().HashOf() || b.StateRoot != common.HexToHash("0x22") || b.Number != 10 {
		t.Fatalf("pinned %+v", b)
	}
	// The same fields hash the same; by hash the answer must match the request.
	if _, err := c.BlockByHash(context.Background(), b.Hash); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BlockByHash(context.Background(), common.HexToHash("0x5555")); err == nil {
		t.Fatal("unknown hash must fail")
	}

	// A lying node: the reported hash is not the hash of the fields it gives.
	liar := ethrpctest.New(31337, ethrpctest.Block{Number: 10, StateRoot: common.HexToHash("0x22"), Timestamp: 1000, Lie: true})
	t.Cleanup(liar.Close)
	lc, err := Dial(context.Background(), liar.URL, "liar", Finality{Mode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lc.Pinned(context.Background()); !errors.Is(err, ErrHeaderHash) {
		t.Fatalf("lie accepted: %v", err)
	}
}

func TestFinalityModes(t *testing.T) {
	for _, s := range []string{"", "tag", "finalized"} {
		if f, err := ParseFinality(s); err != nil || f.Mode != "tag" {
			t.Fatalf("%q: %+v %v", s, f, err)
		}
	}
	if f, err := ParseFinality("depth:3"); err != nil || f.Mode != "depth" || f.Depth != 3 {
		t.Fatalf("depth: %+v %v", f, err)
	}
	// Beacon verification is phase 2: refused, never mapped to the tag.
	if _, err := ParseFinality("beacon"); err == nil {
		t.Fatal("beacon must be refused until implemented")
	}
	if _, err := ParseFinality("nope"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
