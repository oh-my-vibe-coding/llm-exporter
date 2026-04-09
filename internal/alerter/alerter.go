package alerter

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/taosun/llm-exporter/internal/config"
	"github.com/taosun/llm-exporter/internal/status"
)

// Alerter checks consecutive failures and sends webhook alerts.
// A nil *Alerter is safe to call — Check is a no-op.
type Alerter struct {
	mu      sync.RWMutex
	webhook *config.WebhookConfig
	tracker *status.Tracker
}

// New creates an Alerter. Returns nil if webhook is nil (no alerting configured).
func New(webhook *config.WebhookConfig, tracker *status.Tracker) *Alerter {
	if webhook == nil {
		return nil
	}
	return &Alerter{webhook: webhook, tracker: tracker}
}

// Check evaluates whether a webhook alert should fire for the given target.
func (a *Alerter) Check(targetName string) {
	if a == nil {
		return
	}

	a.mu.RLock()
	webhook := a.webhook
	a.mu.RUnlock()

	consecFailures, st := a.tracker.ConsecFailures(targetName)
	if st == nil || consecFailures == 0 {
		return
	}

	if consecFailures%webhook.ConsecutiveFailures == 0 {
		go sendWebhook(webhook.URL, st)
	}
}

func sendWebhook(url string, st *status.TargetStatus) {
	payload, _ := json.Marshal(map[string]any{
		"target":               st.Name,
		"model":                st.Model,
		"endpoint":             st.Endpoint,
		"error":                st.Error,
		"consecutive_failures": st.ConsecFailures,
		"total_probes":         st.TotalProbes,
		"timestamp":            time.Now().Format(time.RFC3339),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("[webhook] failed to create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[webhook] send failed for %s: %v", st.Name, err)
		return
	}
	resp.Body.Close()
	log.Printf("[webhook] alert sent for %s (%d consecutive failures)", st.Name, st.ConsecFailures)
}
