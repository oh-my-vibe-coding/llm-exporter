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

func boolPtr(b bool) *bool { return &b }

func TestOpenAICompatProbe_Streaming_Success(t *testing.T) {
	sseBody := `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: {"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := newOpenAICompat(openaiCompatConfig{
		target: config.Target{
			Endpoint: srv.URL,
			APIKey:   "sk-test",
			Model:    "gpt-4",
			Timeout:  5 * time.Second,
		},
		buildURL: func(t config.Target) string { return t.Endpoint },
		setAuth:  openaiAuth,
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
	if result.InputTokens != 5 {
		t.Errorf("InputTokens = %d, want 5", result.InputTokens)
	}
	if result.OutputTokens != 3 {
		t.Errorf("OutputTokens = %d, want 3", result.OutputTokens)
	}
	if result.TotalTokens != 8 {
		t.Errorf("TotalTokens = %d, want 8", result.TotalTokens)
	}
	if result.TTFT == 0 {
		t.Error("TTFT should be > 0")
	}
}

func TestOpenAICompatProbe_Streaming_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newOpenAICompat(openaiCompatConfig{
		target: config.Target{
			Endpoint: srv.URL,
			Model:    "gpt-4",
			Timeout:  5 * time.Second,
		},
		buildURL: func(t config.Target) string { return t.Endpoint },
		setAuth:  openaiAuth,
	})

	result, _ := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
	if result.Success {
		t.Error("expected Success=false")
	}
	if result.ErrorType != "api_error" {
		t.Errorf("ErrorType = %q, want %q", result.ErrorType, "api_error")
	}
}

func TestOpenAICompatProbe_Streaming_NoContent(t *testing.T) {
	sseBody := "data: {\"choices\":[]}\n\ndata: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := newOpenAICompat(openaiCompatConfig{
		target: config.Target{
			Endpoint: srv.URL,
			Model:    "gpt-4",
			Timeout:  5 * time.Second,
		},
		buildURL: func(t config.Target) string { return t.Endpoint },
		setAuth:  openaiAuth,
	})

	result, _ := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
	if result.Success {
		t.Error("expected Success=false")
	}
	if result.ErrorType != "parse_error" {
		t.Errorf("ErrorType = %q, want %q", result.ErrorType, "parse_error")
	}
}

func TestOpenAICompatProbe_NonStreaming_Success(t *testing.T) {
	respBody := `{"choices":[{"message":{"content":"Hello world"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respBody)
	}))
	defer srv.Close()

	stream := false
	p := newOpenAICompat(openaiCompatConfig{
		target: config.Target{
			Endpoint: srv.URL,
			Model:    "gpt-4",
			Timeout:  5 * time.Second,
			Stream:   &stream,
		},
		buildURL: func(t config.Target) string { return t.Endpoint },
		setAuth:  openaiAuth,
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
	if result.InputTokens != 5 {
		t.Errorf("InputTokens = %d, want 5", result.InputTokens)
	}
	if result.OutputTokens != 3 {
		t.Errorf("OutputTokens = %d, want 3", result.OutputTokens)
	}
	if result.TTFT != result.Duration {
		t.Errorf("non-streaming TTFT (%v) should equal Duration (%v)", result.TTFT, result.Duration)
	}
}

func TestOpenAICompatProbe_NonStreaming_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	stream := false
	p := newOpenAICompat(openaiCompatConfig{
		target: config.Target{
			Endpoint: srv.URL,
			Model:    "gpt-4",
			Timeout:  5 * time.Second,
			Stream:   &stream,
		},
		buildURL: func(t config.Target) string { return t.Endpoint },
		setAuth:  openaiAuth,
	})

	result, _ := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
	if result.Success {
		t.Error("expected Success=false")
	}
	if result.ErrorType != "auth" {
		t.Errorf("ErrorType = %q, want %q", result.ErrorType, "auth")
	}
}

func TestOpenAICompatProbe_NonStreaming_NoContent(t *testing.T) {
	respBody := `{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respBody)
	}))
	defer srv.Close()

	stream := false
	p := newOpenAICompat(openaiCompatConfig{
		target: config.Target{
			Endpoint: srv.URL,
			Model:    "gpt-4",
			Timeout:  5 * time.Second,
			Stream:   &stream,
		},
		buildURL: func(t config.Target) string { return t.Endpoint },
		setAuth:  openaiAuth,
	})

	result, _ := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
	if result.Success {
		t.Error("expected Success=false")
	}
	if result.ErrorType != "parse_error" {
		t.Errorf("ErrorType = %q, want %q", result.ErrorType, "parse_error")
	}
}

func TestOpenAICompatProbe_Headers(t *testing.T) {
	var gotHeaders http.Header
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := newOpenAICompat(openaiCompatConfig{
		target: config.Target{
			Endpoint: srv.URL,
			APIKey:   "sk-test-key",
			Model:    "gpt-4",
			Timeout:  5 * time.Second,
			ExtraHeaders: map[string]string{
				"X-Custom": "custom-val",
			},
		},
		buildURL: func(t config.Target) string { return t.Endpoint },
		setAuth:  openaiAuth,
	})

	p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 50})

	if got := gotHeaders.Get("Authorization"); got != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk-test-key")
	}
	if got := gotHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := gotHeaders.Get("X-Custom"); got != "custom-val" {
		t.Errorf("X-Custom = %q, want %q", got, "custom-val")
	}
	if model, ok := gotBody["model"].(string); !ok || model != "gpt-4" {
		t.Errorf("body model = %v, want %q", gotBody["model"], "gpt-4")
	}
	if stream, ok := gotBody["stream"].(bool); !ok || !stream {
		t.Errorf("body stream = %v, want true", gotBody["stream"])
	}
}
