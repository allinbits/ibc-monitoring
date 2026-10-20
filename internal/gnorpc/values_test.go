package gnorpc

import (
	"reflect"
	"testing"
)

func TestValues(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		v, err := One(`("ethereum-1" string)` + "\n\n")
		if err != nil {
			t.Fatal(err)
		}
		s, err := v.String()
		if err != nil || s != "ethereum-1" {
			t.Fatalf("got %q, %v", s, err)
		}
	})
	t.Run("string with newline and quotes", func(t *testing.T) {
		v, err := One(`("a\nb \"c\"" string)`)
		if err != nil {
			t.Fatal(err)
		}
		s, _ := v.String()
		if s != "a\nb \"c\"" {
			t.Fatalf("got %q", s)
		}
	})
	t.Run("integers and bools", func(t *testing.T) {
		vs, err := Values("(42 uint64)\n(true bool)\n(-7 int64)")
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) != 3 {
			t.Fatalf("got %d values", len(vs))
		}
		if u, _ := vs[0].Uint(); u != 42 {
			t.Fatalf("uint %d", u)
		}
		if b, _ := vs[1].Bool(); !b {
			t.Fatal("bool")
		}
		if i, _ := vs[2].Int(); i != -7 {
			t.Fatalf("int %d", i)
		}
		if _, err := vs[1].Int(); err == nil {
			t.Fatal("bool as int must fail")
		}
	})
	t.Run("string slice", func(t *testing.T) {
		v, err := One(`(slice[("payouts seat 1 at 170" string),("sends seat 4 at 171 naming a/1,b/2" string)] []string)`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := v.Strings()
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"payouts seat 1 at 170", "sends seat 4 at 171 naming a/1,b/2"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("empty and nil slices", func(t *testing.T) {
		for _, raw := range []string{`(slice[] []string)`, `(nil []string)`} {
			v, err := One(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := v.Strings()
			if err != nil || len(got) != 0 {
				t.Fatalf("%s: got %v, %v", raw, got, err)
			}
		}
	})
	t.Run("persisted slice is refused", func(t *testing.T) {
		v, err := One(`(slice[ref(a8ada09dee16d791fd406d629fe29bb0ed084a30:3)] []string)`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.Strings(); err == nil {
			t.Fatal("expected an error for a slice of references")
		}
	})
	t.Run("garbage", func(t *testing.T) {
		if _, err := Values("not a value"); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("one refuses two", func(t *testing.T) {
		if _, err := One("(1 int)\n(2 int)"); err == nil {
			t.Fatal("expected an error")
		}
	})
}
