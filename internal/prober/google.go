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

type googleProber struct {
	target config.Target
	client *http.Client
}

func NewGoogle(t config.Target) Prober {
	return &googleProber{
		target: t,
		client: newProbeClient(t.Timeout),
	}
}

func (p *googleProber) Probe(ctx context.Context, params ProbeParams) (*ProbeResult, error) {
	result := &ProbeResult{}

	body := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]string{
					{"text": probePrompt(params.Prompt)},
				},
			},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": params.MaxTokens,
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		result.ErrorType = "parse_error"
		result.Error = err
		return result, err
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		strings.TrimRight(p.target.Endpoint, "/"),
		p.target.Model,
		p.target.APIKey,
	)

	traceCtx, timings := newConnectTrace(ctx)
	req, err := http.NewRequestWithContext(traceCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		result.ErrorType = "network"
		result.Error = err
		return result, err
	}

	req.Header.Set("Content-Type", "application/json")
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

		var chunk googleChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			continue
		}

		if !ttftRecorded {
			for _, c := range chunk.Candidates {
				for _, p := range c.Content.Parts {
					if p.Text != "" {
						textBuf.WriteString(p.Text)
						result.TTFT = time.Since(start)
						ttftRecorded = true
						break
					}
				}
				if ttftRecorded {
					break
				}
			}
		} else {
			for _, c := range chunk.Candidates {
				for _, p := range c.Content.Parts {
					textBuf.WriteString(p.Text)
				}
			}
		}

		if chunk.UsageMetadata.TotalTokenCount > 0 {
			result.InputTokens = chunk.UsageMetadata.PromptTokenCount
			result.OutputTokens = chunk.UsageMetadata.CandidatesTokenCount
			result.TotalTokens = chunk.UsageMetadata.TotalTokenCount
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

type googleChunk struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}
