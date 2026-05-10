package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

var testLabels = prometheus.Labels{
	"provider":   "openai",
	"model":      "gpt-4",
	"endpoint":   "https://api.openai.com",
	"api_format": "openai",
}

func TestRegister(t *testing.T) {
	reg := prometheus.NewRegistry()
	Register(reg)

	// Set values so Gather() returns them.
	ProbeSuccess.With(testLabels).Set(1)
	ProbeDuration.With(testLabels).Observe(0.5)
	ProbeConnectDuration.With(testLabels).Observe(0.1)
	ProbeTTFT.With(testLabels).Observe(0.2)
	ProbeInputTokens.With(testLabels).Set(10)
	ProbeOutputTokens.With(testLabels).Set(5)
	ProbeTotalTokens.With(testLabels).Set(15)
	ProbeReasoningTokens.With(testLabels).Set(3)
	ProbeCachedInputTokens.With(testLabels).Set(2)
	ProbeCacheCreationTokens.With(testLabels).Set(1)
	ProbeTokenRate.With(testLabels).Set(20)
	ProbeLastSuccess.With(testLabels).Set(1000)
	ProbeSSLCertExpiry.With(testLabels).Set(1234567890)
	rlLabels := prometheus.Labels{
		"provider":   "openai",
		"model":      "gpt-4",
		"endpoint":   "https://api.openai.com",
		"api_format": "openai",
		"kind":       "requests",
	}
	ProbeRateLimitRemaining.With(rlLabels).Set(100)
	errorLabels := prometheus.Labels{
		"provider":   "openai",
		"model":      "gpt-4",
		"endpoint":   "https://api.openai.com",
		"api_format": "openai",
		"error_type": "timeout",
		"status":     "0",
	}
	ProbeErrors.With(errorLabels).Inc()
	BuildInfo.With(prometheus.Labels{
		"version":    "test",
		"git_commit": "abc",
		"build_time": "now",
		"go_version": "go1.23",
	}).Set(1)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	want := map[string]bool{
		"llm_probe_success":                                    false,
		"llm_probe_duration_seconds":                           false,
		"llm_probe_connect_duration_seconds":                   false,
		"llm_probe_ttft_seconds":                               false,
		"llm_probe_input_tokens":                               false,
		"llm_probe_output_tokens":                              false,
		"llm_probe_total_tokens":                               false,
		"llm_probe_reasoning_tokens":                           false,
		"llm_probe_cached_input_tokens":                        false,
		"llm_probe_cache_creation_tokens":                      false,
		"llm_probe_token_rate":                                 false,
		"llm_probe_last_success_timestamp_seconds":             false,
		"llm_probe_rate_limit_remaining":                       false,
		"llm_probe_ssl_earliest_cert_expiry_timestamp_seconds": false,
		"llm_probe_errors_total":                               false,
		"llm_exporter_build_info":                              false,
	}

	for _, f := range families {
		if _, ok := want[f.GetName()]; ok {
			want[f.GetName()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("metric %q not registered or not gatherable", name)
		}
	}

	// Clean up for other tests
	Reset()
}

func TestReset(t *testing.T) {
	ProbeSuccess.With(testLabels).Set(1)
	ProbeInputTokens.With(testLabels).Set(10)

	Reset()

	// After reset, Gather should return no series for these metrics.
	reg := prometheus.NewRegistry()
	reg.MustRegister(ProbeSuccess)
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}
	for _, f := range families {
		if f.GetName() == "llm_probe_success" && len(f.GetMetric()) > 0 {
			t.Errorf("ProbeSuccess still has %d series after Reset()", len(f.GetMetric()))
		}
	}
}
