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

type anthropicProber struct {
	target config.Target
	client *http.Client
}

func NewAnthropic(t config.Target) Prober {
	return &anthropicProber{
		target: t,
		client: newProbeClient(t.Timeout),
	}
}

func (p *anthropicProber) Probe(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	result := newProbeResult()

	body := map[string]any{
		"model":      p.target.Model,
		"stream":     true,
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

	url := strings.TrimRight(p.target.Endpoint, "/") + "/v1/messages"

	traceCtx, timings := newConnectTrace(ctx)
	req, err := http.NewRequestWithContext(traceCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		result.ErrorType = "network"
		result.Error = err
		return result, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if p.target.APIKey != "" {
		req.Header.Set("x-api-key", p.target.APIKey)
	}
	for k, v := range p.target.ExtraHeaders {
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

		if event.Event == "message_stop" {
			break
		}

		switch event.Event {
		case "message_start":
			var ms anthropicMessageStart
			if err := json.Unmarshal([]byte(event.Data), &ms); err != nil {
				continue
			}
			applyAnthropicUsage(result, ms.Message.Usage)
		case "content_block_delta":
			var delta anthropicDelta
			if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
				continue
			}
			// Accept text_delta for normal output. thinking_delta /
			// signature_delta / input_json_delta arrive on thinking and
			// tool_use blocks; we don't surface them as text but they don't
			// break parsing.
			if delta.Delta.Type == "text_delta" && delta.Delta.Text != "" {
				textBuf.WriteString(delta.Delta.Text)
				if !ttftRecorded {
					result.TTFT = time.Since(start)
					ttftRecorded = true
				}
			}
		case "message_delta":
			var md anthropicMessageDelta
			if err := json.Unmarshal([]byte(event.Data), &md); err != nil {
				continue
			}
			applyAnthropicUsage(result, md.Usage)
		}
	}

	result.Duration = time.Since(start)
	if result.TotalTokens == 0 {
		result.TotalTokens = result.InputTokens + result.OutputTokens
	}
	result.ResponseText = textBuf.String()
	result.Success = ttftRecorded
	if !ttftRecorded {
		result.ErrorType = "parse_error"
		result.Error = fmt.Errorf("no content received in stream")
	}
	return result, result.Error
}

// applyAnthropicUsage merges a usage object into the ProbeResult. Anthropic
// reports usage twice — once in message_start (authoritative for input_tokens,
// cache_creation_input_tokens, cache_read_input_tokens) and once in
// message_delta (authoritative for output_tokens, cumulative). We take the
// maximum of each field so either ordering yields a complete picture.
func applyAnthropicUsage(r *ProbeResult, u anthropicUsage) {
	if u.InputTokens > r.InputTokens {
		r.InputTokens = u.InputTokens
	}
	if u.OutputTokens > r.OutputTokens {
		r.OutputTokens = u.OutputTokens
	}
	if u.CacheCreationInputTokens > r.CacheCreationTokens {
		r.CacheCreationTokens = u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens > r.CachedInputTokens {
		r.CachedInputTokens = u.CacheReadInputTokens
	}
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type anthropicMessageStart struct {
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
}

type anthropicDelta struct {
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type anthropicMessageDelta struct {
	Usage anthropicUsage `json:"usage"`
}
