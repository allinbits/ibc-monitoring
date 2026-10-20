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
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/store"
)

type recorder struct {
	mu     sync.Mutex
	events []model.AlarmEvent
	delay  time.Duration
}

func (*recorder) Name() string { return "rec" }
func (r *recorder) Send(_ context.Context, e model.AlarmEvent) error {
	time.Sleep(r.delay)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recorder) list() []model.AlarmEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.AlarmEvent, len(r.events))
	copy(out, r.events)
	return out
}

func (r *recorder) wait(t *testing.T, n int) []model.AlarmEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ev := r.list(); len(ev) >= n {
			return ev
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d events, got %d", n, len(r.list()))
	return nil
}

func result(findings ...model.Finding) model.CheckResult {
	return model.CheckResult{Check: "c", Status: model.CheckAlarm, Findings: findings}
}

func TestManagerTransitions(t *testing.T) {
	st := store.NewMemory(10)
	rec := &recorder{}
	m := NewManager(st, nil, rec)
	t.Cleanup(func() { m.Close(time.Second) })
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	f := model.Finding{Subject: "x", Severity: model.SeverityWarning, Title: "x warns", AlarmKey: "c/x"}
	m.Apply(context.Background(), result(f))
	ev := rec.wait(t, 1)
	if ev[0].Type != "raised" {
		t.Fatalf("events %+v", ev)
	}
	// Same finding again: no transition.
	now = now.Add(time.Minute)
	m.Apply(context.Background(), result(f))
	time.Sleep(20 * time.Millisecond)
	if len(rec.list()) != 1 {
		t.Fatalf("duplicate: %+v", rec.list())
	}
	a, _ := st.Alarm("c/x")
	if !a.LastSeen.Equal(now) || !a.FirstSeen.Equal(time.Unix(1000, 0)) {
		t.Fatalf("times %+v", a)
	}
	// Severity change: updated.
	f.Severity, f.Title = model.SeverityCritical, "x is critical"
	m.Apply(context.Background(), result(f))
	ev = rec.wait(t, 2)
	if ev[1].Type != "updated" || ev[1].Alarm.Severity != model.SeverityCritical {
		t.Fatalf("events %+v", ev)
	}
	// A skipped or errored result keeps the alarm.
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckSkipped})
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckError})
	time.Sleep(20 * time.Millisecond)
	if len(rec.list()) != 2 {
		t.Fatalf("skip cleared: %+v", rec.list())
	}
	// The finding goes away: cleared.
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckOK})
	ev = rec.wait(t, 3)
	if ev[2].Type != "cleared" {
		t.Fatalf("events %+v", ev)
	}
	if len(st.Alarms(false)) != 0 || len(st.Alarms(true)) != 1 {
		t.Fatal("cleared alarm listing")
	}
	// Raised again after a clear: a fresh alarm.
	m.Apply(context.Background(), result(f))
	ev = rec.wait(t, 4)
	if ev[3].Type != "raised" {
		t.Fatalf("events %+v", ev)
	}
	// Another check's alarms are untouched by this check's results.
	st.PutAlarm(model.Alarm{Key: "d/y", Check: "d", State: model.AlarmRaised})
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckOK})
	if a, _ := st.Alarm("d/y"); a.State != model.AlarmRaised {
		t.Fatal("foreign alarm cleared")
	}
}

// The auto-flag eligibility lives on the event: only raised/updated break
// alarms carry it, never a clear (docs/design.md §8.1, review finding 10).
func TestAutoFlagEligibilityOnEvents(t *testing.T) {
	st := store.NewMemory(10)
	rec := &recorder{}
	m := NewManager(st, nil, rec)
	t.Cleanup(func() { m.Close(time.Second) })

	brk := model.Finding{Subject: "r", Severity: model.SeverityBreak, Title: "break", AlarmKey: "orphan/r", AutoFlag: true}
	m.Apply(context.Background(), result(brk))
	ev := rec.wait(t, 1)
	if !ev[0].AutoFlag {
		t.Fatal("a raised break must be eligible")
	}
	// A critical (not break) finding that claims eligibility is not.
	crit := model.Finding{Subject: "s", Severity: model.SeverityCritical, Title: "crit", AlarmKey: "x/s", AutoFlag: true}
	m.Apply(context.Background(), result(brk, crit))
	ev = rec.wait(t, 2)
	if ev[1].Alarm.Key != "x/s" || ev[1].AutoFlag {
		t.Fatalf("critical must not be eligible: %+v", ev[1])
	}
	// Clearing both: neither clear event is eligible although the alarm's
	// own flag is still set.
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckOK})
	ev = rec.wait(t, 4)
	for _, e := range ev[2:] {
		if e.Type != "cleared" || e.AutoFlag {
			t.Fatalf("cleared event eligible: %+v", e)
		}
		if e.Alarm.Key == "orphan/r" && !e.Alarm.AutoFlag {
			t.Fatal("the alarm's own flag is historical and stays")
		}
	}
	for _, e := range st.AlarmEvents(10) {
		if e.Type == "cleared" && e.AutoFlag {
			t.Fatal("stored cleared event eligible")
		}
	}
}

