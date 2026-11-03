package ethrpc

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestThrottleRetriesRateLimitsWithTheBody(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"q":1}` {
			t.Errorf("attempt %d lost the body: %q", calls.Load(), body)
		}
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := &http.Client{Transport: &throttle{next: http.DefaultTransport, interval: time.Millisecond, backoff: time.Millisecond}}
	resp, err := c.Post(srv.URL, "application/json", bytes.NewReader([]byte(`{"q":1}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 3 {
		t.Fatalf("status %d after %d calls", resp.StatusCode, calls.Load())
	}
}

func TestThrottleGivesUpAndSpaces(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := &http.Client{Transport: &throttle{next: http.DefaultTransport, interval: 20 * time.Millisecond, backoff: time.Millisecond}}
	start := time.Now()
	resp, err := c.Post(srv.URL, "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || calls.Load() != maxRateLimitRetries+1 {
		t.Fatalf("status %d after %d calls", resp.StatusCode, calls.Load())
	}
	if el := time.Since(start); el < time.Duration(maxRateLimitRetries)*20*time.Millisecond {
		t.Fatalf("%d requests in %v: not spaced", calls.Load(), el)
	}
}
