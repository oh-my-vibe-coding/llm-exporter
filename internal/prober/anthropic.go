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
	result := &ProbeResult{}

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

		if event.Event == "message_stop" {
			break
		}

		switch event.Event {
		case "message_start":
			var ms anthropicMessageStart
			if err := json.Unmarshal([]byte(event.Data), &ms); err != nil {
				continue
			}
			if ms.Message.Usage.InputTokens > 0 {
				result.InputTokens = ms.Message.Usage.InputTokens
			}
		case "content_block_delta":
			var delta anthropicDelta
			if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
				continue
			}
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
			if md.Usage.OutputTokens > 0 {
				result.OutputTokens = md.Usage.OutputTokens
			}
		}
	}

	result.Duration = time.Since(start)
	result.TotalTokens = result.InputTokens + result.OutputTokens
	result.ResponseText = textBuf.String()
	result.Success = ttftRecorded
	if !ttftRecorded {
		result.ErrorType = "parse_error"
		result.Error = fmt.Errorf("no content received in stream")
	}
	return result, result.Error
}

type anthropicMessageStart struct {
	Message struct {
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type anthropicDelta struct {
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type anthropicMessageDelta struct {
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