// A slow sink must not block Apply, and a full queue drops the oldest.
func TestAsyncSinks(t *testing.T) {
	st := store.NewMemory(10)
	slow := &recorder{delay: 200 * time.Millisecond}
	fast := &recorder{}
	m := NewManager(st, nil, slow, fast)
	t.Cleanup(func() { m.Close(time.Second) })
	start := time.Now()
	var findings []model.Finding
	for i := 0; i < DefaultQueue+20; i++ {
		findings = append(findings, model.Finding{Subject: "x", Severity: model.SeverityWarning, Title: "t", AlarmKey: "c/" + strconv.Itoa(i)})
	}
	m.Apply(context.Background(), result(findings...))
	if el := time.Since(start); el > time.Second {
		t.Fatalf("Apply blocked on the slow sink: %s", el)
	}
	// The fast sink gets everything; the slow one dropped some.
	fast.wait(t, DefaultQueue+20)
	if d := m.Dropped(); d[0] == 0 || d[1] != 0 {
		t.Fatalf("dropped %v", d)
	}
	// Every alarm is in the store whatever the sinks did.
	if n := len(st.Alarms(false)); n != DefaultQueue+20 {
		t.Fatalf("alarms stored %d", n)
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

// Subjects a check could not evaluate keep their alarms (review finding 9).
func TestUnevaluatedKeepsAlarms(t *testing.T) {
	st := store.NewMemory(10)
	rec := &recorder{}
	m := NewManager(st, nil, rec)
	t.Cleanup(func() { m.Close(time.Second) })
	a := model.Finding{Subject: "a", Severity: model.SeverityCritical, Title: "a expired", AlarmKey: "c/a"}
	b := model.Finding{Subject: "b", Severity: model.SeverityWarning, Title: "b warns", AlarmKey: "c/b"}
	m.Apply(context.Background(), result(a, b))
	rec.wait(t, 2)
	// Next run: a's chain is stale, b is fine now → b clears, a stays.
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckOK, Findings: []model.Finding{}, Unevaluated: []string{"c/a"}})
	ev := rec.wait(t, 3)
	if ev[2].Type != "cleared" || ev[2].Alarm.Key != "c/b" {
		t.Fatalf("events %+v", ev)
	}
	if al, ok := st.Alarm("c/a"); !ok || al.State != model.AlarmRaised {
		t.Fatalf("unevaluated alarm cleared: %+v", al)
	}
	// Once a is evaluated again and clean, it clears.
	m.Apply(context.Background(), model.CheckResult{Check: "c", Status: model.CheckOK, Findings: []model.Finding{}})
	ev = rec.wait(t, 4)
	if ev[3].Type != "cleared" || ev[3].Alarm.Key != "c/a" {
		t.Fatalf("events %+v", ev)
	}
}

// gated refuses or blocks deliveries until opened.
type gated struct {
	recorder
	mu   sync.Mutex
	fail bool
}

func (g *gated) Name() string { return "gated" }
func (g *gated) Send(ctx context.Context, e model.AlarmEvent) error {
	g.mu.Lock()
	fail := g.fail
	g.mu.Unlock()
	if fail {
		return context.DeadlineExceeded
	}
	return g.recorder.Send(ctx, e)
}

func (g *gated) set(fail bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fail = fail
}

func keysOf(events []model.AlarmEvent) map[string]int {
	out := map[string]int{}
	for _, e := range events {
		out[e.Alarm.Key]++
	}
	return out
}

// A burst past the queue's capacity, against a sink that refuses every
// delivery, drops and fails notifications but loses none: the pending set
// raises sink_backlog, reconciliation re-sends the latest state of every
// alarm once the sink recovers, and the backlog clears (review finding 8).
func TestBacklogReconciliation(t *testing.T) {
	st := store.NewMemory(10)
	g := &gated{}
	g.set(true)
	m := NewManagerWith(st, nil, Options{Queue: 2, FailingAfter: 50 * time.Millisecond}, g)
	t.Cleanup(func() { m.Close(time.Second) })
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	var findings []model.Finding
	for i := 0; i < 20; i++ {
		findings = append(findings, model.Finding{Subject: strconv.Itoa(i), Severity: model.SeverityWarning, Title: "w", AlarmKey: "c/" + strconv.Itoa(i)})
	}
	m.Apply(context.Background(), result(findings...))
	time.Sleep(50 * time.Millisecond)
	// Nothing delivered; drops or failures account for every event.
	if len(g.list()) != 0 {
		t.Fatalf("delivered while refusing: %d", len(g.list()))
	}
	now = now.Add(time.Minute)
	m.Reconcile(context.Background())
	pending := m.Pending()[0]
	if len(pending) == 0 || m.Dropped()[0] == 0 {
		t.Fatalf("pending %d dropped %d", len(pending), m.Dropped()[0])
	}
	a, ok := st.Alarm("sink_backlog/gated")
	if !ok || a.State != model.AlarmRaised {
		t.Fatalf("no backlog alarm: %+v", a)
	}
	if f, ok := st.Alarm("sink_failing/gated"); !ok || f.State != model.AlarmRaised {
		t.Fatalf("no failing alarm: %+v", f)
	}
	// The sink recovers: reconciliation delivers every alarm's latest state
	// with no new transition, and the backlog drains.
	g.set(false)
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.Reconcile(context.Background())
		time.Sleep(20 * time.Millisecond)
		if len(m.Pending()[0]) == 0 || time.Now().After(deadline) {
			break
		}
	}
	if len(m.Pending()[0]) != 0 {
		t.Fatalf("pending after recovery: %v", m.Pending()[0])
	}
	got := keysOf(g.list())
	for i := 0; i < 20; i++ {
		if got["c/"+strconv.Itoa(i)] == 0 {
			t.Fatalf("alarm c/%d never delivered: %v", i, got)
		}
	}
	m.Reconcile(context.Background())
	if a, _ := st.Alarm("sink_backlog/gated"); a.State != model.AlarmCleared {
		t.Fatalf("backlog not cleared: %+v", a)
	}
	if f, _ := st.Alarm("sink_failing/gated"); f.State != model.AlarmCleared {
		t.Fatalf("failing not cleared: %+v", f)
	}
}
