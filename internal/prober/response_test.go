package prober

import (
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net/http"
	"testing"
	"time"
)

func TestParseRateLimitHeaders_OpenAI(t *testing.T) {
	h := http.Header{}
	h.Set("x-ratelimit-remaining-requests", "42")
	h.Set("x-ratelimit-remaining-tokens", "12345")
	req, tok := parseRateLimitHeaders(h)
	if req != 42 {
		t.Errorf("requests = %d, want 42", req)
	}
	if tok != 12345 {
		t.Errorf("tokens = %d, want 12345", tok)
	}
}

func TestParseRateLimitHeaders_Anthropic(t *testing.T) {
	h := http.Header{}
	h.Set("anthropic-ratelimit-requests-remaining", "9")
	h.Set("anthropic-ratelimit-tokens-remaining", "5000")
	req, tok := parseRateLimitHeaders(h)
	if req != 9 {
		t.Errorf("requests = %d, want 9", req)
	}
	if tok != 5000 {
		t.Errorf("tokens = %d, want 5000", tok)
	}
}

func TestParseRateLimitHeaders_Missing(t *testing.T) {
	h := http.Header{}
	req, tok := parseRateLimitHeaders(h)
	if req != -1 || tok != -1 {
		t.Errorf("expected (-1,-1), got (%d,%d)", req, tok)
	}
}

func TestParseRateLimitHeaders_Malformed(t *testing.T) {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining-Requests", "not a number")
	req, _ := parseRateLimitHeaders(h)
	if req != -1 {
		t.Errorf("malformed value should leave target = -1, got %d", req)
	}
}

func TestEarliestPeerCertNotAfter_Nil(t *testing.T) {
	got := earliestPeerCertNotAfter(nil)
	if !got.IsZero() {
		t.Errorf("want zero, got %v", got)
	}
}

func TestEarliestPeerCertNotAfter_Empty(t *testing.T) {
	got := earliestPeerCertNotAfter(&tls.ConnectionState{})
	if !got.IsZero() {
		t.Errorf("want zero, got %v", got)
	}
}

func TestEarliestPeerCertNotAfter_PicksEarliest(t *testing.T) {
	t1 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2029, 6, 1, 0, 0, 0, 0, time.UTC)

	state := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{NotAfter: t1, SerialNumber: big.NewInt(1)},
			{NotAfter: t2, SerialNumber: big.NewInt(2)},
			{NotAfter: t3, SerialNumber: big.NewInt(3)},
		},
	}
	got := earliestPeerCertNotAfter(state)
	if !got.Equal(t2) {
		t.Errorf("earliest = %v, want %v", got, t2)
	}
}
