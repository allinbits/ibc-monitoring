// Package model holds the types shared by the data layer, the monitor, the
// worker and the API. Their JSON encoding is the API contract the console
// reads: every amount is a decimal string, every height an integer, every
// time an RFC 3339 string.
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Amount is a token amount in base units. It is never a float and never a
// JavaScript Number: it marshals as a decimal string and unmarshals from a
// decimal string or a bare JSON number token (the realms emit int64 as bare
// numbers, Ethereum views give uint256).
type Amount struct {
	i big.Int
}

// NewAmount returns an Amount holding v (nil means zero).
func NewAmount(v *big.Int) Amount {
	var a Amount
	if v != nil {
		a.i.Set(v)
	}
	return a
}

// AmountFromInt64 returns an Amount holding v.
func AmountFromInt64(v int64) Amount {
	var a Amount
	a.i.SetInt64(v)
	return a
}

// ParseAmount parses a decimal string (an optional leading minus sign is
// accepted, since residuals can be negative).
func ParseAmount(s string) (Amount, error) {
	var a Amount
	s = strings.TrimSpace(s)
	if s == "" {
		return a, errors.New("empty amount")
	}
	if _, ok := a.i.SetString(s, 10); !ok {
		return Amount{}, fmt.Errorf("invalid amount %q", s)
	}
	return a, nil
}

// MustAmount is ParseAmount for constants in tests and fixtures.
func MustAmount(s string) Amount {
	a, err := ParseAmount(s)
	if err != nil {
		panic(err)
	}
	return a
}

// Int returns a copy of the value.
func (a Amount) Int() *big.Int { return new(big.Int).Set(&a.i) }

// String returns the decimal representation.
func (a Amount) String() string { return a.i.String() }

// Sign returns -1, 0 or 1.
func (a Amount) Sign() int { return a.i.Sign() }

// IsZero reports whether the amount is zero.
func (a Amount) IsZero() bool { return a.i.Sign() == 0 }

// Cmp compares a and b.
func (a Amount) Cmp(b Amount) int { return a.i.Cmp(&b.i) }

// Add returns a + b.
func (a Amount) Add(b Amount) Amount {
	var r Amount
	r.i.Add(&a.i, &b.i)
	return r
}

// Sub returns a − b.
func (a Amount) Sub(b Amount) Amount {
	var r Amount
	r.i.Sub(&a.i, &b.i)
	return r
}

// MarshalJSON encodes the amount as a decimal string.
func (a Amount) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.i.String())
}

// UnmarshalJSON accepts "123", 123 and -5; never a float.
func (a *Amount) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		a.i.SetInt64(0)
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		s = str
	}
	if strings.ContainsAny(s, ".eE") {
		return fmt.Errorf("amount %q is not an integer", s)
	}
	parsed, err := ParseAmount(s)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// Seq is a uint64 identifier (a packet sequence, a decision number) that
// travels as a decimal string: JavaScript cannot hold a uint64 in a Number.
type Seq uint64

// MarshalJSON encodes the identifier as a decimal string.
func (q Seq) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatUint(uint64(q), 10))
}

// UnmarshalJSON accepts a decimal string or a bare integer token.
func (q *Seq) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*q = 0
		return nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("identifier %q: %w", s, err)
	}
	*q = Seq(v)
	return nil
}

func (q Seq) String() string { return strconv.FormatUint(uint64(q), 10) }
