package prober

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestClassifyHTTPStatus(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{401, "auth"},
		{403, "auth"},
		{429, "rate_limit"},
		{529, "overloaded"},
		{500, "http_5xx"},
		{502, "http_5xx"},
		{503, "http_5xx"},
		{504, "http_5xx"},
		{400, "http_4xx"},
		{404, "http_4xx"},
		{418, "http_4xx"},
		{200, "api_error"},
		{301, "api_error"},
	}
	for _, c := range cases {
		if got := classifyHTTPStatus(c.code); got != c.want {
			t.Errorf("classifyHTTPStatus(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestClassifyProviderBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty", "", ""},
		{"anthropic overloaded", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, "overloaded"},
		{"openai insufficient quota", `{"error":{"type":"insufficient_quota","message":"You exceeded your current quota"}}`, "quota_exceeded"},
		{"openai context length", `{"error":{"code":"context_length_exceeded","message":"maximum context length is 128k"}}`, "context_length"},
		{"content filter", `{"error":{"type":"content_policy_violation"}}`, "content_filter"},
		{"gemini resource exhausted", `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`, "rate_limit"},
		{"invalid api key", `{"error":{"type":"invalid_api_key"}}`, "auth"},
		{"unknown", `{"error":{"message":"something went wrong"}}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyProviderBody(c.body); got != c.want {
				t.Errorf("classifyProviderBody(%q) = %q, want %q", c.body, got, c.want)
			}
		})
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestClassifyNetworkError(t *testing.T) {
	var _ net.Error = timeoutErr{}

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"canceled", context.Canceled, "canceled"},
		{"net timeout", timeoutErr{}, "timeout"},
		{"wrapped deadline text", errors.New("post: context deadline exceeded"), "timeout"},
		{"tls error", errors.New("tls: handshake failure"), "tls_error"},
		{"dns no such host", errors.New("dial tcp: lookup api.example.com: no such host"), "dns_error"},
		{"connection refused", errors.New("dial tcp 127.0.0.1:9999: connect: connection refused"), "connection_refused"},
		{"generic network", errors.New("boom"), "network"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyNetworkError(c.err); got != c.want {
				t.Errorf("classifyNetworkError(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
	_ = time.Now // keep time import if pruned later
}
