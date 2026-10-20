package ethrpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// StorageProof is one slot's proven value.
type StorageProof struct {
	Key   common.Hash
	Value *big.Int
	Proof []string
}

// AccountProof is an eth_getProof answer kept whole: the account proof is
// what binds the storage root to the header's state root, and the storage
// proofs bind each value to that storage root (docs/design.md §3.3).
type AccountProof struct {
	Address      common.Address
	AccountProof []string
	Balance      *big.Int
	CodeHash     common.Hash
	Nonce        uint64
	StorageHash  common.Hash
	Storage      []StorageProof
}

// Proof reads eth_getProof of slots at block. The result is unverified until
// Verify is called against an authenticated state root.
func (c *Client) Proof(ctx context.Context, addr common.Address, slots []common.Hash, block uint64) (AccountProof, error) {
	keys := make([]string, len(slots))
	for i, s := range slots {
		keys[i] = s.Hex()
	}
	res, err := c.geth.GetProof(ctx, addr, keys, new(big.Int).SetUint64(block))
	if err != nil {
		return AccountProof{}, redactErr(fmt.Errorf("ethrpc %s: eth_getProof: %w", c.name, err))
	}
	out := AccountProof{Address: res.Address, AccountProof: res.AccountProof, Balance: res.Balance, CodeHash: res.CodeHash, Nonce: res.Nonce, StorageHash: res.StorageHash}
	for _, sp := range res.StorageProof {
		out.Storage = append(out.Storage, StorageProof{Key: common.HexToHash(sp.Key), Value: sp.Value, Proof: sp.Proof})
	}
	return out, nil
}

// ErrProof is a proof that does not verify.
var ErrProof = errors.New("proof does not verify")

// Verify checks the account proof against stateRoot and every storage proof
// against the proven account's storage root, and returns the proven slot
// values. A non-existent account proves as empty (zero storage root); a slot
// absent from the trie proves as zero.
func (p AccountProof) Verify(stateRoot common.Hash) (map[common.Hash]*big.Int, error) {
	accountKey := crypto.Keccak256(p.Address.Bytes())
	accRLP, err := verifyBranch(stateRoot, accountKey, p.AccountProof)
	if err != nil {
		return nil, fmt.Errorf("%w: account %s: %v", ErrProof, p.Address, err)
	}
	storageRoot := types.EmptyRootHash
	if len(accRLP) > 0 {
		var acc types.StateAccount
		if err := rlp.DecodeBytes(accRLP, &acc); err != nil {
			return nil, fmt.Errorf("%w: account %s: %v", ErrProof, p.Address, err)
		}
		storageRoot = acc.Root
		if acc.Root != p.StorageHash || acc.CodeHash == nil || !bytes.Equal(acc.CodeHash, p.CodeHash.Bytes()) || acc.Nonce != p.Nonce || (p.Balance != nil && acc.Balance.ToBig().Cmp(p.Balance) != 0) {
			return nil, fmt.Errorf("%w: account %s: the proven account differs from the reported one", ErrProof, p.Address)
		}
	} else if p.StorageHash != (common.Hash{}) && p.StorageHash != types.EmptyRootHash {
		return nil, fmt.Errorf("%w: account %s absent but a storage root was reported", ErrProof, p.Address)
	}
	values := make(map[common.Hash]*big.Int, len(p.Storage))
	for _, sp := range p.Storage {
		slotKey := crypto.Keccak256(sp.Key.Bytes())
		var raw []byte
		if storageRoot != types.EmptyRootHash {
			raw, err = verifyBranch(storageRoot, slotKey, sp.Proof)
			if err != nil {
				return nil, fmt.Errorf("%w: slot %s: %v", ErrProof, sp.Key, err)
			}
		}
		v := new(big.Int)
		if len(raw) > 0 {
			var content []byte
			if err := rlp.DecodeBytes(raw, &content); err != nil {
				return nil, fmt.Errorf("%w: slot %s: %v", ErrProof, sp.Key, err)
			}
			v.SetBytes(content)
		}
		if sp.Value != nil && v.Cmp(sp.Value) != 0 {
			return nil, fmt.Errorf("%w: slot %s: proven %s, reported %s", ErrProof, sp.Key, v, sp.Value)
		}
		values[sp.Key] = v
	}
	return values, nil
}

// verifyBranch verifies a Merkle-Patricia proof (hex-encoded RLP nodes)
// for key under root and returns the value (nil when the key is absent).
func verifyBranch(root common.Hash, key []byte, proof []string) ([]byte, error) {
	db := memorydb.New()
	for _, n := range proof {
		node, err := hexutil.Decode(n)
		if err != nil {
			return nil, err
		}
		if err := db.Put(crypto.Keccak256(node), node); err != nil {
			return nil, err
		}
	}
	if len(proof) == 0 && root != types.EmptyRootHash {
		return nil, errors.New("empty proof for a non-empty trie")
	}
	return trie.VerifyProof(root, key, db)
}
