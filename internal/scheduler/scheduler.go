package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/taosun/llm-exporter/internal/config"
	"github.com/taosun/llm-exporter/internal/metrics"
	"github.com/taosun/llm-exporter/internal/prober"
)

// TargetStatus represents the current state of a probe target.
type TargetStatus struct {
	Name           string  `json:"name"`
	Model          string  `json:"model"`
	Endpoint       string  `json:"endpoint"`
	APIFormat      string  `json:"api_format"`
	LastProbeTime  string  `json:"last_probe_time"`
	Success        bool    `json:"last_success"`
	Error          string  `json:"last_error,omitempty"`
	Duration       float64 `json:"last_duration_seconds"`
	TTFT           float64 `json:"last_ttft_seconds"`
	ConsecFailures int     `json:"consecutive_failures"`
	TotalProbes    int64   `json:"total_probes"`
	TotalSuccesses int64   `json:"total_successes"`
}

type targetRunner struct {
	target  config.Target
	prober  prober.Prober
	pattern *regexp.Regexp // compiled expect_pattern, nil if not set
}

type Scheduler struct {
	mu      sync.Mutex
	runners []targetRunner
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	webhook *config.WebhookConfig

	statusMu sync.RWMutex
	statuses map[string]*TargetStatus
}

func New(targets []config.Target, webhook *config.WebhookConfig) (*Scheduler, error) {
	runners, err := buildRunners(targets)
	if err != nil {
		return nil, err
	}
	return &Scheduler{
		runners:  runners,
		webhook:  webhook,
		statuses: make(map[string]*TargetStatus),
	}, nil
}

func buildRunners(targets []config.Target) ([]targetRunner, error) {
	runners := make([]targetRunner, len(targets))
	for i, t := range targets {
		p, err := prober.New(t)
		if err != nil {
			return nil, err
		}
		var pattern *regexp.Regexp
		if t.ExpectPattern != "" {
			pattern = regexp.MustCompile(t.ExpectPattern)
		}
		runners[i] = targetRunner{target: t, prober: p, pattern: pattern}
	}
	return runners, nil
}

func (s *Scheduler) Run(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	for i := range s.runners {
		s.wg.Add(1)
		go func(r targetRunner) {
			defer s.wg.Done()
			s.runTarget(childCtx, r)
		}(s.runners[i])
	}
}

// Stop cancels all running goroutines and waits for them to finish.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

// Reload stops current probes, replaces targets, and restarts.
func (s *Scheduler) Reload(ctx context.Context, targets []config.Target, webhook *config.WebhookConfig) error {
	runners, err := buildRunners(targets)
	if err != nil {
		return err
	}

	s.Stop()

	// Reset all metrics to clear stale label combinations from removed targets.
	metrics.Reset()

	s.mu.Lock()
	s.runners = runners
	s.webhook = webhook
	s.mu.Unlock()

	// Clear old statuses
	s.statusMu.Lock()
	s.statuses = make(map[string]*TargetStatus)
	s.statusMu.Unlock()

	s.Run(ctx)
	return nil
}

// GetStatuses returns a snapshot of all target statuses.
func (s *Scheduler) GetStatuses() []TargetStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()

	result := make([]TargetStatus, 0, len(s.statuses))
	for _, st := range s.statuses {
		result = append(result, *st)
	}
	return result
}

func (s *Scheduler) runTarget(ctx context.Context, r targetRunner) {
	t := r.target
	labels := prometheus.Labels{
		"provider":   t.Name,
		"model":      t.Model,
		"endpoint":   t.Endpoint,
		"api_format": t.APIFormat,
	}

	// Random startup jitter to spread initial probes (capped at 30s).
	maxJitter := t.Interval
	if maxJitter > 30*time.Second {
		maxJitter = 30 * time.Second
	}
	if jitter := time.Duration(rand.Int63n(int64(maxJitter))); jitter > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter):
		}
	}

	var probeCount int
	s.probe(ctx, r, labels, buildParams(t, probeCount))
	probeCount++

	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.probe(ctx, r, labels, buildParams(t, probeCount))
			probeCount++
		}
	}
}

func buildParams(t config.Target, count int) prober.ProbeParams {
	// Prompt rotation: cycle through prompts list if configured.
	prompt := t.Prompt
	if len(t.Prompts) > 0 {
		prompt = t.Prompts[count%len(t.Prompts)]
	}

	if t.FullProbeEvery > 0 && count%t.FullProbeEvery != 0 {
		lightPrompt := t.LightPrompt
		if len(t.Prompts) > 0 {
			lightPrompt = t.Prompts[count%len(t.Prompts)]
		}
		return prober.ProbeParams{
			Prompt:    lightPrompt,
			MaxTokens: t.LightMaxTokens,
		}
	}
	return prober.ProbeParams{
		Prompt:    prompt,
		MaxTokens: t.MaxTokens,
	}
}

func probeType(t config.Target, params prober.ProbeParams) string {
	if t.FullProbeEvery > 0 && params.MaxTokens == t.LightMaxTokens {
		return "light"
	}
	return "full"
}

