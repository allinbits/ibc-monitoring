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
	// A delegation designator is detected on the authenticated buffer itself.
	desig := append([]byte{0xef, 0x01, 0x00}, common.HexToAddress("0x1234").Bytes()...)
	droot, dproof := accountTrie(t, addr, crypto.Keccak256Hash(desig))
	dsrv := ethrpctest.New(31337, ethrpctest.Block{Number: 5, Timestamp: 1, StateRoot: droot})
	t.Cleanup(dsrv.Close)
	dsrv.Contract(addr, abi.MustLoad(abi.Voucher), desig)
	dsrv.ProofFor(addr, dproof)
	dc, err := ethrpc.Dial(context.Background(), dsrv.URL, "fixture", ethrpc.Finality{Mode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	dblock, _ := dc.Pinned(context.Background())
	j = (&Worker{Eth: dc}).CodeHash(context.Background(), CodeHashTarget{Subject: "x", Address: addr, Expected: crypto.Keccak256Hash(desig)}, dblock, agreed)
	if j.Status != "failed" || !strings.Contains(j.Verdict, "does not match") {
		t.Fatalf("designator: %+v", j)
	}
	var found bool
	for _, c := range j.Checks {
		if c.Name == "not an EIP-7702 delegation designator" && !c.Passed {
			found = true
		}
	}
	if !found {
		t.Fatalf("designator check missing: %+v", j.Checks)
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

// A genuine absence proof with an invented code hash and matching bytecode
// never passes: the empty code hash is derived locally (review finding 1).
func TestCodeHashAbsentAccountLie(t *testing.T) {
	addr := common.HexToAddress("0xcafe")
	other := common.HexToAddress("0xd00d")
	code := []byte{0x60, 0x80}
	// The trie holds only another account; the candidate is absent.
	root, _ := accountTrie(t, other, crypto.Keccak256Hash([]byte{1}))
	tdb := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	state := trie.NewEmpty(tdb)
	acc := types.StateAccount{Nonce: 1, Balance: uint256.NewInt(0), Root: types.EmptyRootHash, CodeHash: crypto.Keccak256([]byte{1})}
	accRLP, _ := rlp.EncodeToBytes(&acc)
	state.MustUpdate(crypto.Keccak256(other.Bytes()), accRLP)
	if state.Hash() != root {
		t.Fatal("trie mismatch")
	}
	db := memorydb.New()
	if err := state.Prove(crypto.Keccak256(addr.Bytes()), db); err != nil {
		t.Fatal(err)
	}
	it := db.NewIterator(nil, nil)
	var nodes []string
	for it.Next() {
		nodes = append(nodes, hexutil.Encode(it.Value()))
	}
	it.Release()
	lie := map[string]any{
		"address": addr.Hex(), "accountProof": nodes, "balance": "0x0", "codeHash": crypto.Keccak256Hash(code).Hex(),
		"nonce": "0x0", "storageHash": types.EmptyRootHash.Hex(), "storageProof": []any{},
	}
	srv := ethrpctest.New(31337, ethrpctest.Block{Number: 5, Timestamp: 1, StateRoot: root})
	t.Cleanup(srv.Close)
	srv.Contract(addr, abi.MustLoad(abi.Voucher), code)
	srv.ProofFor(addr, lie)
	c, err := ethrpc.Dial(context.Background(), srv.URL, "fixture", ethrpc.Finality{Mode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	block, _ := c.Pinned(context.Background())
	j := (&Worker{Eth: c}).CodeHash(context.Background(), CodeHashTarget{Subject: "x", Address: addr, Expected: crypto.Keccak256Hash(code)}, block, model.Provenance{Verification: model.VerificationCrossChecked})
	if j.Status != "unverified" {
		t.Fatalf("absence lie gave %s: %+v", j.Status, j)
	}
	// An honest absence report yields "failed" (no code), never "passed".
	honest := map[string]any{"address": addr.Hex(), "accountProof": nodes, "balance": "0x0", "codeHash": common.Hash{}.Hex(), "nonce": "0x0", "storageHash": common.Hash{}.Hex(), "storageProof": []any{}}
	srv.ProofFor(addr, honest)
	srv.Contract(addr, abi.MustLoad(abi.Voucher), nil)
	j = (&Worker{Eth: c}).CodeHash(context.Background(), CodeHashTarget{Subject: "x", Address: addr, Expected: crypto.Keccak256Hash(nil)}, block, model.Provenance{Verification: model.VerificationCrossChecked})
	if j.Status != "failed" {
		t.Fatalf("honest absence gave %s: %+v", j.Status, j)
	}
}
