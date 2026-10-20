package ethrpc

import (
	"context"
	"errors"
	"math/big"
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
)

// proofOf builds a proof of key in t as eth_getProof would return it.
func proofOf(t *testing.T, tr *trie.Trie, key []byte) []string {
	t.Helper()
	db := memorydb.New()
	if err := tr.Prove(key, db); err != nil {
		t.Fatal(err)
	}
	it := db.NewIterator(nil, nil)
	defer it.Release()
	var out []string
	for it.Next() {
		out = append(out, hexutil.Encode(it.Value()))
	}
	return out
}

func TestAccountProofVerify(t *testing.T) {
	tdb := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	addr := common.HexToAddress("0x000000000000000000000000000000000000bbbb")
	slot := common.HexToHash("0x0a")
	value := big.NewInt(0x1234)

	// Storage trie with one slot.
	storage := trie.NewEmpty(tdb)
	enc, _ := rlp.EncodeToBytes(value.Bytes())
	storage.MustUpdate(crypto.Keccak256(slot.Bytes()), enc)
	storageRoot := storage.Hash()

	// Account trie with that account.
	acc := types.StateAccount{Nonce: 7, Balance: uint256.NewInt(99), Root: storageRoot, CodeHash: crypto.Keccak256([]byte{0x60, 0x80})}
	accRLP, _ := rlp.EncodeToBytes(&acc)
	state := trie.NewEmpty(tdb)
	state.MustUpdate(crypto.Keccak256(addr.Bytes()), accRLP)
	stateRoot := state.Hash()

	p := AccountProof{
		Address:      addr,
		AccountProof: proofOf(t, state, crypto.Keccak256(addr.Bytes())),
		Balance:      big.NewInt(99),
		CodeHash:     common.BytesToHash(acc.CodeHash),
		Nonce:        7,
		StorageHash:  storageRoot,
		Storage:      []StorageProof{{Key: slot, Value: value, Proof: proofOf(t, storage, crypto.Keccak256(slot.Bytes()))}},
	}
	account, values, err := p.Verify(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if values[slot].Cmp(value) != 0 {
		t.Fatalf("value %s", values[slot])
	}
	if !account.Exists || account.Nonce != 7 || account.Balance.Int64() != 99 || account.CodeHash != common.BytesToHash(acc.CodeHash) || account.StorageRoot != storageRoot {
		t.Fatalf("account %+v", account)
	}

	// A lying node: the reported value differs from the proven one.
	lie := p
	lie.Storage = []StorageProof{{Key: slot, Value: big.NewInt(1), Proof: p.Storage[0].Proof}}
	if _, _, err := lie.Verify(stateRoot); !errors.Is(err, ErrProof) {
		t.Fatalf("lie accepted: %v", err)
	}
	// A wrong state root (another header).
	if _, _, err := p.Verify(common.HexToHash("0x01")); !errors.Is(err, ErrProof) {
		t.Fatalf("wrong root accepted: %v", err)
	}
	// A storage root that does not match the proven account.
	bad := p
	bad.StorageHash = common.HexToHash("0x02")
	if _, _, err := bad.Verify(stateRoot); !errors.Is(err, ErrProof) {
		t.Fatalf("wrong storage root accepted: %v", err)
	}
	// An absent slot proves as zero.
	absent := common.HexToHash("0x0b")
	q := p
	q.Storage = []StorageProof{{Key: absent, Value: big.NewInt(0), Proof: proofOf(t, storage, crypto.Keccak256(absent.Bytes()))}}
	_, values, err = q.Verify(stateRoot)
	if err != nil || values[absent].Sign() != 0 {
		t.Fatalf("absent slot: %v %v", values, err)
	}
}

// An absence proof is accepted only with the canonical empty report, and the
// account it yields carries the empty code hash derived here, so a provider
// cannot pair a genuine absence with an invented code hash (review finding 1).
func TestAbsentAccount(t *testing.T) {
	tdb := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	other := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	absent := common.HexToAddress("0x000000000000000000000000000000000000bbbb")
	state := trie.NewEmpty(tdb)
	acc := types.StateAccount{Nonce: 1, Balance: uint256.NewInt(1), Root: types.EmptyRootHash, CodeHash: crypto.Keccak256([]byte{1})}
	accRLP, _ := rlp.EncodeToBytes(&acc)
	state.MustUpdate(crypto.Keccak256(other.Bytes()), accRLP)
	root := state.Hash()
	honest := AccountProof{Address: absent, AccountProof: proofOf(t, state, crypto.Keccak256(absent.Bytes())), Balance: big.NewInt(0), CodeHash: common.Hash{}, StorageHash: common.Hash{}}
	account, _, err := honest.Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	if account.Exists || account.CodeHash != EmptyCodeHash || account.StorageRoot != types.EmptyRootHash || account.Nonce != 0 || account.Balance.Sign() != 0 {
		t.Fatalf("absent account %+v", account)
	}
	// A lying provider: the same genuine absence proof with an invented code
	// hash (or nonce, balance, storage root) is refused.
	for _, lie := range []AccountProof{
		{Address: absent, AccountProof: honest.AccountProof, CodeHash: crypto.Keccak256Hash([]byte{0x60, 0x80}), Balance: big.NewInt(0)},
		{Address: absent, AccountProof: honest.AccountProof, Nonce: 3, Balance: big.NewInt(0)},
		{Address: absent, AccountProof: honest.AccountProof, Balance: big.NewInt(5)},
		{Address: absent, AccountProof: honest.AccountProof, StorageHash: common.HexToHash("0x33"), Balance: big.NewInt(0)},
	} {
		if _, _, err := lie.Verify(root); !errors.Is(err, ErrProof) {
			t.Fatalf("lie accepted: %+v: %v", lie, err)
		}
	}
}

// A proof must answer exactly the requested account and slot set (review
// finding 2): the fake node substitutes answers and each is refused.
func TestProofCorrespondence(t *testing.T) {
	asked := common.HexToAddress("0x000000000000000000000000000000000000bbbb")
	slot := common.HexToHash("0x0a")
	other := common.HexToHash("0x0b")
	cases := []struct {
		name   string
		answer gethProofAnswer
	}{
		{"other account", gethProofAnswer{Address: common.HexToAddress("0xcccc"), Slots: []common.Hash{slot}}},
		{"missing slot", gethProofAnswer{Address: asked, Slots: nil}},
		{"extra slot", gethProofAnswer{Address: asked, Slots: []common.Hash{slot, other}}},
		{"duplicate slot", gethProofAnswer{Address: asked, Slots: []common.Hash{slot, slot}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newProofServer(t, tc.answer)
			c, err := Dial(context.Background(), srv.URL, "proof", Finality{Mode: "latest"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Proof(context.Background(), asked, []common.Hash{slot}, 1)
			if !errors.Is(err, ErrProofMismatch) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
	srv := newProofServer(t, gethProofAnswer{Address: asked, Slots: []common.Hash{slot}})
	c, err := Dial(context.Background(), srv.URL, "proof", Finality{Mode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Proof(context.Background(), asked, []common.Hash{slot}, 1)
	if err != nil || p.Address != asked || len(p.Storage) != 1 || p.Storage[0].Key != slot {
		t.Fatalf("exact answer refused: %+v %v", p, err)
	}
}
