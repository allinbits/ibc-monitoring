package redact

import (
	"errors"
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	cases := map[string]string{
		`Post "https://user:pw@rpc.example.com/v3/abcdef0123456789abcdef01": dial tcp: refused`: `Post "https://rpc.example.com/v3/[redacted]": dial tcp: refused`,
		`http://127.0.0.1:8545`:                            `http://127.0.0.1:8545`,
		`https://eth.example.com/?apikey=SECRET123 failed`: `https://eth.example.com failed`,
		`see https://host/path/x#frag.`:                    `see https://host/path/x.`,
		`wss://a.b/ws/ABCDEFGHIJKLMNOPQRSTUVWXYZ123456)`:   `wss://a.b/ws/[redacted])`,
		`no url here`: `no url here`,
		`two http://a.com/tokentokentokentokentoken and http://b.com/short`: `two http://a.com/[redacted] and http://b.com/short`,
	}
	for in, want := range cases {
		if got := String(in); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestRegisteredSecrets(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	Register("hunter2", "", "my-webhook-secret-value")
	got := String("signature with hunter2 and my-webhook-secret-value and hunter2")
	if strings.Contains(got, "hunter2") || strings.Contains(got, "my-webhook-secret-value") {
		t.Fatalf("leaked: %s", got)
	}
	err := Error(errors.New("token hunter2 in https://x.y/z?k=hunter2"))
	if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "k=") {
		t.Fatalf("leaked: %s", err)
	}
	if Error(nil) != nil {
		t.Fatal("nil error")
	}
}
