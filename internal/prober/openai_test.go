package prober

import (
	"net/http"
	"testing"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
)

func TestOpenaiURL(t *testing.T) {
	tests := []struct {
		name   string
		target config.Target
		want   string
	}{
		{
			name:   "default chat path",
			target: config.Target{Endpoint: "https://api.openai.com"},
			want:   "https://api.openai.com/v1/chat/completions",
		},
		{
			name:   "custom chat path",
			target: config.Target{Endpoint: "https://api.openai.com", ChatPath: "/v2/chat"},
			want:   "https://api.openai.com/v2/chat",
		},
		{
			name:   "trailing slash stripped",
			target: config.Target{Endpoint: "https://api.openai.com/"},
			want:   "https://api.openai.com/v1/chat/completions",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := openaiURL(tt.target)
			if got != tt.want {
				t.Errorf("openaiURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpenaiAuth(t *testing.T) {
	t.Run("sets bearer token", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
		openaiAuth(req, "sk-test-key")
		got := req.Header.Get("Authorization")
		want := "Bearer sk-test-key"
		if got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
	})

	t.Run("no header when empty", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
		openaiAuth(req, "")
		if got := req.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
	})
}
