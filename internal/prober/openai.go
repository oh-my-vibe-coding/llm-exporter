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

type openaiProber struct {
	target config.Target
	client *http.Client
}

func NewOpenAI(t config.Target) Prober {
	return &openaiProber{
		target: t,
		client: newProbeClient(t.Timeout),
	}
}

func (p *openaiProber) Probe(ctx context.Context) (*ProbeResult, error) {
	result := &ProbeResult{}

	body := map[string]any{
		"model":      p.target.Model,
		"stream":     true,
		"max_tokens": p.target.MaxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": probePrompt(p.target.Prompt)},
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

	chatPath := p.target.ChatPath
	if chatPath == "" {
		chatPath = "/v1/chat/completions"
	}
	url := strings.TrimRight(p.target.Endpoint, "/") + chatPath

	traceCtx, timings := newConnectTrace(ctx)
	req, err := http.NewRequestWithContext(traceCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		result.ErrorType = "network"
		result.Error = err
		return result, err
	}

	req.Header.Set("Content-Type", "application/json")
	if p.target.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.target.APIKey)
	}
	for k, v := range p.target.ExtraHeaders {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		result.Duration = time.Since(start)
		result.ConnectDuration = timings.duration(start)
		result.ErrorType = classifyNetworkError(err)
		result.Error = err
		return result, err
	}
	defer resp.Body.Close()

	result.ConnectDuration = timings.duration(start)

	if resp.StatusCode != http.StatusOK {
		result.Duration = time.Since(start)
		result.ErrorType = classifyHTTPStatus(resp.StatusCode)
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		result.Error = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errBody))
		return result, result.Error
	}

	reader := NewSSEReader(resp.Body)
	var ttftRecorded bool

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

		if !ttftRecorded && len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			result.TTFT = time.Since(start)
			ttftRecorded = true
		}

		if chunk.Usage.TotalTokens > 0 {
			result.InputTokens = chunk.Usage.PromptTokens
			result.OutputTokens = chunk.Usage.CompletionTokens
			result.TotalTokens = chunk.Usage.TotalTokens
		}
	}

	result.Duration = time.Since(start)
	result.Success = ttftRecorded
	if !ttftRecorded {
		result.ErrorType = "parse_error"
		result.Error = fmt.Errorf("no content received in stream")
	}
	return result, result.Error
}

type openaiChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
