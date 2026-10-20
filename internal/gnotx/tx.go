// Package gnotx decodes the transactions tm2's `block` RPC returns (amino
// binary std.Tx) far enough to find the realm calls in them: for each
// message of type /vm.m_call, the caller, the package path, the function
// and the string arguments. Nothing else of amino is implemented.
//
// Amino's binary form is protobuf wire format: std.Tx{Msgs: field 1
// repeated, Fee: 2, Signatures: 3, Memo: 4}; an interface value (a Msg) is
// an Any{type_url: field 1 string, value: field 2 bytes}; vm.MsgCall{Caller:
// 1, Send: 2, MaxDeposit: 3, PkgPath: 4, Func: 5, Args: 6 repeated string}.
package gnotx

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// MsgCall is a realm call.
type MsgCall struct {
	Caller  string
	PkgPath string
	Func    string
	Args    []string
}

// Tx is the decoded part of a transaction.
type Tx struct {
	// Calls lists the /vm.m_call messages in order; Other counts messages of
	// other types (a MsgRun, a bank send), which the monitor does not decode.
	Calls []MsgCall
	Other int
	Memo  string
}

// MsgCallType is amino's type URL of vm.MsgCall.
const MsgCallType = "/vm.m_call"

var errWire = errors.New("gnotx: malformed amino bytes")

// field is one protobuf field: number, wire type and payload (varint value
// or length-delimited bytes).
type field struct {
	num  uint64
	wire uint64
	v    uint64
	b    []byte
}

// fields splits a message into its fields.
func fields(b []byte) ([]field, error) {
	var out []field
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errWire
		}
		b = b[n:]
		f := field{num: tag >> 3, wire: tag & 7}
		switch f.wire {
		case 0: // varint
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, errWire
			}
			f.v = v
			b = b[n:]
		case 1: // fixed64
			if len(b) < 8 {
				return nil, errWire
			}
			f.v = binary.LittleEndian.Uint64(b[:8])
			b = b[8:]
		case 2: // length-delimited
			l, n := binary.Uvarint(b)
			if n <= 0 || l > uint64(len(b)-n) {
				return nil, errWire
			}
			f.b = b[n : n+int(l)]
			b = b[n+int(l):]
		case 5: // fixed32
			if len(b) < 4 {
				return nil, errWire
			}
			f.v = uint64(binary.LittleEndian.Uint32(b[:4]))
			b = b[4:]
		default:
			return nil, fmt.Errorf("gnotx: unsupported wire type %d", f.wire)
		}
		out = append(out, f)
	}
	return out, nil
}

// Decode decodes a std.Tx.
func Decode(b []byte) (Tx, error) {
	var tx Tx
	fs, err := fields(b)
	if err != nil {
		return Tx{}, err
	}
	for _, f := range fs {
		switch {
		case f.num == 1 && f.wire == 2: // a message, as Any
			any, err := fields(f.b)
			if err != nil {
				return Tx{}, err
			}
			var typeURL string
			var value []byte
			for _, a := range any {
				switch {
				case a.num == 1 && a.wire == 2:
					typeURL = string(a.b)
				case a.num == 2 && a.wire == 2:
					value = a.b
				}
			}
			if typeURL != MsgCallType {
				tx.Other++
				continue
			}
			call, err := decodeMsgCall(value)
			if err != nil {
				return Tx{}, err
			}
			tx.Calls = append(tx.Calls, call)
		case f.num == 4 && f.wire == 2:
			tx.Memo = string(f.b)
		}
	}
	return tx, nil
}

func decodeMsgCall(b []byte) (MsgCall, error) {
	fs, err := fields(b)
	if err != nil {
		return MsgCall{}, err
	}
	var m MsgCall
	for _, f := range fs {
		if f.wire != 2 {
			continue
		}
		switch f.num {
		case 1:
			m.Caller = string(f.b)
		case 4:
			m.PkgPath = string(f.b)
		case 5:
			m.Func = string(f.b)
		case 6:
			m.Args = append(m.Args, string(f.b))
		}
	}
	if m.PkgPath == "" || m.Func == "" {
		return MsgCall{}, errors.New("gnotx: m_call without pkg_path or func")
	}
	return m, nil
}
