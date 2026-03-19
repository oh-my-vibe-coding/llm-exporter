package prober

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/taosun/llm-exporter/internal/config"
)

// ProbeResult holds the outcome of a single probe.
type ProbeResult struct {
	Success         bool
	Duration        time.Duration
	ConnectDuration time.Duration
	TTFT            time.Duration
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	ResponseText    string
	ErrorType       string
	Error           error
}

// ProbeParams holds per-probe parameters that may vary between light and full probes.
type ProbeParams struct {
	Prompt    string
	MaxTokens int
}

// Prober executes a streaming probe against an LLM endpoint.
type Prober interface {
	Probe(ctx context.Context, params ProbeParams) (*ProbeResult, error)
}

// New creates a Prober for the given target configuration.
func New(t config.Target) (Prober, error) {
	switch t.APIFormat {
	case "openai":
		return NewOpenAI(t), nil
	case "anthropic":
		return NewAnthropic(t), nil
	case "google":
		return NewGoogle(t), nil
	case "azure":
		return NewAzure(t), nil
	default:
		return nil, fmt.Errorf("unsupported api_format: %s", t.APIFormat)
	}
}

// newProbeClient creates an http.Client that does NOT reuse connections,
// so every probe measures real DNS + TCP + TLS time.
func newProbeClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: (&net.Dialer{
				Timeout: 10 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}

// probePrompt returns the configured prompt with a timestamp suffix
// to prevent response caching by proxies or providers.
func probePrompt(base string) string {
	return fmt.Sprintf("%s [t=%d]", base, time.Now().UnixMilli())
}

// MinTokenRateOutputTokens is the minimum output tokens required to calculate a meaningful token rate.
const MinTokenRateOutputTokens = 5

// MinTokenRateGenDuration is the minimum generation duration to calculate a meaningful token rate.
const MinTokenRateGenDuration = 100 * time.Millisecond

// connectTracer creates an httptrace.ClientTrace that measures DNS+TCP+TLS time.
type connectTimings struct {
	dnsStart     time.Time
	connectStart time.Time
	connectDone  time.Time
	tlsDone      time.Time
	gotConn      time.Time
	reused       bool
}

func newConnectTrace(ctx context.Context) (context.Context, *connectTimings) {
	t := &connectTimings{}
	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			t.dnsStart = time.Now()
		},
		ConnectStart: func(_, _ string) {
			if t.connectStart.IsZero() {
				t.connectStart = time.Now()
			}
		},
		ConnectDone: func(_, _ string, _ error) {
			t.connectDone = time.Now()
		},
		TLSHandshakeStart: func() {},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			t.tlsDone = time.Now()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			t.gotConn = time.Now()
			t.reused = info.Reused
		},
	}
	return httptrace.WithClientTrace(ctx, trace), t
}

func (t *connectTimings) duration() time.Duration {
	if t.reused {
		return 0
	}
	begin := t.dnsStart
	if begin.IsZero() {
		begin = t.connectStart
	}
	if begin.IsZero() {
		return 0
	}
	end := t.tlsDone
	if end.IsZero() {
		end = t.connectDone
	}
	if end.IsZero() {
		end = t.gotConn
	}
	if end.IsZero() {
		return 0
	}
	return end.Sub(begin)
}
