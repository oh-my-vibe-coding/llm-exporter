package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr string         `yaml:"listen_addr"`
	Targets    []Target       `yaml:"targets"`
	Webhook    *WebhookConfig `yaml:"webhook"`
}

type WebhookConfig struct {
	URL                 string `yaml:"url"`
	ConsecutiveFailures int    `yaml:"consecutive_failures"`
}

type Target struct {
	Name         string            `yaml:"name"`
	Endpoint     string            `yaml:"endpoint"`
	APIKey       string            `yaml:"api_key"`
	Model        string            `yaml:"model"`
	Prompt       string            `yaml:"prompt"`
	Prompts      []string          `yaml:"prompts"`
	APIFormat    string            `yaml:"api_format"`
	Timeout      time.Duration     `yaml:"timeout"`
	Interval     time.Duration     `yaml:"interval"`
	MaxTokens    int               `yaml:"max_tokens"`
	ChatPath     string            `yaml:"chat_path"`
	APIVersion   string            `yaml:"api_version"`
	ExtraHeaders map[string]string `yaml:"extra_headers"`
	Stream       *bool             `yaml:"stream"`
	ExpectPattern string           `yaml:"expect_pattern"`

	// Light probe mode: most probes use minimal tokens, only periodic full probes.
	// FullProbeEvery=0 disables light mode (default). FullProbeEvery=10 means
	// every 10th probe is full, the other 9 are light.
	FullProbeEvery int    `yaml:"full_probe_every"`
	LightPrompt    string `yaml:"light_prompt"`
	LightMaxTokens int    `yaml:"light_max_tokens"`
}

// IsStreaming returns whether the target uses streaming mode. Defaults to true.
func (t Target) IsStreaming() bool {
	if t.Stream == nil {
		return true
	}
	return *t.Stream
}

var envVarRe = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		key := envVarRe.FindStringSubmatch(match)[1]
		if val, ok := os.LookupEnv(key); ok {
			return val
		}
		return match
	})
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	expanded := expandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":9101"
	}

	if cfg.Webhook != nil {
		if cfg.Webhook.URL == "" {
			return nil, fmt.Errorf("webhook: url is required")
		}
		if cfg.Webhook.ConsecutiveFailures <= 0 {
			cfg.Webhook.ConsecutiveFailures = 3
		}
	}

	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		if t.Prompt == "" && len(t.Prompts) == 0 {
			t.Prompt = "Hi"
		}
		if t.Timeout == 0 {
			t.Timeout = 30 * time.Second
		}
		if t.Interval == 0 {
			t.Interval = 300 * time.Second
		}
		if t.MaxTokens == 0 {
			t.MaxTokens = 20
		}
		if t.FullProbeEvery > 0 {
			if t.LightPrompt == "" {
				t.LightPrompt = "Hi"
			}
			if t.LightMaxTokens == 0 {
				t.LightMaxTokens = 5
			}
		}
		if t.APIFormat == "" {
			t.APIFormat = "openai"
		}
		if t.APIFormat == "azure" && t.APIVersion == "" {
			t.APIVersion = "2024-10-21"
		}
		if t.ExpectPattern != "" {
			if _, err := regexp.Compile(t.ExpectPattern); err != nil {
				return nil, fmt.Errorf("target %q: invalid expect_pattern: %w", t.Name, err)
			}
		}
		if t.Name == "" {
			return nil, fmt.Errorf("target at index %d: name is required", i)
		}
		if t.Endpoint == "" {
			return nil, fmt.Errorf("target %q: endpoint is required", t.Name)
		}
		if t.Model == "" {
			return nil, fmt.Errorf("target %q: model is required", t.Name)
		}
	}

	return &cfg, nil
}
