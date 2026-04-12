package prober

import (
	"net/http"
	"testing"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
)

func TestAzureURL(t *testing.T) {
	tests := []struct {
		name   string
		target config.Target
		want   string
	}{
		{
			name:   "default api version",
			target: config.Target{Endpoint: "https://myresource.openai.azure.com", Model: "gpt-4"},
			want:   "https://myresource.openai.azure.com/openai/deployments/gpt-4/chat/completions?api-version=2024-10-21",
		},
		{
			name:   "custom api version",
			target: config.Target{Endpoint: "https://myresource.openai.azure.com", Model: "gpt-4", APIVersion: "2023-12-01"},
			want:   "https://myresource.openai.azure.com/openai/deployments/gpt-4/chat/completions?api-version=2023-12-01",
		},
		{
			name:   "trailing slash stripped",
			target: config.Target{Endpoint: "https://myresource.openai.azure.com/", Model: "gpt-4"},
			want:   "https://myresource.openai.azure.com/openai/deployments/gpt-4/chat/completions?api-version=2024-10-21",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := azureURL(tt.target)
			if got != tt.want {
				t.Errorf("azureURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAzureAuth(t *testing.T) {
	t.Run("sets api-key header", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
		azureAuth(req, "my-azure-key")
		got := req.Header.Get("api-key")
		if got != "my-azure-key" {
			t.Errorf("api-key = %q, want %q", got, "my-azure-key")
		}
	})

	t.Run("no header when empty", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
		azureAuth(req, "")
		if got := req.Header.Get("api-key"); got != "" {
			t.Errorf("api-key = %q, want empty", got)
		}
	})
}
