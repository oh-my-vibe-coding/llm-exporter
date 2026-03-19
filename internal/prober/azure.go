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

	"github.com/taosun/llm-exporter/internal/config"
)

type azureProber struct {
	target config.Target
	client *http.Client
}

func NewAzure(t config.Target) Prober {
	return &azureProber{
		target: t,
		client: newProbeClient(t.Timeout),
	}
}

func (p *azureProber) Probe(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	if !p.target.IsStreaming() {
		return p.probeNonStreaming(ctx, params)
	}
	return p.probeStreaming(ctx, params)
}

func (p *azureProber) buildURL() string {
	apiVersion := p.target.APIVersion
	if apiVersion == "" {
		apiVersion = "2024-10-21"
	}
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		strings.TrimRight(p.target.Endpoint, "/"), p.target.Model, apiVersion)
}

func (p *azureProber) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.target.APIKey != "" {
		req.Header.Set("api-key", p.target.APIKey)
	}
	for k, v := range p.target.ExtraHeaders {
		req.Header.Set(k, v)
	}
}

func (p *azureProber) probeStreaming(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	result := &ProbeResult{}

	body := map[string]any{
		"model":      p.target.Model,
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

	traceCtx, timings := newConnectTrace(ctx)
	req, err := http.NewRequestWithContext(traceCtx, http.MethodPost, p.buildURL(), bytes.NewReader(payload))
	if err != nil {
		result.ErrorType = "network"
		result.Error = err
		return result, err
	}
	p.setHeaders(req)

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

	if resp.StatusCode != http.StatusOK {
		result.Duration = time.Since(start)
		result.ErrorType = classifyHTTPStatus(resp.StatusCode)
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
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

func (p *azureProber) probeNonStreaming(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	result := &ProbeResult{}

	body := map[string]any{
		"model":      p.target.Model,
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

	traceCtx, timings := newConnectTrace(ctx)
	req, err := http.NewRequestWithContext(traceCtx, http.MethodPost, p.buildURL(), bytes.NewReader(payload))
	if err != nil {
		result.ErrorType = "network"
		result.Error = err
		return result, err
	}
	p.setHeaders(req)

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

	if resp.StatusCode != http.StatusOK {
		result.Duration = time.Since(start)
		result.ErrorType = classifyHTTPStatus(resp.StatusCode)
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
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

	if !result.Success {
		result.ErrorType = "parse_error"
		result.Error = fmt.Errorf("no content in response")
	}
	return result, result.Error
}
