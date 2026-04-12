package scheduler

import (
	"context"
	"errors"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/alerter"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/metrics"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/prober"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/status"
)

// mockProber implements prober.Prober for testing.
type mockProber struct {
	result *prober.ProbeResult
	err    error
	calls  atomic.Int32
}

func (m *mockProber) Probe(_ context.Context, _ prober.ProbeParams) (*prober.ProbeResult, error) {
	m.calls.Add(1)
	return m.result, m.err
}

// testTarget returns a minimal config.Target for testing.
func testTarget(name string) config.Target {
	return config.Target{
		Name:      name,
		Endpoint:  "https://example.com",
		Model:     "test-model",
		APIFormat: "openai",
		Prompt:    "hi",
		MaxTokens: 10,
		Timeout:   5 * time.Second,
		Interval:  100 * time.Millisecond,
	}
}

// newTestScheduler creates a Scheduler with a single mock prober for direct probe() testing.
func newTestSchedulerWithMock(mock *mockProber, target config.Target) *Scheduler {
	tracker := status.NewTracker()
	return &Scheduler{
		runners: []targetRunner{{target: target, prober: mock}},
		tracker: tracker,
		alerter: alerter.New(nil, tracker),
	}
}

// registerMetrics registers metrics on a fresh registry to avoid global conflicts.
func registerMetrics(t *testing.T) {
	t.Helper()
	metrics.Reset()
	reg := prometheus.NewRegistry()
	metrics.Register(reg)
}

func TestBuildParams_Default(t *testing.T) {
	target := config.Target{
		Prompt:    "Hello",
		MaxTokens: 20,
	}

	params := buildParams(target, 0)
	if params.Prompt != "Hello" {
		t.Errorf("Prompt = %q, want %q", params.Prompt, "Hello")
	}
	if params.MaxTokens != 20 {
		t.Errorf("MaxTokens = %d, want 20", params.MaxTokens)
	}
}

func TestBuildParams_LightMode(t *testing.T) {
	target := config.Target{
		Prompt:         "Count from 1 to 20",
		MaxTokens:      100,
		FullProbeEvery: 5,
		LightPrompt:    "Hi",
		LightMaxTokens: 5,
	}

	// count=0 => full (0 % 5 == 0)
	p0 := buildParams(target, 0)
	if p0.Prompt != "Count from 1 to 20" || p0.MaxTokens != 100 {
		t.Errorf("count=0: got Prompt=%q MaxTokens=%d, want full probe", p0.Prompt, p0.MaxTokens)
	}

	// count=1 => light
	p1 := buildParams(target, 1)
	if p1.Prompt != "Hi" || p1.MaxTokens != 5 {
		t.Errorf("count=1: got Prompt=%q MaxTokens=%d, want light probe", p1.Prompt, p1.MaxTokens)
	}

	// count=5 => full again
	p5 := buildParams(target, 5)
	if p5.Prompt != "Count from 1 to 20" || p5.MaxTokens != 100 {
		t.Errorf("count=5: got Prompt=%q MaxTokens=%d, want full probe", p5.Prompt, p5.MaxTokens)
	}
}

func TestBuildParams_PromptRotation(t *testing.T) {
	target := config.Target{
		Prompts:   []string{"A", "B", "C"},
		MaxTokens: 20,
	}

	for i := 0; i < 6; i++ {
		params := buildParams(target, i)
		want := target.Prompts[i%3]
		if params.Prompt != want {
			t.Errorf("count=%d: Prompt = %q, want %q", i, params.Prompt, want)
		}
	}
}

func TestBuildParams_PromptRotationWithLightMode(t *testing.T) {
	target := config.Target{
		Prompts:        []string{"A", "B", "C"},
		MaxTokens:      100,
		FullProbeEvery: 3,
		LightPrompt:    "light-default",
		LightMaxTokens: 5,
	}

	// count=0 => full, prompt "A"
	p := buildParams(target, 0)
	if p.Prompt != "A" || p.MaxTokens != 100 {
		t.Errorf("count=0: Prompt=%q MaxTokens=%d", p.Prompt, p.MaxTokens)
	}

	// count=1 => light, prompt "B" (from rotation)
	p = buildParams(target, 1)
	if p.Prompt != "B" || p.MaxTokens != 5 {
		t.Errorf("count=1: Prompt=%q MaxTokens=%d", p.Prompt, p.MaxTokens)
	}
}

func TestProbeType(t *testing.T) {
	target := config.Target{
		FullProbeEvery: 10,
		LightMaxTokens: 5,
	}

	full := prober.ProbeParams{MaxTokens: 20}
	light := prober.ProbeParams{MaxTokens: 5}

	if got := probeType(target, full); got != "full" {
		t.Errorf("probeType(full) = %q, want %q", got, "full")
	}
	if got := probeType(target, light); got != "light" {
		t.Errorf("probeType(light) = %q, want %q", got, "light")
	}

	// No light mode configured
	noLight := config.Target{}
	if got := probeType(noLight, full); got != "full" {
		t.Errorf("probeType(noLight, full) = %q, want %q", got, "full")
	}
}

