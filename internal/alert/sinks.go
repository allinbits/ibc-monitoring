package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/redact"
)

// LogSink writes alarm transitions to the structured log.
type LogSink struct {
	Log *slog.Logger
}

func (LogSink) Name() string { return "log" }

func (s LogSink) Send(_ context.Context, e model.AlarmEvent) error {
	l := s.Log
	if l == nil {
		l = slog.Default()
	}
	l.Warn("alarm "+e.Type, "key", e.Alarm.Key, "check", e.Alarm.Check, "severity", e.Alarm.Severity, "title", e.Alarm.Title, "detail", e.Alarm.Detail, "autoflag", e.Alarm.AutoFlag)
	return nil
}

// WebhookSink POSTs each transition as JSON to a URL. With a secret, the body
// is signed: X-Ibcmon-Signature: sha256=<hex hmac>.
type WebhookSink struct {
	URL     string
	Secret  string
	Client  *http.Client
	Retries int
}

func (WebhookSink) Name() string { return "webhook" }

func (s WebhookSink) Send(ctx context.Context, e model.AlarmEvent) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	retries := s.Retries
	if retries <= 0 {
		retries = 3
	}
	var last error
	for attempt := 0; attempt < retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "ibcmon")
		if s.Secret != "" {
			mac := hmac.New(sha256.New, []byte(s.Secret))
			mac.Write(body)
			req.Header.Set("X-Ibcmon-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		resp, err := client.Do(req)
		if err != nil {
			last = redact.Error(err)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		last = fmt.Errorf("webhook: http %d", resp.StatusCode)
	}
	return last
}

// TODO(phase 3, docs/design.md §8.4): AutoFlagSink, which signs the ADR's
// containment flags with the watch keys for findings with AutoFlag set,
// behind autoflag.enabled and the watch-key environment variables, with a
// dry-run mode. It must never clear a flag, set a `sends` flag or vote.
