package prober

import (
	"crypto/tls"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// populateFromResponse fills HTTPStatusCode, RateLimitRemaining{Requests,Tokens},
// and SSLCertNotAfter on the given ProbeResult. Safe to call with resp==nil.
func populateFromResponse(result *ProbeResult, resp *http.Response) {
	if result == nil || resp == nil {
		return
	}
	result.HTTPStatusCode = resp.StatusCode
	result.RateLimitRemainingRequests, result.RateLimitRemainingTokens = parseRateLimitHeaders(resp.Header)
	result.SSLCertNotAfter = earliestPeerCertNotAfter(resp.TLS)
}

// parseRateLimitHeaders extracts rate-limit remaining counts from common
// provider response headers. Returns (-1, -1) when a header is missing, so
// callers can distinguish "provider did not report" from "zero remaining".
//
// OpenAI / OpenAI-compatible: x-ratelimit-remaining-requests, x-ratelimit-remaining-tokens
// Anthropic: anthropic-ratelimit-requests-remaining, anthropic-ratelimit-tokens-remaining
// Gemini: no standard header at time of writing; caller may extend.
func parseRateLimitHeaders(h http.Header) (remainingRequests, remainingTokens int) {
	remainingRequests, remainingTokens = -1, -1

	candidates := []struct {
		headerName string
		target     *int
	}{
		{"X-RateLimit-Remaining-Requests", &remainingRequests},
		{"X-Ratelimit-Remaining-Requests", &remainingRequests},
		{"Anthropic-Ratelimit-Requests-Remaining", &remainingRequests},
		{"X-RateLimit-Remaining-Tokens", &remainingTokens},
		{"X-Ratelimit-Remaining-Tokens", &remainingTokens},
		{"Anthropic-Ratelimit-Tokens-Remaining", &remainingTokens},
	}

	for _, c := range candidates {
		if *c.target != -1 {
			continue
		}
		if v := h.Get(c.headerName); v != "" {
			v = strings.TrimSpace(v)
			if n, err := strconv.Atoi(v); err == nil {
				*c.target = n
			}
		}
	}
	return
}

// earliestPeerCertNotAfter returns the earliest NotAfter across the TLS
// handshake's peer certificates, or the zero time if state is nil or empty.
func earliestPeerCertNotAfter(state *tls.ConnectionState) time.Time {
	if state == nil || len(state.PeerCertificates) == 0 {
		return time.Time{}
	}
	earliest := state.PeerCertificates[0].NotAfter
	for _, cert := range state.PeerCertificates[1:] {
		if cert.NotAfter.Before(earliest) {
			earliest = cert.NotAfter
		}
	}
	return earliest
}