func (s *Scheduler) probe(ctx context.Context, r targetRunner, labels prometheus.Labels, params prober.ProbeParams) {
	t := r.target
	probeCtx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	result, err := r.prober.Probe(probeCtx, params)
	if err != nil {
		log.Printf("[%s] probe error: %v", t.Name, err)
	}

	if result == nil {
		return
	}

	// Response validation: if expect_pattern is set and probe succeeded, check the response.
	if result.Success && r.pattern != nil {
		if !r.pattern.MatchString(result.ResponseText) {
			result.Success = false
			result.ErrorType = "validation_error"
			result.Error = fmt.Errorf("response did not match expect_pattern %q", t.ExpectPattern)
			log.Printf("[%s] validation failed: response text %q", t.Name, truncate(result.ResponseText, 100))
		}
	}

	// Update target status.
	s.updateStatus(t, result)

	if result.Success {
		metrics.ProbeSuccess.With(labels).Set(1)
		metrics.ProbeLastSuccess.With(labels).SetToCurrentTime()
		metrics.ProbeDuration.With(labels).Observe(result.Duration.Seconds())
		metrics.ProbeTTFT.With(labels).Observe(result.TTFT.Seconds())
		if result.ConnectDuration > 0 {
			metrics.ProbeConnectDuration.With(labels).Observe(result.ConnectDuration.Seconds())
		}
		metrics.ProbeInputTokens.With(labels).Set(float64(result.InputTokens))
		metrics.ProbeOutputTokens.With(labels).Set(float64(result.OutputTokens))
		metrics.ProbeTotalTokens.With(labels).Set(float64(result.TotalTokens))

		// Token generation rate: output_tokens / generation_time
		// Only calculate when we have enough data for a meaningful rate
		genDuration := result.Duration - result.TTFT
		if genDuration >= prober.MinTokenRateGenDuration && result.OutputTokens >= prober.MinTokenRateOutputTokens {
			rate := float64(result.OutputTokens) / genDuration.Seconds()
			metrics.ProbeTokenRate.With(labels).Set(rate)
		}

		log.Printf("[%s] probe ok (%s): connect=%.3fs ttft=%.2fs duration=%.2fs tokens=%d/%d rate=%.1ftok/s",
			t.Name,
			probeType(t, params),
			result.ConnectDuration.Seconds(),
			result.TTFT.Seconds(),
			result.Duration.Seconds(),
			result.InputTokens,
			result.OutputTokens,
			func() float64 {
				gen := result.Duration - result.TTFT
				if gen >= prober.MinTokenRateGenDuration && result.OutputTokens >= prober.MinTokenRateOutputTokens {
					return float64(result.OutputTokens) / gen.Seconds()
				}
				return 0
			}(),
		)
	} else {
		metrics.ProbeSuccess.With(labels).Set(0)
		if result.Duration > 0 {
			metrics.ProbeDuration.With(labels).Observe(result.Duration.Seconds())
		}
		if result.ConnectDuration > 0 {
			metrics.ProbeConnectDuration.With(labels).Observe(result.ConnectDuration.Seconds())
		}
		if result.ErrorType != "" {
			errorLabels := prometheus.Labels{
				"provider":   labels["provider"],
				"model":      labels["model"],
				"endpoint":   labels["endpoint"],
				"api_format": labels["api_format"],
				"error_type": result.ErrorType,
			}
			metrics.ProbeErrors.With(errorLabels).Inc()
		}

		// Check webhook alert.
		s.checkWebhook(t.Name)
	}
}

func (s *Scheduler) updateStatus(t config.Target, result *prober.ProbeResult) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	st, ok := s.statuses[t.Name]
	if !ok {
		st = &TargetStatus{
			Name:      t.Name,
			Model:     t.Model,
			Endpoint:  t.Endpoint,
			APIFormat: t.APIFormat,
		}
		s.statuses[t.Name] = st
	}

	st.LastProbeTime = time.Now().Format(time.RFC3339)
	st.Success = result.Success
	st.Duration = result.Duration.Seconds()
	st.TTFT = result.TTFT.Seconds()
	st.TotalProbes++

	if result.Success {
		st.ConsecFailures = 0
		st.TotalSuccesses++
		st.Error = ""
	} else {
		st.ConsecFailures++
		if result.Error != nil {
			st.Error = result.Error.Error()
		}
	}
}

func (s *Scheduler) checkWebhook(targetName string) {
	s.mu.Lock()
	webhook := s.webhook
	s.mu.Unlock()

	if webhook == nil {
		return
	}

	s.statusMu.RLock()
	st, ok := s.statuses[targetName]
	if !ok {
		s.statusMu.RUnlock()
		return
	}
	consecFailures := st.ConsecFailures
	stCopy := *st
	s.statusMu.RUnlock()

	if consecFailures > 0 && consecFailures%webhook.ConsecutiveFailures == 0 {
		go sendWebhook(webhook.URL, &stCopy)
	}
}

func sendWebhook(url string, status *TargetStatus) {
	payload, _ := json.Marshal(map[string]any{
		"target":               status.Name,
		"model":                status.Model,
		"endpoint":             status.Endpoint,
		"error":                status.Error,
		"consecutive_failures": status.ConsecFailures,
		"total_probes":         status.TotalProbes,
		"timestamp":            time.Now().Format(time.RFC3339),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("[webhook] failed to create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[webhook] send failed for %s: %v", status.Name, err)
		return
	}
	resp.Body.Close()
	log.Printf("[webhook] alert sent for %s (%d consecutive failures)", status.Name, status.ConsecFailures)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
