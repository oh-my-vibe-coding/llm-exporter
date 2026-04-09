package alerter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/status"
)

func TestAlerter_NilSafe(t *testing.T) {
	var a *Alerter
	// Must not panic.
	a.Check("anything")
}

func TestAlerter_NilWebhook(t *testing.T) {
	tr := status.NewTracker()
	a := New(nil, tr)
	if a != nil {
		t.Error("New(nil, ...) should return nil")
	}
	// Must not panic.
	a.Check("anything")
}

func TestAlerter_NoAlertBelowThreshold(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := status.NewTracker()
	a := New(&config.WebhookConfig{URL: srv.URL, ConsecutiveFailures: 3}, tr)

	// 2 failures — below threshold of 3.
	for i := 0; i < 2; i++ {
		tr.Update("t", "m", "e", "openai", status.ProbeOutcome{Error: errFail})
		a.Check("t")
	}

	time.Sleep(50 * time.Millisecond)
	if c := called.Load(); c != 0 {
		t.Errorf("webhook called %d times, want 0", c)
	}
}

func TestAlerter_FiresAtThreshold(t *testing.T) {
	var called atomic.Int32
	var lastPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		json.NewDecoder(r.Body).Decode(&lastPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := status.NewTracker()
	a := New(&config.WebhookConfig{URL: srv.URL, ConsecutiveFailures: 3}, tr)

	// 3 failures — exactly at threshold.
	for i := 0; i < 3; i++ {
		tr.Update("t", "m", "e", "openai", status.ProbeOutcome{Error: errFail})
		a.Check("t")
	}

	time.Sleep(100 * time.Millisecond)
	if c := called.Load(); c != 1 {
		t.Errorf("webhook called %d times, want 1", c)
	}
	if lastPayload["target"] != "t" {
		t.Errorf("payload target = %v, want %q", lastPayload["target"], "t")
	}
}

func TestAlerter_FiresAtMultiple(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := status.NewTracker()
	a := New(&config.WebhookConfig{URL: srv.URL, ConsecutiveFailures: 2}, tr)

	// 4 failures — fires at 2 and 4.
	for i := 0; i < 4; i++ {
		tr.Update("t", "m", "e", "openai", status.ProbeOutcome{Error: errFail})
		a.Check("t")
	}

	time.Sleep(100 * time.Millisecond)
	if c := called.Load(); c != 2 {
		t.Errorf("webhook called %d times, want 2", c)
	}
}

var errFail = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test failure" }