func TestAdaptiveNext_Disabled(t *testing.T) {
	// When consecSuccess < backoff_after, interval stays at base.
	target := config.Target{
		Interval:         5 * time.Minute,
		AdaptiveInterval: true,
		MaxInterval:      20 * time.Minute,
		BackoffAfter:     5,
	}

	interval, consec := adaptiveNext(target, true, 0)
	if interval != 5*time.Minute {
		t.Errorf("interval = %v, want 5m", interval)
	}
	if consec != 1 {
		t.Errorf("consec = %d, want 1", consec)
	}
}

func TestAdaptiveNext_Backoff(t *testing.T) {
	target := config.Target{
		Interval:         5 * time.Minute,
		AdaptiveInterval: true,
		MaxInterval:      20 * time.Minute,
		BackoffAfter:     3,
	}

	// After 3 successes (threshold), next success starts doubling.
	interval, consec := adaptiveNext(target, true, 3)
	if interval != 10*time.Minute {
		t.Errorf("interval = %v, want 10m (first doubling)", interval)
	}
	if consec != 4 {
		t.Errorf("consec = %d, want 4", consec)
	}

	// Another success => 2 doublings => 20m (= max_interval).
	interval, consec = adaptiveNext(target, true, 4)
	if interval != 20*time.Minute {
		t.Errorf("interval = %v, want 20m (capped at max)", interval)
	}
	if consec != 5 {
		t.Errorf("consec = %d, want 5", consec)
	}

	// Further success => still capped at max.
	interval, consec = adaptiveNext(target, true, 10)
	if interval != 20*time.Minute {
		t.Errorf("interval = %v, want 20m (still capped)", interval)
	}
}

func TestAdaptiveNext_ResetOnFailure(t *testing.T) {
	target := config.Target{
		Interval:         5 * time.Minute,
		AdaptiveInterval: true,
		MaxInterval:      20 * time.Minute,
		BackoffAfter:     3,
	}

	// Failure resets to base interval regardless of previous consec count.
	interval, consec := adaptiveNext(target, false, 10)
	if interval != 5*time.Minute {
		t.Errorf("interval = %v, want 5m (reset on failure)", interval)
	}
	if consec != 0 {
		t.Errorf("consec = %d, want 0", consec)
	}
}

// --- probe() tests ---

func TestProbe_Success(t *testing.T) {
	registerMetrics(t)
	target := testTarget("probe-ok")
	mock := &mockProber{
		result: &prober.ProbeResult{
			Success:         true,
			Duration:        500 * time.Millisecond,
			ConnectDuration: 50 * time.Millisecond,
			TTFT:            200 * time.Millisecond,
			InputTokens:     10,
			OutputTokens:    8,
			TotalTokens:     18,
			ResponseText:    "hello",
		},
	}

	s := newTestSchedulerWithMock(mock, target)
	labels := prometheus.Labels{
		"provider": target.Name, "model": target.Model,
		"endpoint": target.Endpoint, "api_format": target.APIFormat,
	}

	ok := s.probe(context.Background(), s.runners[0], labels, prober.ProbeParams{Prompt: "hi", MaxTokens: 10})
	if !ok {
		t.Error("probe() should return true for success")
	}
	if mock.calls.Load() != 1 {
		t.Errorf("prober called %d times, want 1", mock.calls.Load())
	}

	statuses := s.GetStatuses()
	if len(statuses) != 1 {
		t.Fatalf("statuses len = %d, want 1", len(statuses))
	}
	if !statuses[0].Success {
		t.Error("expected last_success=true after successful probe")
	}
}

func TestProbe_Failure(t *testing.T) {
	registerMetrics(t)
	target := testTarget("probe-fail")
	mock := &mockProber{
		result: &prober.ProbeResult{
			Success:   false,
			Duration:  1 * time.Second,
			ErrorType: "timeout",
			Error:     errors.New("context deadline exceeded"),
		},
	}

	s := newTestSchedulerWithMock(mock, target)
	labels := prometheus.Labels{
		"provider": target.Name, "model": target.Model,
		"endpoint": target.Endpoint, "api_format": target.APIFormat,
	}

	ok := s.probe(context.Background(), s.runners[0], labels, prober.ProbeParams{Prompt: "hi", MaxTokens: 10})
	if ok {
		t.Error("probe() should return false for failure")
	}

	statuses := s.GetStatuses()
	if len(statuses) != 1 {
		t.Fatalf("statuses len = %d, want 1", len(statuses))
	}
	if statuses[0].Success {
		t.Error("expected last_success=false after failed probe")
	}
}

func TestProbe_NilResult(t *testing.T) {
	registerMetrics(t)
	target := testTarget("probe-nil")
	mock := &mockProber{
		result: nil,
		err:    errors.New("connection refused"),
	}

	s := newTestSchedulerWithMock(mock, target)
	labels := prometheus.Labels{
		"provider": target.Name, "model": target.Model,
		"endpoint": target.Endpoint, "api_format": target.APIFormat,
	}

	ok := s.probe(context.Background(), s.runners[0], labels, prober.ProbeParams{Prompt: "hi", MaxTokens: 10})
	if ok {
		t.Error("probe() should return false for nil result")
	}
}

