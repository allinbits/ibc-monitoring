package ethrpc

import (
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

func filterQuery(addr common.Address, topics [][]common.Hash, from, to uint64) ethereum.FilterQuery {
	return ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(from),
		ToBlock:   new(big.Int).SetUint64(to),
		Addresses: []common.Address{addr},
		Topics:    topics,
	}
}
