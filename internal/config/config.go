package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr string            `yaml:"listen_addr"`
	Targets    []Target          `yaml:"targets"`
	Webhook    *WebhookConfig    `yaml:"webhook"`
	Modules    map[string]Module `yaml:"modules"`
}

type WebhookConfig struct {
	URL                 string `yaml:"url"`
	ConsecutiveFailures int    `yaml:"consecutive_failures"`
}

// Module is a reusable probe profile used by the /probe?module=<name>&target=<url>
// endpoint (blackbox_exporter-style multi-target mode). The target URL from
// the query string becomes Endpoint at probe time; every other field is read
// from the module.
type Module struct {
	APIFormat     string            `yaml:"api_format"`
	APIKey        string            `yaml:"api_key"`
	Model         string            `yaml:"model"`
	Prompt        string            `yaml:"prompt"`
	APIVersion    string            `yaml:"api_version"`
	ChatPath      string            `yaml:"chat_path"`
	ExtraHeaders  map[string]string `yaml:"extra_headers"`
	Timeout       time.Duration     `yaml:"timeout"`
	MaxTokens     int               `yaml:"max_tokens"`
	Stream        *bool             `yaml:"stream"`
	ExpectPattern string            `yaml:"expect_pattern"`
}

// ToTarget materializes a Module with the given target URL into a runnable
// Target. The returned Target is NOT validated by Load; callers relying on
// defaults should apply them explicitly.
func (m Module) ToTarget(name, endpoint string) Target {
	t := Target{
		Name:          name,
		Endpoint:      endpoint,
		APIKey:        m.APIKey,
		Model:         m.Model,
		Prompt:        m.Prompt,
		APIFormat:     m.APIFormat,
		APIVersion:    m.APIVersion,
		ChatPath:      m.ChatPath,
		ExtraHeaders:  m.ExtraHeaders,
		Timeout:       m.Timeout,
		MaxTokens:     m.MaxTokens,
		Stream:        m.Stream,
		ExpectPattern: m.ExpectPattern,
	}
	if t.APIFormat == "" {
		t.APIFormat = "openai"
	}
	if t.Timeout == 0 {
		t.Timeout = 30 * time.Second
	}
	if t.MaxTokens == 0 {
		t.MaxTokens = 20
	}
	if t.Prompt == "" {
		t.Prompt = "Hi"
	}
	return t
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

	// Adaptive interval: when enabled, the probe interval increases after
	// consecutive successes and resets to the base interval on failure.
	AdaptiveInterval bool          `yaml:"adaptive_interval"`
	MaxInterval      time.Duration `yaml:"max_interval"`
	BackoffAfter     int           `yaml:"backoff_after"`
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

var validAPIFormats = map[string]bool{
	"openai": true, "openai-responses": true, "azure": true, "anthropic": true, "google": true,
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

	if len(cfg.Targets) == 0 && len(cfg.Modules) == 0 {
		return nil, fmt.Errorf("no targets or modules configured")
	}

	for name, m := range cfg.Modules {
		if m.APIFormat != "" && !validAPIFormats[m.APIFormat] {
			return nil, fmt.Errorf("module %q: unsupported api_format %q (must be one of: openai, openai-responses, azure, anthropic, google)", name, m.APIFormat)
		}
		if m.ExpectPattern != "" {
			if _, err := regexp.Compile(m.ExpectPattern); err != nil {
				return nil, fmt.Errorf("module %q: invalid expect_pattern: %w", name, err)
			}
		}
	}

	seenNames := make(map[string]int)

	for i := range cfg.Targets {
		t := &cfg.Targets[i]

		// --- Required fields (fail fast) ---
		if t.Name == "" {
			return nil, fmt.Errorf("target at index %d: name is required", i)
		}
		if prev, ok := seenNames[t.Name]; ok {
			return nil, fmt.Errorf("target %q at index %d: duplicate name (first at index %d)", t.Name, i, prev)
		}
		seenNames[t.Name] = i
		if t.Endpoint == "" {
			return nil, fmt.Errorf("target %q: endpoint is required", t.Name)
		}
		if t.Model == "" {
			return nil, fmt.Errorf("target %q: model is required", t.Name)
		}

		// --- Mutual exclusivity ---
		if t.Prompt != "" && len(t.Prompts) > 0 {
			return nil, fmt.Errorf("target %q: cannot set both prompt and prompts", t.Name)
		}

		// --- Defaults ---
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
		if t.APIFormat == "" {
			t.APIFormat = "openai"
		}
		if t.APIFormat == "azure" && t.APIVersion == "" {
			t.APIVersion = "2024-10-21"
		}

		// --- Enum validation ---
		if !validAPIFormats[t.APIFormat] {
			return nil, fmt.Errorf("target %q: unsupported api_format %q (must be one of: openai, openai-responses, azure, anthropic, google)", t.Name, t.APIFormat)
		}

		// --- Numeric constraints ---
		if t.Timeout < 0 {
			return nil, fmt.Errorf("target %q: timeout must not be negative", t.Name)
		}
		if t.Interval < 0 {
			return nil, fmt.Errorf("target %q: interval must not be negative", t.Name)
		}

		// --- Light probe mode ---
		if t.FullProbeEvery == 1 {
			return nil, fmt.Errorf("target %q: full_probe_every must be >= 2 (1 makes light mode pointless)", t.Name)
		}
		if t.FullProbeEvery > 0 {
			if t.LightPrompt == "" {
				t.LightPrompt = "Hi"
			}
			if t.LightMaxTokens == 0 {
				t.LightMaxTokens = 5
			}
		}
		if t.FullProbeEvery == 0 && (t.LightPrompt != "" || t.LightMaxTokens != 0) {
			return nil, fmt.Errorf("target %q: light_prompt/light_max_tokens have no effect without full_probe_every > 0", t.Name)
		}

		// --- Adaptive interval ---
		if t.AdaptiveInterval {
			if t.MaxInterval == 0 {
				t.MaxInterval = t.Interval * 4
			}
			if t.BackoffAfter == 0 {
				t.BackoffAfter = 5
			}
			if t.MaxInterval < t.Interval {
				return nil, fmt.Errorf("target %q: max_interval must be >= interval", t.Name)
			}
		}
		if !t.AdaptiveInterval && (t.MaxInterval != 0 || t.BackoffAfter != 0) {
			return nil, fmt.Errorf("target %q: max_interval/backoff_after have no effect without adaptive_interval: true", t.Name)
		}

		// --- Regex ---
		if t.ExpectPattern != "" {
			if _, err := regexp.Compile(t.ExpectPattern); err != nil {
				return nil, fmt.Errorf("target %q: invalid expect_pattern: %w", t.Name, err)
			}
		}
	}

	return &cfg, nil
}