func TestProbe_ExpectPattern_Match(t *testing.T) {
	registerMetrics(t)
	target := testTarget("pattern-match")
	target.ExpectPattern = "hello"
	mock := &mockProber{
		result: &prober.ProbeResult{
			Success:      true,
			Duration:     100 * time.Millisecond,
			TTFT:         50 * time.Millisecond,
			ResponseText: "hello world",
		},
	}

	s := newTestSchedulerWithMock(mock, target)
	s.runners[0].pattern = regexp.MustCompile(target.ExpectPattern)
	labels := prometheus.Labels{
		"provider": target.Name, "model": target.Model,
		"endpoint": target.Endpoint, "api_format": target.APIFormat,
	}

	ok := s.probe(context.Background(), s.runners[0], labels, prober.ProbeParams{Prompt: "hi", MaxTokens: 10})
	if !ok {
		t.Error("probe() should return true when pattern matches")
	}
}

func TestProbe_ExpectPattern_Mismatch(t *testing.T) {
	registerMetrics(t)
	target := testTarget("pattern-mismatch")
	target.ExpectPattern = "hello"
	mock := &mockProber{
		result: &prober.ProbeResult{
			Success:      true,
			Duration:     100 * time.Millisecond,
			TTFT:         50 * time.Millisecond,
			ResponseText: "goodbye",
		},
	}

	s := newTestSchedulerWithMock(mock, target)
	s.runners[0].pattern = regexp.MustCompile(target.ExpectPattern)
	labels := prometheus.Labels{
		"provider": target.Name, "model": target.Model,
		"endpoint": target.Endpoint, "api_format": target.APIFormat,
	}

	ok := s.probe(context.Background(), s.runners[0], labels, prober.ProbeParams{Prompt: "hi", MaxTokens: 10})
	if ok {
		t.Error("probe() should return false when pattern doesn't match")
	}
}

// --- Run/Stop/Reload tests ---

func TestRunStop(t *testing.T) {
	registerMetrics(t)
	mock := &mockProber{
		result: &prober.ProbeResult{
			Success:  true,
			Duration: 10 * time.Millisecond,
			TTFT:     5 * time.Millisecond,
		},
	}

	target := testTarget("run-stop")
	target.Interval = 50 * time.Millisecond

	s := newTestSchedulerWithMock(mock, target)

	ctx := context.Background()
	s.Run(ctx)

	// Wait enough for at least 1 probe (initial jitter is capped at min(interval, 30s))
	time.Sleep(200 * time.Millisecond)

	s.Stop()

	if mock.calls.Load() < 1 {
		t.Errorf("prober called %d times, want >= 1", mock.calls.Load())
	}
}

func TestReload(t *testing.T) {
	registerMetrics(t)
	mock1 := &mockProber{
		result: &prober.ProbeResult{
			Success:  true,
			Duration: 10 * time.Millisecond,
			TTFT:     5 * time.Millisecond,
		},
	}

	target1 := testTarget("reload-old")
	target1.Interval = 50 * time.Millisecond

	s := newTestSchedulerWithMock(mock1, target1)

	ctx := context.Background()
	s.Run(ctx)
	time.Sleep(150 * time.Millisecond)

	// Reload with a new target
	newTargets := []config.Target{testTarget("reload-new")}
	err := s.Reload(ctx, newTargets, nil)
	if err != nil {
		t.Fatalf("Reload() error: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	s.Stop()

	// Verify new target is running (GetStatuses returns it)
	statuses := s.GetStatuses()
	found := false
	for _, st := range statuses {
		if st.Name == "reload-new" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find 'reload-new' in statuses after reload")
	}
}

func TestGetStatuses_AfterProbe(t *testing.T) {
	registerMetrics(t)
	target := testTarget("get-status")
	mock := &mockProber{
		result: &prober.ProbeResult{
			Success:  true,
			Duration: 100 * time.Millisecond,
			TTFT:     50 * time.Millisecond,
		},
	}

	s := newTestSchedulerWithMock(mock, target)
	labels := prometheus.Labels{
		"provider": target.Name, "model": target.Model,
		"endpoint": target.Endpoint, "api_format": target.APIFormat,
	}

	s.probe(context.Background(), s.runners[0], labels, prober.ProbeParams{Prompt: "hi", MaxTokens: 10})

	statuses := s.GetStatuses()
	if len(statuses) == 0 {
		t.Fatal("GetStatuses() returned empty after probe")
	}
	if statuses[0].Name != "get-status" {
		t.Errorf("Name = %q, want %q", statuses[0].Name, "get-status")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short, 10) = %q, want %q", got, "short")
	}
	if got := truncate("this is a long string", 10); got != "this is a ..." {
		t.Errorf("truncate(long, 10) = %q, want %q", got, "this is a ...")
	}
}
