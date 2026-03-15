package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/taosun/llm-exporter/internal/config"
	"github.com/taosun/llm-exporter/internal/metrics"
	"github.com/taosun/llm-exporter/internal/prober"
)

type Scheduler struct {
	mu      sync.Mutex
	targets []config.Target
	probers []prober.Prober
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func New(targets []config.Target) (*Scheduler, error) {
	probers, err := buildProbers(targets)
	if err != nil {
		return nil, err
	}
	return &Scheduler{targets: targets, probers: probers}, nil
}

func buildProbers(targets []config.Target) ([]prober.Prober, error) {
	probers := make([]prober.Prober, len(targets))
	for i, t := range targets {
		p, err := prober.New(t)
		if err != nil {
			return nil, err
		}
		probers[i] = p
	}
	return probers, nil
}

func (s *Scheduler) Run(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	for i := range s.targets {
		s.wg.Add(1)
		go func(t config.Target, p prober.Prober) {
			defer s.wg.Done()
			s.runTarget(childCtx, t, p)
		}(s.targets[i], s.probers[i])
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
func (s *Scheduler) Reload(ctx context.Context, targets []config.Target) error {
	probers, err := buildProbers(targets)
	if err != nil {
		return err
	}

	s.Stop()

	s.mu.Lock()
	s.targets = targets
	s.probers = probers
	s.mu.Unlock()

	s.Run(ctx)
	return nil
}

func (s *Scheduler) runTarget(ctx context.Context, t config.Target, p prober.Prober) {
	labels := prometheus.Labels{
		"provider":   t.Name,
		"model":      t.Model,
		"endpoint":   t.Endpoint,
		"api_format": t.APIFormat,
	}

	s.probe(ctx, t, p, labels)

	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.probe(ctx, t, p, labels)
		}
	}
}

func (s *Scheduler) probe(ctx context.Context, t config.Target, p prober.Prober, labels prometheus.Labels) {
	probeCtx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	result, err := p.Probe(probeCtx)
	if err != nil {
		log.Printf("[%s] probe error: %v", t.Name, err)
	}

	if result == nil {
		return
	}

	if result.Success {
		metrics.ProbeSuccess.With(labels).Set(1)
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

		log.Printf("[%s] probe ok: connect=%.3fs ttft=%.2fs duration=%.2fs tokens=%d/%d rate=%.1ftok/s",
			t.Name,
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
	}
}
