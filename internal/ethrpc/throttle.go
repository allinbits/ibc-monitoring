package ethrpc

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// throttle is an http.RoundTripper that spaces requests at least interval
// apart and retries an HTTP 429 answer a few times with a growing backoff:
// public RPCs (Arc's official one) rate-limit the bursts a collection makes.
type throttle struct {
	next     http.RoundTripper
	interval time.Duration
	backoff  time.Duration

	mu   sync.Mutex
	last time.Time
}

const maxRateLimitRetries = 4

func (t *throttle) wait(req *http.Request) error {
	t.mu.Lock()
	at := t.last.Add(t.interval)
	now := time.Now()
	if at.Before(now) {
		at = now
	}
	t.last = at
	t.mu.Unlock()
	return sleep(req, time.Until(at))
}

func sleep(req *http.Request, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-req.Context().Done():
		return req.Context().Err()
	}
}

func (t *throttle) RoundTrip(req *http.Request) (*http.Response, error) {
	backoff := t.backoff
	for attempt := 0; ; attempt++ {
		if err := t.wait(req); err != nil {
			return nil, err
		}
		resp, err := t.next.RoundTrip(req)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests || attempt == maxRateLimitRetries || req.GetBody == nil {
			return resp, err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err := sleep(req, backoff); err != nil {
			return nil, err
		}
		backoff *= 2
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		req = req.Clone(req.Context())
		req.Body = body
	}
}

// WithMaxRPS spaces the requests to the node at most rps a second and
// retries rate-limited (HTTP 429) answers with a backoff. Zero leaves
// requests unthrottled.
func WithMaxRPS(rps float64) DialOption {
	return func(c *dialConfig) { c.maxRPS = rps }
}
