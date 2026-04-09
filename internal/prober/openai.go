package prober

import (
	"net/http"
	"strings"

	"github.com/taosun/llm-exporter/internal/config"
)

func NewOpenAI(t config.Target) Prober {
	return newOpenAICompat(openaiCompatConfig{
		target:   t,
		buildURL: openaiURL,
		setAuth:  openaiAuth,
	})
}

func openaiURL(t config.Target) string {
	chatPath := t.ChatPath
	if chatPath == "" {
		chatPath = "/v1/chat/completions"
	}
	return strings.TrimRight(t.Endpoint, "/") + chatPath
}

func openaiAuth(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}
