// Package gnorpctest is a fake tm2 RPC server answering from a recorded
// fixture, for tests of the gno views and the checks (docs/design.md §13).
package gnorpctest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"time"
)

// Fixture is a recording: the node's status and every abci_query answer keyed
// by "<path>|<data>" (data as sent, e.g. "gno.land/r/aib/ibc/core.ClientIDs()").
type Fixture struct {
	ChainID string            `json:"chain_id"`
	Height  int64             `json:"height"`
	Hash    string            `json:"hash"`
	Time    time.Time         `json:"time"`
	AppHash string            `json:"app_hash,omitempty"`
	Queries map[string]string `json:"queries"`
	// Errors maps a query key to an ABCI error message.
	Errors map[string]string `json:"errors,omitempty"`
}

// Load reads a fixture file.
func Load(path string) (*Fixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f Fixture
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Queries == nil {
		f.Queries = map[string]string{}
	}
	return &f, nil
}

// Server serves a fixture over HTTP as a tm2 node would.
type Server struct {
	*httptest.Server
	mu      sync.Mutex
	fixture *Fixture
	// Unknown records the keys of queries the fixture did not hold.
	Unknown []string
}

// New starts a server for the fixture; close it with Close.
func New(f *Fixture) *Server {
	s := &Server{fixture: f}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Set replaces or adds one query answer.
func (s *Server) Set(key, answer string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fixture.Queries[key] = answer
}

type request struct {
	ID     json.RawMessage        `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var result any
	switch req.Method {
	case "status":
		result = s.status()
	case "block":
		result = s.block()
	case "abci_query":
		result = s.abciQuery(req.Params)
	case "block_results":
		result = map[string]any{"results": map[string]any{"deliver_tx": []any{}}}
	default:
		s.reply(w, req.ID, nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method})
		return
	}
	s.reply(w, req.ID, result, nil)
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (s *Server) reply(w http.ResponseWriter, id json.RawMessage, result any, rpcErr *rpcError) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func b64(hexOrText string) string {
	return base64.StdEncoding.EncodeToString([]byte(hexOrText))
}

func (s *Server) status() any {
	f := s.fixture
	return map[string]any{
		"node_info": map[string]any{"network": f.ChainID},
		"sync_info": map[string]any{
			"latest_block_hash":   base64.StdEncoding.EncodeToString(mustHex(f.Hash)),
			"latest_block_height": strconv.FormatInt(f.Height, 10),
			"latest_block_time":   f.Time,
			"catching_up":         false,
		},
	}
}

func (s *Server) block() any {
	f := s.fixture
	return map[string]any{
		"block_meta": map[string]any{
			"block_id": map[string]any{"hash": base64.StdEncoding.EncodeToString(mustHex(f.Hash))},
			"header": map[string]any{
				"chain_id": f.ChainID,
				"height":   strconv.FormatInt(f.Height, 10),
				"time":     f.Time,
				"app_hash": base64.StdEncoding.EncodeToString(mustHex(f.AppHash)),
			},
		},
	}
}

func mustHex(s string) []byte {
	out := make([]byte, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		v, err := strconv.ParseUint(s[i:i+2], 16, 8)
		if err != nil {
			return []byte(s)
		}
		out = append(out, byte(v))
	}
	return out
}

func (s *Server) abciQuery(params map[string]interface{}) any {
	path, _ := params["path"].(string)
	dataB64, _ := params["data"].(string)
	data, _ := base64.StdEncoding.DecodeString(dataB64)
	key := path + "|" + string(data)
	s.mu.Lock()
	answer, ok := s.fixture.Queries[key]
	errMsg, isErr := s.fixture.Errors[key]
	if !ok && !isErr {
		s.Unknown = append(s.Unknown, key)
	}
	s.mu.Unlock()
	base := map[string]any{"Error": nil, "Data": nil, "Events": nil, "Log": "", "Info": ""}
	if isErr {
		base["Error"] = map[string]any{"@type": "/std.InternalError", "value": errMsg}
		base["Log"] = errMsg
	} else if !ok {
		base["Error"] = map[string]any{"@type": "/std.InternalError", "value": "fixture: unknown query " + key}
		base["Log"] = "fixture: unknown query " + key
	} else {
		base["Data"] = b64(answer)
	}
	return map[string]any{"response": map[string]any{
		"ResponseBase": base,
		"Key":          nil,
		"Value":        nil,
		"Proof":        nil,
		"Height":       strconv.FormatInt(s.fixture.Height, 10),
	}}
}
