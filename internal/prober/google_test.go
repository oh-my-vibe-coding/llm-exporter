package prober

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
)

func TestGoogleProbe_Success(t *testing.T) {
	sseBody := `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}

data: {"candidates":[{"content":{"parts":[{"text":" world"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":4,"totalTokenCount":12}}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := NewGoogle(config.Target{
		Endpoint: srv.URL,
		APIKey:   "test-key",
		Model:    "gemini-pro",
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
	if result.InputTokens != 8 {
		t.Errorf("InputTokens = %d, want 8", result.InputTokens)
	}
	if result.OutputTokens != 4 {
		t.Errorf("OutputTokens = %d, want 4", result.OutputTokens)
	}
	if result.TotalTokens != 12 {
		t.Errorf("TotalTokens = %d, want 12", result.TotalTokens)
	}
	if result.TTFT == 0 {
		t.Error("TTFT should be > 0")
	}
}

func TestGoogleProbe_HTTPError(t *testing.T) {
	tests := []struct {
		status    int
		wantError string
	}{
		{http.StatusUnauthorized, "auth"},
		{http.StatusTooManyRequests, "rate_limit"},
		{http.StatusInternalServerError, "api_error"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("HTTP_%d", tt.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "error", tt.status)
			}))
			defer srv.Close()

			p := NewGoogle(config.Target{
				Endpoint: srv.URL,
				Model:    "gemini-pro",
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

func TestGoogleProbe_NoContent(t *testing.T) {
	sseBody := `data: {"candidates":[{"content":{"parts":[]}}]}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := NewGoogle(config.Target{
		Endpoint: srv.URL,
		Model:    "gemini-pro",
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

func TestGoogleProbe_URLFormat(t *testing.T) {
	var gotURL string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {}\n\n")
	}))
	defer srv.Close()

	p := NewGoogle(config.Target{
		Endpoint: srv.URL,
		APIKey:   "my-api-key",
		Model:    "gemini-1.5-pro",
		Timeout:  5 * time.Second,
	})

	p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 30})

	if !strings.Contains(gotURL, "/v1beta/models/gemini-1.5-pro:streamGenerateContent") {
		t.Errorf("URL missing model path: %s", gotURL)
	}
	if !strings.Contains(gotURL, "alt=sse") {
		t.Errorf("URL missing alt=sse: %s", gotURL)
	}
	if !strings.Contains(gotURL, "key=my-api-key") {
		t.Errorf("URL missing API key: %s", gotURL)
	}
}
