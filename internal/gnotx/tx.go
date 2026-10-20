// Package gnotx decodes the transactions tm2's `block` RPC returns (amino
// binary std.Tx) far enough to find the realm calls in them: for each
// message of type /vm.m_call, the caller, the package path, the function
// and the string arguments. Nothing else of amino is implemented.
//
// Amino's binary form is protobuf wire format: std.Tx{Msgs: field 1
// repeated, Fee: 2, Signatures: 3, Memo: 4}; an interface value (a Msg) is
// an Any{type_url: field 1 string, value: field 2 bytes}; vm.MsgCall{Caller:
// 1, Send: 2, MaxDeposit: 3, PkgPath: 4, Func: 5, Args: 6 repeated string}.
//
// The decoder reads incrementally under explicit limits and keeps nothing it
// does not need: a hostile transaction costs at most the limits, and one
// past them is refused, never partially trusted (docs/design.md §4.1).
package gnotx

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// MsgCall is a realm call.
type MsgCall struct {
	Index   int // the message's index in the transaction
	Caller  string
	PkgPath string
	Func    string
	Args    []string
}

// Tx is the decoded part of a transaction.
type Tx struct {
	// Calls lists the /vm.m_call messages with their indexes; Other lists
	// the indexes of messages of other types (a MsgRun, a bank send), which
	// the monitor does not decode. Messages counts them all.
	Calls    []MsgCall
	Other    []int
	Messages int
	Memo     string
}

// MsgCallType is amino's type URL of vm.MsgCall.
const MsgCallType = "/vm.m_call"

// Limits bound what a transaction may cost to decode.
type Limits struct {
	MaxTxBytes   int // the whole transaction
	MaxFields    int // fields scanned per message (unknown ones included)
	MaxMessages  int // messages per transaction
	MaxArgs      int // arguments per call
	MaxValueSize int // any string or bytes field
}

// DefaultLimits are generous for any honest relay transaction.
var DefaultLimits = Limits{MaxTxBytes: 1 << 20, MaxFields: 4096, MaxMessages: 64, MaxArgs: 64, MaxValueSize: 512 << 10}

var (
	errWire  = errors.New("gnotx: malformed amino bytes")
	ErrLimit = errors.New("gnotx: transaction past a decoding limit")
)

// scanner walks one message's fields without materialising them.
type scanner struct {
	b      []byte
	limits Limits
	seen   int
}

// next returns the next field (number, wire type, varint value or
// length-delimited payload); ok is false at the end.
func (s *scanner) next() (num, wire uint64, v uint64, b []byte, ok bool, err error) {
	if len(s.b) == 0 {
		return 0, 0, 0, nil, false, nil
	}
	s.seen++
	if s.seen > s.limits.MaxFields {
		return 0, 0, 0, nil, false, fmt.Errorf("%w: more than %d fields", ErrLimit, s.limits.MaxFields)
	}
	tag, n := binary.Uvarint(s.b)
	if n <= 0 {
		return 0, 0, 0, nil, false, errWire
	}
	s.b = s.b[n:]
	num, wire = tag>>3, tag&7
	switch wire {
	case 0:
		v, n = binary.Uvarint(s.b)
		if n <= 0 {
			return 0, 0, 0, nil, false, errWire
		}
		s.b = s.b[n:]
	case 1:
		if len(s.b) < 8 {
			return 0, 0, 0, nil, false, errWire
		}
		v = binary.LittleEndian.Uint64(s.b[:8])
		s.b = s.b[8:]
	case 2:
		l, n := binary.Uvarint(s.b)
		if n <= 0 || l > uint64(len(s.b)-n) {
			return 0, 0, 0, nil, false, errWire
		}
		if l > uint64(s.limits.MaxValueSize) {
			return 0, 0, 0, nil, false, fmt.Errorf("%w: a field of %d bytes", ErrLimit, l)
		}
		b = s.b[n : n+int(l)]
		s.b = s.b[n+int(l):]
	case 5:
		if len(s.b) < 4 {
			return 0, 0, 0, nil, false, errWire
		}
		v = uint64(binary.LittleEndian.Uint32(s.b[:4]))
		s.b = s.b[4:]
	default:
		return 0, 0, 0, nil, false, fmt.Errorf("gnotx: unsupported wire type %d", wire)
	}
	return num, wire, v, b, true, nil
}

// Decode decodes a std.Tx under the default limits.
func Decode(b []byte) (Tx, error) { return DecodeWithLimits(b, DefaultLimits) }

// DecodeWithLimits decodes a std.Tx under the given limits.
func DecodeWithLimits(b []byte, limits Limits) (Tx, error) {
	if len(b) > limits.MaxTxBytes {
		return Tx{}, fmt.Errorf("%w: %d bytes", ErrLimit, len(b))
	}
	var tx Tx
	s := &scanner{b: b, limits: limits}
	for {
		num, wire, _, payload, ok, err := s.next()
		if err != nil {
			return Tx{}, err
		}
		if !ok {
			break
		}
		switch {
		case num == 1 && wire == 2:
			idx := tx.Messages
			tx.Messages++
			if tx.Messages > limits.MaxMessages {
				return Tx{}, fmt.Errorf("%w: more than %d messages", ErrLimit, limits.MaxMessages)
			}
			typeURL, value, err := decodeAny(payload, limits)
			if err != nil {
				return Tx{}, err
			}
			if typeURL != MsgCallType {
				tx.Other = append(tx.Other, idx)
				continue
			}
			call, err := decodeMsgCall(value, limits)
			if err != nil {
				return Tx{}, err
			}
			call.Index = idx
			tx.Calls = append(tx.Calls, call)
		case num == 4 && wire == 2:
			tx.Memo = string(payload)
		}
	}
	return tx, nil
}

func decodeAny(b []byte, limits Limits) (typeURL string, value []byte, err error) {
	s := &scanner{b: b, limits: limits}
	for {
		num, wire, _, payload, ok, err := s.next()
		if err != nil {
			return "", nil, err
		}
		if !ok {
			return typeURL, value, nil
		}
		switch {
		case num == 1 && wire == 2:
			typeURL = string(payload)
		case num == 2 && wire == 2:
			value = payload
		}
	}
}

func decodeMsgCall(b []byte, limits Limits) (MsgCall, error) {
	s := &scanner{b: b, limits: limits}
	var m MsgCall
	for {
		num, wire, _, payload, ok, err := s.next()
		if err != nil {
			return MsgCall{}, err
		}
		if !ok {
			break
		}
		if wire != 2 {
			continue
		}
		switch num {
		case 1:
			m.Caller = string(payload)
		case 4:
			m.PkgPath = string(payload)
		case 5:
			m.Func = string(payload)
		case 6:
			if len(m.Args) >= limits.MaxArgs {
				return MsgCall{}, fmt.Errorf("%w: more than %d arguments", ErrLimit, limits.MaxArgs)
			}
			m.Args = append(m.Args, string(payload))
		}
	}
	if m.PkgPath == "" || m.Func == "" {
		return MsgCall{}, errors.New("gnotx: m_call without pkg_path or func")
	}
	return m, nil
}
