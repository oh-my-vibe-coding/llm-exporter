package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	os.Setenv("TEST_KEY_1", "value1")
	os.Setenv("TEST_KEY_2", "value2")
	defer os.Unsetenv("TEST_KEY_1")
	defer os.Unsetenv("TEST_KEY_2")

	tests := []struct {
		input string
		want  string
	}{
		{"${TEST_KEY_1}", "value1"},
		{"prefix-${TEST_KEY_1}-suffix", "prefix-value1-suffix"},
		{"${TEST_KEY_1}-${TEST_KEY_2}", "value1-value2"},
		{"${NONEXISTENT_VAR}", "${NONEXISTENT_VAR}"},
		{"no vars here", "no vars here"},
		{"", ""},
	}
	for _, tt := range tests {
		got := expandEnv(tt.input)
		if got != tt.want {
			t.Errorf("expandEnv(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoad_Defaults(t *testing.T) {
	content := `
targets:
  - name: test
    endpoint: "https://example.com"
    model: "gpt-4o"
`
	path := writeTemp(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ListenAddr != ":9101" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9101")
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(cfg.Targets))
	}

	tgt := cfg.Targets[0]
	if tgt.Prompt != "Hi" {
		t.Errorf("Prompt = %q, want %q", tgt.Prompt, "Hi")
	}
	if tgt.APIFormat != "openai" {
		t.Errorf("APIFormat = %q, want %q", tgt.APIFormat, "openai")
	}
	if tgt.MaxTokens != 20 {
		t.Errorf("MaxTokens = %d, want 20", tgt.MaxTokens)
	}
	if tgt.Timeout.Seconds() != 30 {
		t.Errorf("Timeout = %v, want 30s", tgt.Timeout)
	}
	if tgt.Interval.Seconds() != 300 {
		t.Errorf("Interval = %v, want 300s", tgt.Interval)
	}
	if !tgt.IsStreaming() {
		t.Error("IsStreaming() = false, want true")
	}
}

func TestLoad_AzureDefaults(t *testing.T) {
	content := `
targets:
  - name: azure-test
    endpoint: "https://myresource.openai.azure.com"
    model: "gpt-4o"
    api_format: azure
`
	path := writeTemp(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	tgt := cfg.Targets[0]
	if tgt.APIVersion != "2024-10-21" {
		t.Errorf("APIVersion = %q, want %q", tgt.APIVersion, "2024-10-21")
	}
}

func TestLoad_Validation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			"missing name",
			`targets: [{endpoint: "https://x.com", model: "m"}]`,
			"name is required",
		},
		{
			"missing endpoint",
			`targets: [{name: "t", model: "m"}]`,
			"endpoint is required",
		},
		{
			"missing model",
			`targets: [{name: "t", endpoint: "https://x.com"}]`,
			"model is required",
		},
		{
			"invalid expect_pattern",
			`targets: [{name: "t", endpoint: "https://x.com", model: "m", expect_pattern: "[invalid"}]`,
			"invalid expect_pattern",
		},
		{
			"webhook missing url",
			"webhook:\n  consecutive_failures: 3\ntargets: [{name: t, endpoint: 'https://x.com', model: m}]",
			"webhook: url is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, tt.content)
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !contains(got, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestLoad_Prompts(t *testing.T) {
	content := `
targets:
  - name: test
    endpoint: "https://example.com"
    model: "gpt-4o"
    prompts:
      - "Hello"
      - "Hi"
      - "Hey"
`
	path := writeTemp(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	tgt := cfg.Targets[0]
	if len(tgt.Prompts) != 3 {
		t.Errorf("len(Prompts) = %d, want 3", len(tgt.Prompts))
	}
}

func TestLoad_StreamFalse(t *testing.T) {
	content := `
targets:
  - name: test
    endpoint: "https://example.com"
    model: "gpt-4o"
    stream: false
`
	path := writeTemp(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	tgt := cfg.Targets[0]
	if tgt.IsStreaming() {
		t.Error("IsStreaming() = true, want false")
	}
}

func TestLoad_WebhookDefaults(t *testing.T) {
	content := `
webhook:
  url: "https://hooks.example.com/webhook"
targets:
  - name: test
    endpoint: "https://example.com"
    model: "gpt-4o"
`
	path := writeTemp(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Webhook == nil {
		t.Fatal("Webhook is nil")
	}
	if cfg.Webhook.ConsecutiveFailures != 3 {
		t.Errorf("ConsecutiveFailures = %d, want 3", cfg.Webhook.ConsecutiveFailures)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
