package alert

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

type recorder struct{ events []model.AlarmEvent }

func (*recorder) Name() string { return "rec" }
func (r *recorder) Send(_ context.Context, e model.AlarmEvent) error {
	r.events = append(r.events, e)
	return nil
}

func result(findings ...model.Finding) model.CheckResult {
	return model.CheckResult{Check: "c", Status: model.CheckAlarm, Findings: findings}
}

func TestManagerTransitions(t *testing.T) {
	st := store.NewMemory(10)
	rec := &recorder{}
	m := NewManager(st, nil, rec)
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	f := model.Finding{Subject: "x", Severity: model.SeverityWarning, Title: "x warns", AlarmKey: "c/x"}
	m.Apply(context.Background(), result(f))
	if len(rec.events) != 1 || rec.events[0].Type != "raised" {
		t.Fatalf("events %+v", rec.events)
	}
	// Same finding again: no transition.
	now = now.Add(time.Minute)
	m.Apply(context.Background(), result(f))
	if len(rec.events) != 1 {
		t.Fatalf("duplicate: %+v", rec.events)
	}
	a, _ := st.Alarm("c/x")
	if !a.LastSeen.Equal(now) || !a.FirstSeen.Equal(time.Unix(1000, 0)) {
		t.Fatalf("times %+v", a)
	}
	// Severity change: updated.
	f.Severity, f.Title = model.SeverityCritical, "x is critical"
	m.Apply(context.Background(), result(f))
	if len(rec.events) != 2 || rec.events[1].Type != "updated" || rec.events[1].Alarm.Severity != model.SeverityCritical {
		t.Fatalf("events %+v", rec.events)
	}
	// A skipped result keeps the alarm.
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckSkipped})
	if len(rec.events) != 2 {
		t.Fatalf("skip cleared: %+v", rec.events)
	}
	// The finding goes away: cleared.
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckOK})
	if len(rec.events) != 3 || rec.events[2].Type != "cleared" {
		t.Fatalf("events %+v", rec.events)
	}
	if len(st.Alarms(false)) != 0 || len(st.Alarms(true)) != 1 {
		t.Fatal("cleared alarm listing")
	}
	// Raised again after a clear: a fresh alarm.
	m.Apply(context.Background(), result(f))
	if len(rec.events) != 4 || rec.events[3].Type != "raised" {
		t.Fatalf("events %+v", rec.events)
	}
	// Another check's alarms are untouched by this check's results.
	st.PutAlarm(model.Alarm{Key: "d/y", Check: "d", State: model.AlarmRaised})
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckOK})
	if a, _ := st.Alarm("d/y"); a.State != model.AlarmRaised {
		t.Fatal("foreign alarm cleared")
	}
}

func TestWebhookSink(t *testing.T) {
	var got model.AlarmEvent
	var sig string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		sig = r.Header.Get("X-Ibcmon-Signature")
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	s := WebhookSink{URL: srv.URL, Secret: "s3cret"}
	e := model.AlarmEvent{Type: "raised", Alarm: model.Alarm{Key: "k", Title: "t"}}
	if err := s.Send(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if got.Alarm.Key != "k" {
		t.Fatalf("got %+v", got)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	if sig != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("signature %s", sig)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()
	if err := (WebhookSink{URL: bad.URL, Retries: 1}).Send(context.Background(), e); err == nil {
		t.Fatal("expected an error")
	}
}
