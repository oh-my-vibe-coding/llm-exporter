package prober

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
)

func TestAnthropicProbe_Success(t *testing.T) {
	sseBody := `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":10}}}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" world"}}

event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := NewAnthropic(config.Target{
		Endpoint: srv.URL,
		APIKey:   "test-key",
		Model:    "claude-3",
		Timeout:  5 * time.Second,
	})

	result, err := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.ResponseText != "Hello world" {
		t.Errorf("ResponseText = %q, want %q", result.ResponseText, "Hello world")
	}
	if result.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", result.InputTokens)
	}
	if result.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", result.OutputTokens)
	}
	if result.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", result.TotalTokens)
	}
	if result.TTFT == 0 {
		t.Error("TTFT should be > 0")
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}
}

func TestAnthropicProbe_HTTPError(t *testing.T) {
	tests := []struct {
		status    int
		wantError string
	}{
		{http.StatusUnauthorized, "auth"},
		{http.StatusForbidden, "auth"},
		{http.StatusTooManyRequests, "rate_limit"},
		{http.StatusInternalServerError, "api_error"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("HTTP_%d", tt.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "error", tt.status)
			}))
			defer srv.Close()

			p := NewAnthropic(config.Target{
				Endpoint: srv.URL,
				Model:    "claude-3",
				Timeout:  5 * time.Second,
			})

			result, _ := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
			if result.Success {
				t.Error("expected Success=false")
			}
			if result.ErrorType != tt.wantError {
				t.Errorf("ErrorType = %q, want %q", result.ErrorType, tt.wantError)
			}
		})
	}
}

func TestAnthropicProbe_NoContent(t *testing.T) {
	sseBody := `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":10}}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := NewAnthropic(config.Target{
		Endpoint: srv.URL,
		Model:    "claude-3",
		Timeout:  5 * time.Second,
	})

	result, _ := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
	if result.Success {
		t.Error("expected Success=false")
	}
	if result.ErrorType != "parse_error" {
		t.Errorf("ErrorType = %q, want %q", result.ErrorType, "parse_error")
	}
}

func TestAnthropicProbe_Headers(t *testing.T) {
	var gotHeaders http.Header
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_stop\ndata: {}\n\n")
	}))
	defer srv.Close()

	p := NewAnthropic(config.Target{
		Endpoint: srv.URL,
		APIKey:   "sk-ant-test",
		Model:    "claude-3-sonnet",
		Timeout:  5 * time.Second,
		ExtraHeaders: map[string]string{
			"X-Custom": "value",
		},
	})

	p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 50})

	if got := gotHeaders.Get("x-api-key"); got != "sk-ant-test" {
		t.Errorf("x-api-key = %q, want %q", got, "sk-ant-test")
	}
	if got := gotHeaders.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", got, "2023-06-01")
	}
	if got := gotHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := gotHeaders.Get("X-Custom"); got != "value" {
		t.Errorf("X-Custom = %q, want %q", got, "value")
	}
	if model, ok := gotBody["model"].(string); !ok || model != "claude-3-sonnet" {
		t.Errorf("body model = %v, want %q", gotBody["model"], "claude-3-sonnet")
	}
	if stream, ok := gotBody["stream"].(bool); !ok || !stream {
		t.Errorf("body stream = %v, want true", gotBody["stream"])
	}
}
