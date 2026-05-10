package prober

import (
	"context"
	"net"
	"strings"
)

// classifyHTTPStatus returns a coarse error_type for a non-2xx HTTP status.
// The HTTP status code itself is surfaced via the status label on
// llm_probe_errors_total, so this function narrows by semantic category only.
func classifyHTTPStatus(code int) string {
	switch {
	case code == 401, code == 403:
		return "auth"
	case code == 429:
		return "rate_limit"
	case code == 529:
		return "overloaded"
	case code >= 500 && code < 600:
		return "http_5xx"
	case code >= 400 && code < 500:
		return "http_4xx"
	default:
		return "api_error"
	}
}

// classifyProviderBody inspects a provider JSON error body (already read, may
// be up to ~1KiB) and returns a refined error_type, or "" if no refinement
// applies. Callers should prefer the body classification over the status-code
// one when non-empty.
//
// We intentionally pattern-match the small set of well-documented error codes
// the research identified — Anthropic `overloaded_error`, OpenAI
// `insufficient_quota` / `context_length_exceeded`, content-filter signals,
// and Gemini `RESOURCE_EXHAUSTED`. This is a best-effort refinement, not a
// strict JSON decode.
func classifyProviderBody(body string) string {
	if body == "" {
		return ""
	}
	lower := strings.ToLower(body)
	switch {
	case strings.Contains(lower, "overloaded_error"),
		strings.Contains(lower, "\"overloaded\""):
		return "overloaded"
	case strings.Contains(lower, "insufficient_quota"),
		strings.Contains(lower, "billing_not_active"),
		strings.Contains(lower, "quota_exceeded"):
		return "quota_exceeded"
	case strings.Contains(lower, "context_length_exceeded"),
		strings.Contains(lower, "maximum context length"),
		strings.Contains(lower, "context window"):
		return "context_length"
	case strings.Contains(lower, "content_filter"),
		strings.Contains(lower, "content_policy"),
		strings.Contains(lower, "safety"):
		return "content_filter"
	case strings.Contains(lower, "resource_exhausted"):
		return "rate_limit"
	case strings.Contains(lower, "invalid_api_key"),
		strings.Contains(lower, "authentication_error"),
		strings.Contains(lower, "permission_denied"):
		return "auth"
	}
	return ""
}

func classifyNetworkError(err error) string {
	if err == nil {
		return ""
	}
	if err == context.DeadlineExceeded {
		return "timeout"
	}
	if err == context.Canceled {
		return "canceled"
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return "timeout"
	}
	msg := err.Error()
	if strings.Contains(msg, "context deadline exceeded") {
		return "timeout"
	}
	if strings.Contains(msg, "context canceled") {
		return "canceled"
	}
	if strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:") {
		return "tls_error"
	}
	if strings.Contains(msg, "no such host") || strings.Contains(msg, "dns") {
		return "dns_error"
	}
	if strings.Contains(msg, "connection refused") {
		return "connection_refused"
	}
	return "network"
}
