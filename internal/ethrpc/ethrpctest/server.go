// Package ethrpctest is a fake execution-layer JSON-RPC server for tests: it
// answers eth_call from handlers registered per contract and method, with the
// outputs ABI-packed by the test, and serves one pinned block.
package ethrpctest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Handler answers one method: it receives the unpacked inputs and returns
// the output values to pack.
type Handler func(args []any) ([]any, error)

// Server is the fake node.
type Server struct {
	*httptest.Server
	mu       sync.Mutex
	chainID  uint64
	head     uint64
	blocks   map[uint64]Block
	handlers map[string]map[string]Handler // addr → selector hex → handler
	abis     map[string]*gethabi.ABI       // addr → abi
	code     map[string][]byte
	Calls    []string
}

// Block is a served block.
type Block struct {
	Number    uint64
	Hash      common.Hash
	StateRoot common.Hash
	Timestamp uint64
}

// New starts a server with one chain id and a head block.
func New(chainID uint64, head Block) *Server {
	s := &Server{chainID: chainID, head: head.Number, blocks: map[uint64]Block{head.Number: head}, handlers: map[string]map[string]Handler{}, abis: map[string]*gethabi.ABI{}, code: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// AddBlock serves another block.
func (s *Server) AddBlock(b Block) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocks[b.Number] = b
	if b.Number > s.head {
		s.head = b.Number
	}
}

// Contract registers an ABI at an address with some code bytes.
func (s *Server) Contract(addr common.Address, a *gethabi.ABI, code []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := strings.ToLower(addr.Hex())
	s.abis[k] = a
	s.code[k] = code
	if s.handlers[k] == nil {
		s.handlers[k] = map[string]Handler{}
	}
}

// On registers a method handler on a contract.
func (s *Server) On(addr common.Address, method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := strings.ToLower(addr.Hex())
	a := s.abis[k]
	if a == nil {
		panic("ethrpctest: no contract at " + addr.Hex())
	}
	m, ok := a.Methods[method]
	if !ok {
		panic("ethrpctest: no method " + method)
	}
	s.handlers[k][hexutil.Encode(m.ID)] = h
}

// Returns registers a handler answering constant values.
func (s *Server) Returns(addr common.Address, method string, values ...any) {
	s.On(addr, method, func([]any) ([]any, error) { return values, nil })
}

type request struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	// Batches are not needed by the client; single requests only.
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.Calls = append(s.Calls, req.Method)
	s.mu.Unlock()
	result, rpcErr := s.dispatch(req)
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) dispatch(req request) (any, *rpcError) {
	switch req.Method {
	case "eth_chainId":
		return hexutil.EncodeUint64(s.chainID), nil
	case "eth_getBlockByNumber":
		var tag string
		_ = json.Unmarshal(req.Params[0], &tag)
		return s.block(tag), nil
	case "eth_getBlockByHash":
		var h common.Hash
		_ = json.Unmarshal(req.Params[0], &h)
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, b := range s.blocks {
			if b.Hash == h {
				return blockJSON(b), nil
			}
		}
		return nil, nil
	case "eth_getCode":
		var addr common.Address
		_ = json.Unmarshal(req.Params[0], &addr)
		s.mu.Lock()
		defer s.mu.Unlock()
		return hexutil.Bytes(s.code[strings.ToLower(addr.Hex())]), nil
	case "eth_call":
		return s.call(req.Params)
	case "eth_getLogs":
		return []any{}, nil
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
}

func (s *Server) block(tag string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n uint64
	switch tag {
	case "latest", "finalized", "safe", "pending":
		n = s.head
	default:
		v, err := hexutil.DecodeUint64(tag)
		if err != nil {
			return nil
		}
		n = v
	}
	b, ok := s.blocks[n]
	if !ok {
		return nil
	}
	return blockJSON(b)
}

func blockJSON(b Block) map[string]any {
	return map[string]any{
		"number":    hexutil.EncodeUint64(b.Number),
		"hash":      b.Hash,
		"stateRoot": b.StateRoot,
		"timestamp": hexutil.EncodeUint64(b.Timestamp),
	}
}

func (s *Server) call(params []json.RawMessage) (any, *rpcError) {
	var callObj struct {
		To   common.Address `json:"to"`
		Data hexutil.Bytes  `json:"data"`
	}
	if err := json.Unmarshal(params[0], &callObj); err != nil {
		return nil, &rpcError{Code: -32602, Message: err.Error()}
	}
	if len(callObj.Data) < 4 {
		return nil, &rpcError{Code: -32602, Message: "short calldata"}
	}
	k := strings.ToLower(callObj.To.Hex())
	s.mu.Lock()
	a := s.abis[k]
	h := s.handlers[k][hexutil.Encode(callObj.Data[:4])]
	s.mu.Unlock()
	if a == nil {
		return hexutil.Bytes{}, nil // no code: empty answer, as a node would
	}
	m, err := a.MethodById(callObj.Data[:4])
	if err != nil {
		return nil, &rpcError{Code: 3, Message: "execution reverted", Data: hexutil.Encode(revertReason("unknown selector"))}
	}
	if h == nil {
		return nil, &rpcError{Code: 3, Message: "execution reverted", Data: hexutil.Encode(revertReason("ethrpctest: no handler for " + m.Name))}
	}
	args, err := m.Inputs.Unpack(callObj.Data[4:])
	if err != nil {
		return nil, &rpcError{Code: -32602, Message: err.Error()}
	}
	outs, err := h(args)
	if err != nil {
		return nil, &rpcError{Code: 3, Message: "execution reverted", Data: hexutil.Encode(revertReason(err.Error()))}
	}
	packed, err := m.Outputs.Pack(outs...)
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: fmt.Sprintf("ethrpctest: pack %s: %v", m.Name, err)}
	}
	return hexutil.Bytes(packed), nil
}

// revertReason encodes Error(string).
func revertReason(msg string) []byte {
	str, _ := gethabi.NewType("string", "", nil)
	packed, _ := gethabi.Arguments{{Type: str}}.Pack(msg)
	return append([]byte{0x08, 0xc3, 0x79, 0xa0}, packed...)
}
