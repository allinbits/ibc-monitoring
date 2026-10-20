package model

import (
	"encoding/json"
	"testing"
)

func TestAmountJSON(t *testing.T) {
	// Above int64 and above 2^53: must survive a round trip exactly.
	const big = "340282366920938463463374607431768211455"
	for _, in := range []string{`"` + big + `"`, big, `123`, `"-5"`, `0`} {
		var a Amount
		if err := json.Unmarshal([]byte(in), &a); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		out, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		want := in
		if want[0] != '"' {
			want = `"` + want + `"`
		}
		if string(out) != want {
			t.Fatalf("%s: round trip gave %s", in, out)
		}
	}
	for _, bad := range []string{`1.5`, `"1e3"`, `"abc"`, `""`} {
		var a Amount
		if err := json.Unmarshal([]byte(bad), &a); err == nil {
			t.Fatalf("%s: expected an error", bad)
		}
	}
	var s struct {
		A Amount `json:"a"`
	}
	if err := json.Unmarshal([]byte(`{"a": null}`), &s); err != nil || !s.A.IsZero() {
		t.Fatalf("null: %v %v", s.A, err)
	}
}

func TestAmountArithmetic(t *testing.T) {
	a := MustAmount("10")
	b := MustAmount("25")
	if got := a.Sub(b).String(); got != "-15" {
		t.Fatalf("sub: %s", got)
	}
	if got := a.Add(b).String(); got != "35" {
		t.Fatalf("add: %s", got)
	}
	same := MustAmount("10")
	if a.Cmp(b) >= 0 || b.Cmp(a) <= 0 || a.Cmp(same) != 0 {
		t.Fatal("cmp")
	}
	// Add must not alias its operands.
	c := a.Add(b)
	if a.String() != "10" || c.String() != "35" {
		t.Fatal("aliasing")
	}
}
