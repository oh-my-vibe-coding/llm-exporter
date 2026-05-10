package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
)

func TestProbeHandler_MissingQuery(t *testing.T) {
	cfg := &config.Config{Modules: map[string]config.Module{"m1": {APIFormat: "openai"}}}
	var ptr atomic.Pointer[config.Config]
	ptr.Store(cfg)
	h := probeHandler(ptr.Load)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe?target=https://x.example", nil)
	h(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("missing module should 400, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/probe?module=m1", nil)
	h(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("missing target should 400, got %d", rr.Code)
	}
}

func TestProbeHandler_UnknownModule(t *testing.T) {
	cfg := &config.Config{Modules: map[string]config.Module{"m1": {APIFormat: "openai"}}}
	var ptr atomic.Pointer[config.Config]
	ptr.Store(cfg)
	h := probeHandler(ptr.Load)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe?target=https://x.example&module=unknown", nil)
	h(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("unknown module should 400, got %d", rr.Code)
	}
}

func TestProbeHandler_RunsProbeAndEmitsMetrics(t *testing.T) {
	sseBody := `data: {"choices":[{"delta":{"content":"hi"}}]}

data: {"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}

data: [DONE]

`
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			t.Errorf("backend path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer backend.Close()

	cfg := &config.Config{
		Modules: map[string]config.Module{
			"tiny-openai": {
				APIFormat: "openai",
				Model:     "gpt-4",
				APIKey:    "sk-test",
				Timeout:   5 * time.Second,
				MaxTokens: 5,
				Prompt:    "Hi",
			},
		},
	}
	var ptr atomic.Pointer[config.Config]
	ptr.Store(cfg)
	h := probeHandler(ptr.Load)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe?target="+backend.URL+"&module=tiny-openai", nil)
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("probe status = %d, body = %s", rr.Code, rr.Body.String())
	}
	body, _ := io.ReadAll(rr.Body)
	text := string(body)
	for _, want := range []string{
		"probe_success 1",
		"probe_input_tokens 2",
		"probe_output_tokens 1",
		"probe_total_tokens 3",
		"probe_http_status_code 200",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in response; body:\n%s", want, text)
		}
	}
}

func TestProbeHandler_EmitsErrorType(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"type":"insufficient_quota"}}`, http.StatusForbidden)
	}))
	defer backend.Close()

	cfg := &config.Config{
		Modules: map[string]config.Module{
			"m": {APIFormat: "openai", Model: "gpt-4", Timeout: 5 * time.Second, MaxTokens: 5, Prompt: "Hi"},
		},
	}
	var ptr atomic.Pointer[config.Config]
	ptr.Store(cfg)
	h := probeHandler(ptr.Load)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe?target="+backend.URL+"&module=m", nil)
	h(rr, req)
	text := rr.Body.String()
	if !strings.Contains(text, `probe_error_type{type="quota_exceeded"} 1`) {
		t.Errorf("expected probe_error_type with quota_exceeded; body:\n%s", text)
	}
	if !strings.Contains(text, "probe_http_status_code 403") {
		t.Errorf("expected probe_http_status_code 403; body:\n%s", text)
	}
}
