package prober

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
)

// openaiCompatConfig provides the two points of variation between
// OpenAI-protocol-compatible probers: URL construction and auth headers.
type openaiCompatConfig struct {
	target   config.Target
	buildURL func(config.Target) string
	setAuth  func(req *http.Request, apiKey string)
}

// openaiCompatProber implements the Prober interface for any
// OpenAI-compatible chat completions API.
type openaiCompatProber struct {
	cfg    openaiCompatConfig
	client *http.Client
}

func newOpenAICompat(cfg openaiCompatConfig) *openaiCompatProber {
	return &openaiCompatProber{
		cfg:    cfg,
		client: newProbeClient(cfg.target.Timeout),
	}
}

func (p *openaiCompatProber) Probe(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	if !p.cfg.target.IsStreaming() {
		return p.probeNonStreaming(ctx, params)
	}
	return p.probeStreaming(ctx, params)
}

func (p *openaiCompatProber) probeStreaming(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	result := newProbeResult()

	body := map[string]any{
		"model":      p.cfg.target.Model,
		"stream":     true,
		"max_tokens": params.MaxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": probePrompt(params.Prompt)},
		},
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		result.ErrorType = "parse_error"
		result.Error = err
		return result, err
	}

	url := p.cfg.buildURL(p.cfg.target)

	traceCtx, timings := newConnectTrace(ctx)
	req, err := http.NewRequestWithContext(traceCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		result.ErrorType = "network"
		result.Error = err
		return result, err
	}

	req.Header.Set("Content-Type", "application/json")
	p.cfg.setAuth(req, p.cfg.target.APIKey)
	for k, v := range p.cfg.target.ExtraHeaders {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		result.Duration = time.Since(start)
		result.ConnectDuration = timings.duration()
		result.ErrorType = classifyNetworkError(err)
		result.Error = err
		return result, err
	}
	defer resp.Body.Close()

	result.ConnectDuration = timings.duration()
	populateFromResponse(result, resp)

	if resp.StatusCode != http.StatusOK {
		result.Duration = time.Since(start)
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if refined := classifyProviderBody(string(errBody)); refined != "" {
			result.ErrorType = refined
		} else {
			result.ErrorType = classifyHTTPStatus(resp.StatusCode)
		}
		result.Error = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errBody))
		return result, result.Error
	}

	reader := NewSSEReader(resp.Body)
	var ttftRecorded bool
	var textBuf strings.Builder

	for {
		event, err := reader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			result.Duration = time.Since(start)
			result.ErrorType = "parse_error"
			result.Error = err
			return result, err
		}

		if event.Data == "[DONE]" {
			break
		}

		var chunk openaiChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content := chunk.Choices[0].Delta.Content
			textBuf.WriteString(content)
			if !ttftRecorded {
				result.TTFT = time.Since(start)
				ttftRecorded = true
			}
		}

		if chunk.Usage.TotalTokens > 0 {
			result.InputTokens = chunk.Usage.PromptTokens
			result.OutputTokens = chunk.Usage.CompletionTokens
			result.TotalTokens = chunk.Usage.TotalTokens
			result.CachedInputTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			result.ReasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
		}
	}

	result.Duration = time.Since(start)
	result.ResponseText = textBuf.String()
	result.Success = ttftRecorded
	if !ttftRecorded {
		result.ErrorType = "parse_error"
		result.Error = fmt.Errorf("no content received in stream")
	}
	return result, result.Error
}

func (p *openaiCompatProber) probeNonStreaming(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	result := newProbeResult()

	body := map[string]any{
		"model":      p.cfg.target.Model,
		"max_tokens": params.MaxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": probePrompt(params.Prompt)},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		result.ErrorType = "parse_error"
		result.Error = err
		return result, err
	}

	url := p.cfg.buildURL(p.cfg.target)

	traceCtx, timings := newConnectTrace(ctx)
	req, err := http.NewRequestWithContext(traceCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		result.ErrorType = "network"
		result.Error = err
		return result, err
	}

	req.Header.Set("Content-Type", "application/json")
	p.cfg.setAuth(req, p.cfg.target.APIKey)
	for k, v := range p.cfg.target.ExtraHeaders {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		result.Duration = time.Since(start)
		result.ConnectDuration = timings.duration()
		result.ErrorType = classifyNetworkError(err)
		result.Error = err
		return result, err
	}
	defer resp.Body.Close()

	result.ConnectDuration = timings.duration()
	populateFromResponse(result, resp)

	if resp.StatusCode != http.StatusOK {
		result.Duration = time.Since(start)
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if refined := classifyProviderBody(string(errBody)); refined != "" {
			result.ErrorType = refined
		} else {
			result.ErrorType = classifyHTTPStatus(resp.StatusCode)
		}
		result.Error = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errBody))
		return result, result.Error
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result.Duration = time.Since(start)
		result.ErrorType = "parse_error"
		result.Error = err
		return result, err
	}

	var respData openaiResponse
	if err := json.Unmarshal(respBody, &respData); err != nil {
		result.Duration = time.Since(start)
		result.ErrorType = "parse_error"
		result.Error = fmt.Errorf("parse response: %w", err)
		return result, result.Error
	}

	result.Duration = time.Since(start)
	result.TTFT = result.Duration

	if len(respData.Choices) > 0 && respData.Choices[0].Message.Content != "" {
		result.ResponseText = respData.Choices[0].Message.Content
		result.Success = true
	}

	result.InputTokens = respData.Usage.PromptTokens
	result.OutputTokens = respData.Usage.CompletionTokens
	result.TotalTokens = respData.Usage.TotalTokens
	result.CachedInputTokens = respData.Usage.PromptTokensDetails.CachedTokens
	result.ReasoningTokens = respData.Usage.CompletionTokensDetails.ReasoningTokens

	if !result.Success {
		result.ErrorType = "parse_error"
		result.Error = fmt.Errorf("no content in response")
	}
	return result, result.Error
}

type openaiChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage openaiUsage `json:"usage"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage openaiUsage `json:"usage"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
		AudioTokens  int `json:"audio_tokens"`
	} `json:"prompt_tokens_details"`

	CompletionTokensDetails struct {
		ReasoningTokens          int `json:"reasoning_tokens"`
		AudioTokens              int `json:"audio_tokens"`
		AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
		RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
	} `json:"completion_tokens_details"`
}
