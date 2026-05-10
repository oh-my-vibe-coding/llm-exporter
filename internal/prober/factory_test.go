package prober

import (
	"testing"
	"time"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
)

func TestNew_AllFormats(t *testing.T) {
	base := config.Target{
		Name:     "test",
		Endpoint: "https://example.com",
		Model:    "test-model",
		Timeout:  5 * time.Second,
	}

	formats := []string{"openai", "openai-responses", "anthropic", "google", "azure"}
	for _, f := range formats {
		t.Run(f, func(t *testing.T) {
			target := base
			target.APIFormat = f
			p, err := New(target)
			if err != nil {
				t.Fatalf("New(%q) error: %v", f, err)
			}
			if p == nil {
				t.Fatalf("New(%q) returned nil", f)
			}
		})
	}
}

func TestNew_UnsupportedFormat(t *testing.T) {
	target := config.Target{
		Name:      "test",
		Endpoint:  "https://example.com",
		Model:     "test-model",
		APIFormat: "unsupported",
		Timeout:   5 * time.Second,
	}
	_, err := New(target)
	if err == nil {
		t.Fatal("New(unsupported) should return error")
	}
}
