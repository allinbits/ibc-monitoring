package gnotx

import (
	"encoding/hex"
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
	if tx.Memo != "fixture" || tx.Other != 0 || len(tx.Calls) != 2 {
		t.Fatalf("tx %+v", tx)
	}
	c := tx.Calls[0]
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
