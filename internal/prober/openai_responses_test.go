package prober

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
)

func TestResponsesProbe_Streaming_Success(t *testing.T) {
	sseBody := `event: response.created
data: {"response":{"id":"resp_1"}}

event: response.output_text.delta
data: {"delta":"Hello"}

event: response.output_text.delta
data: {"delta":" world"}

event: response.completed
data: {"response":{"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":5}}}}

`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := NewOpenAIResponses(config.Target{
		Endpoint: srv.URL,
		APIKey:   "sk-test",
		Model:    "gpt-5.5",
		Timeout:  5 * time.Second,
	})

	result, err := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected Success")
	}
	if result.ResponseText != "Hello world" {
		t.Errorf("ResponseText = %q, want %q", result.ResponseText, "Hello world")
	}
	if result.InputTokens != 10 || result.OutputTokens != 3 || result.TotalTokens != 13 {
		t.Errorf("tokens = %d/%d/%d, want 10/3/13", result.InputTokens, result.OutputTokens, result.TotalTokens)
	}
	if result.CachedInputTokens != 2 {
		t.Errorf("CachedInputTokens = %d, want 2", result.CachedInputTokens)
	}
	if result.ReasoningTokens != 5 {
		t.Errorf("ReasoningTokens = %d, want 5", result.ReasoningTokens)
	}
}

func TestResponsesProbe_NonStreaming_Success(t *testing.T) {
	body := `{
		"id":"resp_2",
		"output":[
			{"type":"message","content":[{"type":"output_text","text":"answer"}]}
		],
		"usage":{
			"input_tokens":5,"output_tokens":1,"total_tokens":6,
			"input_tokens_details":{"cached_tokens":3},
			"output_tokens_details":{"reasoning_tokens":0}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	p := NewOpenAIResponses(config.Target{
		Endpoint: srv.URL,
		APIKey:   "sk-test",
		Model:    "gpt-5.5",
		Timeout:  5 * time.Second,
		Stream:   boolPtr(false),
	})

	result, err := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected Success")
	}
	if result.ResponseText != "answer" {
		t.Errorf("ResponseText = %q, want %q", result.ResponseText, "answer")
	}
	if result.CachedInputTokens != 3 {
		t.Errorf("CachedInputTokens = %d, want 3", result.CachedInputTokens)
	}
}

func TestResponsesProbe_HTTPError_BodyRefinesClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"type":"insufficient_quota"}}`, http.StatusForbidden)
	}))
	defer srv.Close()

	p := NewOpenAIResponses(config.Target{
		Endpoint: srv.URL,
		Model:    "gpt-5.5",
		Timeout:  5 * time.Second,
	})

	result, _ := p.Probe(context.Background(), ProbeParams{Prompt: "hi", MaxTokens: 20})
	if result.ErrorType != "quota_exceeded" {
		t.Errorf("ErrorType = %q, want quota_exceeded", result.ErrorType)
	}
	if result.HTTPStatusCode != 403 {
		t.Errorf("HTTPStatusCode = %d, want 403", result.HTTPStatusCode)
	}
}
