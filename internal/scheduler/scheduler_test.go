package scheduler

import (
	"testing"

	"github.com/taosun/llm-exporter/internal/config"
	"github.com/taosun/llm-exporter/internal/prober"
)

func TestBuildParams_Default(t *testing.T) {
	target := config.Target{
		Prompt:    "Hello",
		MaxTokens: 20,
	}

	params := buildParams(target, 0)
	if params.Prompt != "Hello" {
		t.Errorf("Prompt = %q, want %q", params.Prompt, "Hello")
	}
	if params.MaxTokens != 20 {
		t.Errorf("MaxTokens = %d, want 20", params.MaxTokens)
	}
}

func TestBuildParams_LightMode(t *testing.T) {
	target := config.Target{
		Prompt:         "Count from 1 to 20",
		MaxTokens:      100,
		FullProbeEvery: 5,
		LightPrompt:    "Hi",
		LightMaxTokens: 5,
	}

	// count=0 => full (0 % 5 == 0)
	p0 := buildParams(target, 0)
	if p0.Prompt != "Count from 1 to 20" || p0.MaxTokens != 100 {
		t.Errorf("count=0: got Prompt=%q MaxTokens=%d, want full probe", p0.Prompt, p0.MaxTokens)
	}

	// count=1 => light
	p1 := buildParams(target, 1)
	if p1.Prompt != "Hi" || p1.MaxTokens != 5 {
		t.Errorf("count=1: got Prompt=%q MaxTokens=%d, want light probe", p1.Prompt, p1.MaxTokens)
	}

	// count=5 => full again
	p5 := buildParams(target, 5)
	if p5.Prompt != "Count from 1 to 20" || p5.MaxTokens != 100 {
		t.Errorf("count=5: got Prompt=%q MaxTokens=%d, want full probe", p5.Prompt, p5.MaxTokens)
	}
}

func TestBuildParams_PromptRotation(t *testing.T) {
	target := config.Target{
		Prompts:   []string{"A", "B", "C"},
		MaxTokens: 20,
	}

	for i := 0; i < 6; i++ {
		params := buildParams(target, i)
		want := target.Prompts[i%3]
		if params.Prompt != want {
			t.Errorf("count=%d: Prompt = %q, want %q", i, params.Prompt, want)
		}
	}
}

func TestBuildParams_PromptRotationWithLightMode(t *testing.T) {
	target := config.Target{
		Prompts:        []string{"A", "B", "C"},
		MaxTokens:      100,
		FullProbeEvery: 3,
		LightPrompt:    "light-default",
		LightMaxTokens: 5,
	}

	// count=0 => full, prompt "A"
	p := buildParams(target, 0)
	if p.Prompt != "A" || p.MaxTokens != 100 {
		t.Errorf("count=0: Prompt=%q MaxTokens=%d", p.Prompt, p.MaxTokens)
	}

	// count=1 => light, prompt "B" (from rotation)
	p = buildParams(target, 1)
	if p.Prompt != "B" || p.MaxTokens != 5 {
		t.Errorf("count=1: Prompt=%q MaxTokens=%d", p.Prompt, p.MaxTokens)
	}
}

func TestProbeType(t *testing.T) {
	target := config.Target{
		FullProbeEvery: 10,
		LightMaxTokens: 5,
	}

	full := prober.ProbeParams{MaxTokens: 20}
	light := prober.ProbeParams{MaxTokens: 5}

	if got := probeType(target, full); got != "full" {
		t.Errorf("probeType(full) = %q, want %q", got, "full")
	}
	if got := probeType(target, light); got != "light" {
		t.Errorf("probeType(light) = %q, want %q", got, "light")
	}

	// No light mode configured
	noLight := config.Target{}
	if got := probeType(noLight, full); got != "full" {
		t.Errorf("probeType(noLight, full) = %q, want %q", got, "full")
	}
}
