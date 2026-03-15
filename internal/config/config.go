package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr string   `yaml:"listen_addr"`
	Targets    []Target `yaml:"targets"`
}

type Target struct {
	Name         string            `yaml:"name"`
	Endpoint     string            `yaml:"endpoint"`
	APIKey       string            `yaml:"api_key"`
	Model        string            `yaml:"model"`
	Prompt       string            `yaml:"prompt"`
	APIFormat    string            `yaml:"api_format"`
	Timeout      time.Duration     `yaml:"timeout"`
	Interval     time.Duration     `yaml:"interval"`
	MaxTokens    int               `yaml:"max_tokens"`
	ChatPath     string            `yaml:"chat_path"`
	ExtraHeaders map[string]string `yaml:"extra_headers"`
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

	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		if t.Prompt == "" {
			t.Prompt = "Count from 1 to 20, one number per line."
		}
		if t.Timeout == 0 {
			t.Timeout = 30 * time.Second
		}
		if t.Interval == 0 {
			t.Interval = 60 * time.Second
		}
		if t.MaxTokens == 0 {
			t.MaxTokens = 100
		}
		if t.APIFormat == "" {
			t.APIFormat = "openai"
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
