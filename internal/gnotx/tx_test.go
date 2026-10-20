package gnotx

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
)

// Fixture generated with tm2's amino codec (gnolang/gno at 794aafcc):
// std.Tx with two vm.MsgCall messages to gno.land/r/aib/ibc/ethrelay
// (RecvPacket and Acknowledgement, each with one base64 argument), a fee and
// the memo "fixture". The arguments are the ethrelaywire fixtures.
const txHex = "0a81020a0a2f766d2e6d5f63616c6c12f2010a2867316a67386d74757475396b6868667763346e786d756863706674663070616a6468667673716635221b676e6f2e6c616e642f722f6169622f6962632f65746872656c61792a0a526563765061636b6574329c01414141414141414141416349593278705a5735304c54414b5a58526f5a584a6c645730744d5141414141427035694e5141516830636d467563325a6c63676830636d467563325a6c6367647059334d794d433078476d46776347787059324630615739754c336774633239736157527064486b7459574a704174367441414141414141414141414141414141414a69576741634241514542416749440aa2020a0a2f766d2e6d5f63616c6c1293020a2867316a67386d74757475396b6868667763346e786d756863706674663070616a6468667673716635221b676e6f2e6c616e642f722f6169622f6962632f65746872656c61792a0f41636b6e6f776c656467656d656e7432b801414141414141414141416349593278705a5735304c54414b5a58526f5a584a6c645730744d5141414141427035694e5141516830636d467563325a6c63676830636d467563325a6c6367647059334d794d433078476d46776347787059324630615739754c336774633239736157527064486b7459574a704174367441414141414141414141414141414141414a69576751455265794a795a584e31624851694f694a4255513039496e3048415145424151494341773d3d1214088084af5f120d313030303030303075676e6f74220766697874757265"

func TestDecodeAminoTx(t *testing.T) {
	b, _ := hex.DecodeString(txHex)
	tx, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Memo != "fixture" || len(tx.Other) != 0 || tx.Messages != 2 || len(tx.Calls) != 2 {
		t.Fatalf("tx %+v", tx)
	}
	c := tx.Calls[0]
	if c.Index != 0 || tx.Calls[1].Index != 1 {
		t.Fatalf("indexes %d %d", c.Index, tx.Calls[1].Index)
	}
	if c.Caller != "g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5" || c.PkgPath != "gno.land/r/aib/ibc/ethrelay" || c.Func != "RecvPacket" || len(c.Args) != 1 {
		t.Fatalf("call %+v", c)
	}
	if c.Args[0] != "AAAAAAAAAAcIY2xpZW50LTAKZXRoZXJldW0tMQAAAABp5iNQAQh0cmFuc2Zlcgh0cmFuc2ZlcgdpY3MyMC0xGmFwcGxpY2F0aW9uL3gtc29saWRpdHktYWJpAt6tAAAAAAAAAAAAAAAAAJiWgAcBAQEBAgID" {
		t.Fatalf("arg %q", c.Args[0])
	}
	if tx.Calls[1].Func != "Acknowledgement" {
		t.Fatalf("second call %+v", tx.Calls[1])
	}
	// Garbage and truncation fail rather than mis-decode.
	if _, err := Decode(b[:len(b)-3]); err == nil {
		t.Fatal("truncated accepted")
	}
	if _, err := Decode([]byte{0x0a, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}); err == nil {
		t.Fatal("hostile length accepted")
	}
}

// Hostile inputs are refused at a bounded cost (review finding 7): a
// transaction past the size limit before anything is scanned, a field bomb
// at the field limit, too many messages, too many arguments, an oversized
// value; and unknown messages are counted by index, not kept.
func TestHostileInputs(t *testing.T) {
	limits := Limits{MaxTxBytes: 4096, MaxFields: 64, MaxMessages: 4, MaxArgs: 3, MaxValueSize: 128}
	// Size: refused without scanning.
	if _, err := DecodeWithLimits(make([]byte, 4097), limits); !errors.Is(err, ErrLimit) {
		t.Fatalf("size: %v", err)
	}
	// A field bomb: 100 two-byte varint fields of number 7.
	bomb := make([]byte, 0, 200)
	for i := 0; i < 100; i++ {
		bomb = append(bomb, 0x38, 0x01)
	}
	if _, err := DecodeWithLimits(bomb, limits); !errors.Is(err, ErrLimit) {
		t.Fatalf("field bomb: %v", err)
	}
	// Below the field limit the same unknown fields are skipped, not kept.
	if tx, err := DecodeWithLimits(bomb[:2*60], limits); err != nil || tx.Messages != 0 {
		t.Fatalf("unknown fields: %+v %v", tx, err)
	}
	// Too many messages (each an Any of another type).
	other := anyMsg("/vm.m_run", []byte{0x0a, 0x01, 0x41})
	var many []byte
	for i := 0; i < 5; i++ {
		many = append(many, field(1, other)...)
	}
	if _, err := DecodeWithLimits(many, limits); !errors.Is(err, ErrLimit) {
		t.Fatalf("messages: %v", err)
	}
	// Unknown messages are reported by index.
	tx, err := DecodeWithLimits(many[:len(many)/5*3], limits)
	if err != nil || tx.Messages != 3 || len(tx.Other) != 3 || tx.Other[2] != 2 {
		t.Fatalf("others: %+v %v", tx, err)
	}
	// Too many arguments.
	call := field(4, []byte("gno.land/r/x"))
	call = append(call, field(5, []byte("F"))...)
	for i := 0; i < 4; i++ {
		call = append(call, field(6, []byte("a"))...)
	}
	if _, err := DecodeWithLimits(field(1, anyMsg(MsgCallType, call)), limits); !errors.Is(err, ErrLimit) {
		t.Fatalf("args: %v", err)
	}
	// An oversized value.
	big := field(1, anyMsg(MsgCallType, append(append(field(4, []byte("gno.land/r/x")), field(5, []byte("F"))...), field(6, make([]byte, 129))...)))
	if _, err := DecodeWithLimits(big, limits); !errors.Is(err, ErrLimit) {
		t.Fatalf("value: %v", err)
	}
}

// field encodes a length-delimited protobuf field.
func field(num uint64, payload []byte) []byte {
	out := binary.AppendUvarint(nil, num<<3|2)
	out = binary.AppendUvarint(out, uint64(len(payload)))
	return append(out, payload...)
}

// anyMsg encodes amino's Any{type_url, value}.
func anyMsg(typeURL string, value []byte) []byte {
	return append(field(1, []byte(typeURL)), field(2, value)...)
}
