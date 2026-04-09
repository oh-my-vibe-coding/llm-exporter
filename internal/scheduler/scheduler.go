package scheduler

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"regexp"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/alerter"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/metrics"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/prober"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/status"
)

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

	tracker *status.Tracker
	alerter *alerter.Alerter
}

func New(targets []config.Target, webhook *config.WebhookConfig) (*Scheduler, error) {
	runners, err := buildRunners(targets)
	if err != nil {
		return nil, err
	}
	tracker := status.NewTracker()
	return &Scheduler{
		runners: runners,
		tracker: tracker,
		alerter: alerter.New(webhook, tracker),
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
	s.alerter = alerter.New(webhook, s.tracker)
	s.mu.Unlock()

	s.tracker.Reset()
	s.Run(ctx)
	return nil
}

// GetStatuses returns a snapshot of all target statuses.
func (s *Scheduler) GetStatuses() []status.TargetStatus {
	return s.tracker.GetAll()
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
	var consecSuccess int
	currentInterval := t.Interval

	success := s.probe(ctx, r, labels, buildParams(t, probeCount))
	probeCount++
	if t.AdaptiveInterval {
		newInterval, newConsec := adaptiveNext(t, success, consecSuccess)
		if newInterval != currentInterval {
			log.Printf("[%s] adaptive interval: %s -> %s (consec_success=%d)", t.Name, currentInterval, newInterval, newConsec)
		}
		currentInterval, consecSuccess = newInterval, newConsec
	}

	timer := time.NewTimer(currentInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			success = s.probe(ctx, r, labels, buildParams(t, probeCount))
			probeCount++
			if t.AdaptiveInterval {
				newInterval, newConsec := adaptiveNext(t, success, consecSuccess)
				if newInterval != currentInterval {
					log.Printf("[%s] adaptive interval: %s -> %s (consec_success=%d)", t.Name, currentInterval, newInterval, newConsec)
				}
				currentInterval, consecSuccess = newInterval, newConsec
			}
			timer.Reset(currentInterval)
		}
	}
}

// adaptiveNext computes the next interval based on probe result.
// On failure, resets to the base interval. On success, doubles the interval
// after BackoffAfter consecutive successes, capped at MaxInterval.
func adaptiveNext(t config.Target, success bool, consecSuccess int) (time.Duration, int) {
	if !success {
		return t.Interval, 0
	}
	consecSuccess++
	if consecSuccess <= t.BackoffAfter {
		return t.Interval, consecSuccess
	}
	// Double for each success beyond the threshold.
	doublings := consecSuccess - t.BackoffAfter
	interval := t.Interval
	for i := 0; i < doublings; i++ {
		interval *= 2
		if interval >= t.MaxInterval {
			return t.MaxInterval, consecSuccess
		}
	}
	return interval, consecSuccess
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

func (s *Scheduler) probe(ctx context.Context, r targetRunner, labels prometheus.Labels, params prober.ProbeParams) bool {
	t := r.target
	probeCtx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	result, err := r.prober.Probe(probeCtx, params)
	if err != nil {
		log.Printf("[%s] probe error: %v", t.Name, err)
	}

	if result == nil {
		return false
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
	s.tracker.Update(t.Name, t.Model, t.Endpoint, t.APIFormat, status.ProbeOutcome{
		Success:  result.Success,
		Duration: result.Duration,
		TTFT:     result.TTFT,
		Error:    result.Error,
	})

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
		s.alerter.Check(t.Name)
	}
	return result.Success
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
