package ethrpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A provider that accepts a request and never answers fails the request
// within the configured bound (review finding 9).
func TestRequestTimeout(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-done }))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(done) })
	c, err := Dial(context.Background(), srv.URL, "hanging", Finality{Mode: "latest"}, WithRequestTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := c.Pinned(context.Background()); err == nil {
		t.Fatal("a hanging provider answered")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("the request was not bounded: %s", time.Since(start))
	}
}
