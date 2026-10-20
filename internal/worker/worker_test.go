package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"

	"github.com/allinbits/ibc-monitoring/abi"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/ethrpc/ethrpctest"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/redact"
)

// accountTrie builds a state trie holding one account with the given code
// hash and returns the root and the eth_getProof answer a node would give.
func accountTrie(t *testing.T, addr common.Address, codeHash common.Hash) (common.Hash, map[string]any) {
	t.Helper()
	tdb := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	acc := types.StateAccount{Nonce: 1, Balance: uint256.NewInt(0), Root: types.EmptyRootHash, CodeHash: codeHash.Bytes()}
	accRLP, _ := rlp.EncodeToBytes(&acc)
	state := trie.NewEmpty(tdb)
	state.MustUpdate(crypto.Keccak256(addr.Bytes()), accRLP)
	root := state.Hash()
	db := memorydb.New()
	if err := state.Prove(crypto.Keccak256(addr.Bytes()), db); err != nil {
		t.Fatal(err)
	}
	it := db.NewIterator(nil, nil)
	defer it.Release()
	var nodes []string
	for it.Next() {
		nodes = append(nodes, hexutil.Encode(it.Value()))
	}
	return root, map[string]any{
		"address": addr.Hex(), "accountProof": nodes, "balance": "0x0", "codeHash": codeHash.Hex(),
		"nonce": "0x1", "storageHash": types.EmptyRootHash.Hex(), "storageProof": []any{},
	}
}

func TestCodeHashAndRedaction(t *testing.T) {
	redact.Reset()
	t.Cleanup(redact.Reset)
	redact.Register("https://rpc.example/v3/SECRETTOKEN")
	addr := common.HexToAddress("0xcafe")
	code := []byte{0x60, 0x80, 0x60, 0x40}
	root, proof := accountTrie(t, addr, crypto.Keccak256Hash(code))
	srv := ethrpctest.New(31337, ethrpctest.Block{Number: 5, Timestamp: 1, StateRoot: root})
	t.Cleanup(srv.Close)
	srv.Contract(addr, abi.MustLoad(abi.Voucher), code)
	srv.ProofFor(addr, proof)
	c, err := ethrpc.Dial(context.Background(), srv.URL, "fixture", ethrpc.Finality{Mode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	block, err := c.Pinned(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	agreed := model.Provenance{Verification: model.VerificationCrossChecked}
	w := &Worker{Eth: c}
	j := w.CodeHash(context.Background(), CodeHashTarget{Subject: "decision 1/7 kind 7 via https://rpc.example/v3/SECRETTOKEN", Address: addr, Expected: crypto.Keccak256Hash(code)}, block, agreed)
	if j.Status != "passed" || len(j.Checks) != 4 || j.Provenance.StateVerification != model.StateProof {
		t.Fatalf("job %+v", j)
	}
	if strings.Contains(j.Subject, "SECRETTOKEN") {
		t.Fatalf("subject leaks: %s", j.Subject)
	}
	// A mismatching pin fails (the code itself is authenticated).
	j = w.CodeHash(context.Background(), CodeHashTarget{Subject: "x", Address: addr, Expected: common.HexToHash("0x01")}, block, agreed)
	if j.Status != "failed" {
		t.Fatalf("job %+v", j)
	}
	// A block that is not header-agreed gives no verdict at all.
	j = w.CodeHash(context.Background(), CodeHashTarget{Subject: "x", Address: addr, Expected: crypto.Keccak256Hash(code)}, block, model.Provenance{Verification: model.VerificationUnverified})
	if j.Status != "unverified" {
		t.Fatalf("unverified block gave %s", j.Status)
	}
	// A node that serves other bytecode than the account's proven code hash
	// is caught: the verdict is unverified, not a pass or a fail.
	srv.Contract(addr, abi.MustLoad(abi.Voucher), []byte{0x60, 0x80, 0x60, 0x41})
	j = w.CodeHash(context.Background(), CodeHashTarget{Subject: "x", Address: addr, Expected: crypto.Keccak256Hash(code)}, block, agreed)
	if j.Status != "unverified" || !strings.Contains(j.Verdict, "proven code hash") {
		t.Fatalf("lying node: %+v", j)
	}
	// A proof that does not verify against the agreed root is refused too.
	srv.Contract(addr, abi.MustLoad(abi.Voucher), code)
	wrong := block
	wrong.StateRoot = common.HexToHash("0x9")
	j = w.CodeHash(context.Background(), CodeHashTarget{Subject: "x", Address: addr, Expected: crypto.Keccak256Hash(code)}, wrong, agreed)
	if j.Status != "unverified" {
		t.Fatalf("wrong root: %+v", j)
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
