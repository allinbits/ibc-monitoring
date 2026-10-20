package ibcpacket

import (
	"encoding/hex"
	"testing"
)

// The vector of gno-realms p/aib/ibc/types/v0 commitment_test.gno
// (TestCommitPacket, "json packet"): the same bytes both implementations commit.
func TestCommitMatchesGno(t *testing.T) {
	p := Packet{
		Sequence:          1,
		SourceClient:      "07-tendermint-0",
		DestinationClient: "07-tendermint-1",
		TimeoutTimestamp:  100,
		Payloads: []Payload{{
			SourcePort:      "transfer",
			DestinationPort: "transfer",
			Version:         "ics20-1",
			Encoding:        "application/json",
			Value:           []byte(`{"denom":"uatom","amount":"1000000","sender":"sender","receiver":"receiver","memo":"memo"}`),
		}},
	}
	c := Commit(p)
	if got := hex.EncodeToString(c[:]); got != "a096722aa6534040a0efbdae05765132a7b223ad306d6512f3734821bd046505" {
		t.Fatalf("commitment %s", got)
	}
	// The sequence is not part of the commitment (it is in the key); every
	// other field is.
	q := p
	q.Sequence = 2
	if Commit(q) != c {
		t.Fatal("sequence must not change the commitment")
	}
	q = p
	q.Payloads[0].Value = []byte(`{"denom":"uatom","amount":"1000001"}`)
	if Commit(q) == c {
		t.Fatal("payload must change the commitment")
	}
}

func TestPathAndScope(t *testing.T) {
	if got := hex.EncodeToString(Path("eth-0", KindPacketCommitment, 58)); got != hex.EncodeToString([]byte("eth-0"))+"01"+"000000000000003a" {
		t.Fatal(got)
	}
	xfer := Packet{Payloads: []Payload{{SourcePort: "transfer", DestinationPort: "transfer"}}}
	gov := Packet{Payloads: []Payload{{SourcePort: "gov", DestinationPort: "ibcgov"}}}
	notice := Packet{Payloads: []Payload{{SourcePort: "ibcgov", DestinationPort: "gov"}}}
	if !IsTransfer(xfer) || IsTransfer(gov) || IsTransfer(Packet{}) {
		t.Fatal("transfer scope")
	}
	if !IsGovernance(gov) || !IsGovernance(notice) || IsGovernance(xfer) {
		t.Fatal("governance scope")
	}
}
