package prober

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
)

func NewAzure(t config.Target) Prober {
	return newOpenAICompat(openaiCompatConfig{
		target:   t,
		buildURL: azureURL,
		setAuth:  azureAuth,
	})
}

func azureURL(t config.Target) string {
	apiVersion := t.APIVersion
	if apiVersion == "" {
		apiVersion = "2024-10-21"
	}
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		strings.TrimRight(t.Endpoint, "/"), t.Model, apiVersion)
}

func azureAuth(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("api-key", apiKey)
	}
}
