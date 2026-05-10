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

// responsesProber probes the OpenAI Responses API (/v1/responses). The wire
// protocol differs from Chat Completions: typed SSE events (response.created,
// response.output_text.delta, response.completed, ...) and usage fields use
// input_tokens / output_tokens at the top level, plus nested
// input_tokens_details.cached_tokens and output_tokens_details.reasoning_tokens.
type responsesProber struct {
	target config.Target
	client *http.Client
}

// NewOpenAIResponses returns a Prober for the OpenAI Responses API.
func NewOpenAIResponses(t config.Target) Prober {
	return &responsesProber{
		target: t,
		client: newProbeClient(t.Timeout),
	}
}

func (p *responsesProber) Probe(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	if !p.target.IsStreaming() {
		return p.probeNonStreaming(ctx, params)
	}
	return p.probeStreaming(ctx, params)
}

func (p *responsesProber) url() string {
	chatPath := p.target.ChatPath
	if chatPath == "" {
		chatPath = "/v1/responses"
	}
	return strings.TrimRight(p.target.Endpoint, "/") + chatPath
}

func (p *responsesProber) buildBody(params ProbeParams, stream bool) ([]byte, error) {
	body := map[string]any{
		"model":             p.target.Model,
		"input":             probePrompt(params.Prompt),
		"max_output_tokens": params.MaxTokens,
		"stream":            stream,
	}
	return json.Marshal(body)
}

func (p *responsesProber) newRequest(ctx context.Context, payload []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.target.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.target.APIKey)
	}
	for k, v := range p.target.ExtraHeaders {
		req.Header.Set(k, v)
	}
	return req, nil
}

func (p *responsesProber) probeStreaming(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	result := newProbeResult()

	payload, err := p.buildBody(params, true)
	if err != nil {
		result.ErrorType = "parse_error"
		result.Error = err
		return result, err
	}

	traceCtx, timings := newConnectTrace(ctx)
	req, err := p.newRequest(traceCtx, payload)
	if err != nil {
		result.ErrorType = "network"
		result.Error = err
		return result, err
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

		switch event.Event {
		case "response.output_text.delta":
			var d responsesTextDelta
			if err := json.Unmarshal([]byte(event.Data), &d); err != nil {
				continue
			}
			if d.Delta != "" {
				textBuf.WriteString(d.Delta)
				if !ttftRecorded {
					result.TTFT = time.Since(start)
					ttftRecorded = true
				}
			}
		case "response.completed":
			var c responsesCompleted
			if err := json.Unmarshal([]byte(event.Data), &c); err != nil {
				continue
			}
			applyResponsesUsage(result, c.Response.Usage)
		case "response.failed", "response.incomplete":
			var c responsesCompleted
			if err := json.Unmarshal([]byte(event.Data), &c); err == nil {
				applyResponsesUsage(result, c.Response.Usage)
			}
			result.Duration = time.Since(start)
			result.ErrorType = "api_error"
			result.Error = fmt.Errorf("responses stream ended with %s", event.Event)
			return result, result.Error
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

func (p *responsesProber) probeNonStreaming(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	result := newProbeResult()

	payload, err := p.buildBody(params, false)
	if err != nil {
		result.ErrorType = "parse_error"
		result.Error = err
		return result, err
	}

	traceCtx, timings := newConnectTrace(ctx)
	req, err := p.newRequest(traceCtx, payload)
	if err != nil {
		result.ErrorType = "network"
		result.Error = err
		return result, err
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

	var data responsesObject
	if err := json.Unmarshal(respBody, &data); err != nil {
		result.Duration = time.Since(start)
		result.ErrorType = "parse_error"
		result.Error = fmt.Errorf("parse response: %w", err)
		return result, result.Error
	}

	result.Duration = time.Since(start)
	result.TTFT = result.Duration

	if text := data.OutputText(); text != "" {
		result.ResponseText = text
		result.Success = true
	}

	applyResponsesUsage(result, data.Usage)

	if !result.Success {
		result.ErrorType = "parse_error"
		result.Error = fmt.Errorf("no content in response")
	}
	return result, result.Error
}

type responsesTextDelta struct {
	Delta string `json:"delta"`
}

type responsesCompleted struct {
	Response responsesObject `json:"response"`
}

type responsesObject struct {
	Usage  responsesUsage  `json:"usage"`
	Output []responsesItem `json:"output"`
}

// OutputText walks the output items and concatenates any output_text content.
// Used for non-streaming responses where the full object is returned.
func (r *responsesObject) OutputText() string {
	var b strings.Builder
	for _, item := range r.Output {
		if item.Type != "message" {
			continue
		}
		for _, c := range item.Content {
			if c.Type == "output_text" {
				b.WriteString(c.Text)
			}
		}
	}
	return b.String()
}

type responsesItem struct {
	Type    string                 `json:"type"`
	Content []responsesItemContent `json:"content"`
}

type responsesItemContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`

	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`

	OutputTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func applyResponsesUsage(r *ProbeResult, u responsesUsage) {
	if u.TotalTokens == 0 && u.InputTokens == 0 && u.OutputTokens == 0 {
		return
	}
	r.InputTokens = u.InputTokens
	r.OutputTokens = u.OutputTokens
	if u.TotalTokens > 0 {
		r.TotalTokens = u.TotalTokens
	} else {
		r.TotalTokens = u.InputTokens + u.OutputTokens
	}
	r.CachedInputTokens = u.InputTokensDetails.CachedTokens
	r.ReasoningTokens = u.OutputTokensDetails.ReasoningTokens
}
