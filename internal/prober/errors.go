package prober

import (
	"context"
	"net"
	"strings"
)

func classifyHTTPStatus(code int) string {
	switch {
	case code == 401 || code == 403:
		return "auth"
	case code == 429:
		return "rate_limit"
	default:
		return "api_error"
	}
}

func classifyNetworkError(err error) string {
	if err == nil {
		return ""
	}
	if ctx := context.DeadlineExceeded; err == ctx {
		return "timeout"
	}
	if ctx := context.Canceled; err == ctx {
		return "timeout"
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return "timeout"
	}
	if strings.Contains(err.Error(), "context deadline exceeded") {
		return "timeout"
	}
	if strings.Contains(err.Error(), "context canceled") {
		return "timeout"
	}
	return "network"
}
