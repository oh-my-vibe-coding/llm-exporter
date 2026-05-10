package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/prober"
)

// probeHandler implements the blackbox_exporter-style multi-target probe
// endpoint. It expects ?target=<endpoint-url>&module=<module-name>, runs a
// single one-shot probe using the pre-declared module, and writes Prometheus
// metrics for that one probe to the response.
//
// Metrics exposed per scrape:
//
//	probe_success
//	probe_duration_seconds
//	probe_ttft_seconds
//	probe_connect_duration_seconds
//	probe_input_tokens / probe_output_tokens / probe_total_tokens
//	probe_reasoning_tokens / probe_cached_input_tokens / probe_cache_creation_tokens
//	probe_http_status_code
//	probe_rate_limit_remaining{kind="requests|tokens"}
//	probe_ssl_earliest_cert_expiry_timestamp_seconds
//	probe_error_type{type="..."}  (present iff probe failed)
//
// The naming mirrors blackbox_exporter so users can apply the same alerting
// patterns they already have.
func probeHandler(cfgGetter func() *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := cfgGetter()
		target := r.URL.Query().Get("target")
		moduleName := r.URL.Query().Get("module")
		if target == "" {
			http.Error(w, "missing 'target' query parameter", http.StatusBadRequest)
			return
		}
		if moduleName == "" {
			http.Error(w, "missing 'module' query parameter", http.StatusBadRequest)
			return
		}
		mod, ok := cfg.Modules[moduleName]
		if !ok {
			http.Error(w, fmt.Sprintf("unknown module %q (defined modules: %d)", moduleName, len(cfg.Modules)), http.StatusBadRequest)
			return
		}

		runtimeTarget := mod.ToTarget(moduleName, target)
		p, err := prober.New(runtimeTarget)
		if err != nil {
			http.Error(w, fmt.Sprintf("build prober: %v", err), http.StatusBadRequest)
			return
		}

		reg := prometheus.NewRegistry()
		result, _ := p.Probe(r.Context(), prober.ProbeParams{
			Prompt:    runtimeTarget.Prompt,
			MaxTokens: runtimeTarget.MaxTokens,
		})
		if result == nil {
			result = &prober.ProbeResult{ErrorType: "unknown"}
		}
		registerProbeResult(reg, result)

		if result.Error != nil {
			log.Printf("[probe module=%s target=%s] error: %v", moduleName, target, result.Error)
		}

		promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	}
}

// registerProbeResult mirrors a single ProbeResult onto a fresh registry,
// using blackbox-style metric names. Each scrape gets its own registry, so
// label cardinality never accumulates.
func registerProbeResult(reg *prometheus.Registry, result *prober.ProbeResult) {
	newGauge := func(name, help string) prometheus.Gauge {
		g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help})
		reg.MustRegister(g)
		return g
	}

	success := 0.0
	if result.Success {
		success = 1.0
	}
	newGauge("probe_success", "Displays whether or not the probe was a success.").Set(success)

	newGauge("probe_duration_seconds", "Total request duration in seconds.").Set(result.Duration.Seconds())
	newGauge("probe_ttft_seconds", "Time to first token in seconds.").Set(result.TTFT.Seconds())
	newGauge("probe_connect_duration_seconds", "Connection setup duration (DNS + TCP + TLS) in seconds.").Set(result.ConnectDuration.Seconds())
	newGauge("probe_input_tokens", "Input (prompt) tokens consumed.").Set(float64(result.InputTokens))
	newGauge("probe_output_tokens", "Output (completion) tokens generated.").Set(float64(result.OutputTokens))
	newGauge("probe_total_tokens", "Total tokens consumed.").Set(float64(result.TotalTokens))
	newGauge("probe_reasoning_tokens", "Reasoning/thinking tokens in the probe.").Set(float64(result.ReasoningTokens))
	newGauge("probe_cached_input_tokens", "Prompt tokens served from provider-side prompt cache.").Set(float64(result.CachedInputTokens))
	newGauge("probe_cache_creation_tokens", "Tokens written into the provider prompt cache.").Set(float64(result.CacheCreationTokens))
	newGauge("probe_http_status_code", "HTTP status code of the probe response (0 if no response).").Set(float64(result.HTTPStatusCode))

	if result.RateLimitRemainingRequests >= 0 || result.RateLimitRemainingTokens >= 0 {
		rl := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "probe_rate_limit_remaining",
			Help: "Remaining rate-limit budget reported by the provider.",
		}, []string{"kind"})
		reg.MustRegister(rl)
		if result.RateLimitRemainingRequests >= 0 {
			rl.WithLabelValues("requests").Set(float64(result.RateLimitRemainingRequests))
		}
		if result.RateLimitRemainingTokens >= 0 {
			rl.WithLabelValues("tokens").Set(float64(result.RateLimitRemainingTokens))
		}
	}

	if !result.SSLCertNotAfter.IsZero() {
		newGauge("probe_ssl_earliest_cert_expiry_timestamp_seconds", "Unix timestamp of the earliest TLS peer certificate NotAfter.").Set(float64(result.SSLCertNotAfter.Unix()))
	}

	if !result.Success && result.ErrorType != "" {
		errType := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "probe_error_type",
			Help: "Set to 1 with a 'type' label describing the error classification (only present on failure).",
		}, []string{"type"})
		reg.MustRegister(errType)
		errType.WithLabelValues(result.ErrorType).Set(1)
	}
}
