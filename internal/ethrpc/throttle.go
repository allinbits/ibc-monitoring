package ethrpc

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// throttle is an http.RoundTripper that spaces requests at least interval
// apart and retries an HTTP 429 answer a few times with a growing backoff:
// public RPCs (Arc's official one) rate-limit the bursts a collection makes.
// With attempt set, it also retries a request whose answer has not started
// within attempt: free public RPCs (drpc's) answer in a fraction of a second
// but now and then hold a request until the client's timeout. Every request
// the monitor makes is a read, so a retry is harmless.
type throttle struct {
	next     http.RoundTripper
	interval time.Duration
	backoff  time.Duration
	attempt  time.Duration

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
		resp, slow, err := t.try(req)
		last := attempt == maxRateLimitRetries || req.GetBody == nil
		switch {
		case slow && !last && req.Context().Err() == nil:
			// retried at once: the wait above spaces it
		case err != nil || resp.StatusCode != http.StatusTooManyRequests || last:
			return resp, err
		default:
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if err := sleep(req, backoff); err != nil {
				return nil, err
			}
			backoff *= 2
		}
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		req = req.Clone(req.Context())
		req.Body = body
	}
}

// try sends req once; slow is true when it was abandoned because its answer
// had not started within t.attempt.
func (t *throttle) try(req *http.Request) (resp *http.Response, slow bool, err error) {
	if t.attempt <= 0 {
		resp, err = t.next.RoundTrip(req)
		return resp, false, err
	}
	ctx, cancel := context.WithCancel(req.Context())
	timer := time.AfterFunc(t.attempt, cancel)
	resp, err = t.next.RoundTrip(req.WithContext(ctx))
	if !timer.Stop() {
		// The timer fired: this attempt was cancelled (or answered just as
		// it was, and its body would be cut off).
		if resp != nil {
			resp.Body.Close()
		}
		cancel()
		return nil, true, ctx.Err()
	}
	if err != nil {
		cancel()
		return nil, false, err
	}
	resp.Body = cancelOnClose{resp.Body, cancel}
	return resp, false, nil
}

// cancelOnClose releases an attempt's context when its body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// WithMaxRPS spaces the requests to the node at most rps a second and
// retries rate-limited (HTTP 429) answers with a backoff. Zero leaves
// requests unthrottled.
func WithMaxRPS(rps float64) DialOption {
	return func(c *dialConfig) { c.maxRPS = rps }
}
