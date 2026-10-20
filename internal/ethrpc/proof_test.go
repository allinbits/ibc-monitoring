package ethrpc

import (
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
	values, err := p.Verify(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if values[slot].Cmp(value) != 0 {
		t.Fatalf("value %s", values[slot])
	}

	// A lying node: the reported value differs from the proven one.
	lie := p
	lie.Storage = []StorageProof{{Key: slot, Value: big.NewInt(1), Proof: p.Storage[0].Proof}}
	if _, err := lie.Verify(stateRoot); !errors.Is(err, ErrProof) {
		t.Fatalf("lie accepted: %v", err)
	}
	// A wrong state root (another header).
	if _, err := p.Verify(common.HexToHash("0x01")); !errors.Is(err, ErrProof) {
		t.Fatalf("wrong root accepted: %v", err)
	}
	// A storage root that does not match the proven account.
	bad := p
	bad.StorageHash = common.HexToHash("0x02")
	if _, err := bad.Verify(stateRoot); !errors.Is(err, ErrProof) {
		t.Fatalf("wrong storage root accepted: %v", err)
	}
	// An absent slot proves as zero.
	absent := common.HexToHash("0x0b")
	q := p
	q.Storage = []StorageProof{{Key: absent, Value: big.NewInt(0), Proof: proofOf(t, storage, crypto.Keccak256(absent.Bytes()))}}
	values, err = q.Verify(stateRoot)
	if err != nil || values[absent].Sign() != 0 {
		t.Fatalf("absent slot: %v %v", values, err)
	}
}
