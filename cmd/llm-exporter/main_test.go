package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/scheduler"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/version"
)

func newTestScheduler(t *testing.T) *scheduler.Scheduler {
	t.Helper()
	sched, err := scheduler.New([]config.Target{
		{
			Name:      "test",
			Endpoint:  "https://example.com",
			Model:     "test-model",
			APIFormat: "openai",
			Prompt:    "hi",
			MaxTokens: 10,
		},
	}, nil)
	if err != nil {
		t.Fatalf("scheduler.New: %v", err)
	}
	return sched
}

func TestHealthzEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "ok\n" {
		t.Errorf("body = %q, want %q", body, "ok\n")
	}
}

func TestVersionEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"version":    version.Version,
			"git_commit": version.GitCommit,
			"build_time": version.BuildTime,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["version"] != version.Version {
		t.Errorf("version = %q, want %q", resp["version"], version.Version)
	}
}

func TestReloadEndpoint_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/-/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Write([]byte("ok\n"))
	})

	req := httptest.NewRequest(http.MethodGet, "/-/reload", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestTargetsEndpoint(t *testing.T) {
	sched := newTestScheduler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/targets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sched.GetStatuses())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/targets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestReload_Success(t *testing.T) {
	sched := newTestScheduler(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `targets:
  - name: reloaded
    endpoint: https://example.com
    model: gpt-4
    api_format: openai
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx := context.Background()
	if err := reload(ctx, cfgPath, sched); err != nil {
		t.Fatalf("reload() error: %v", err)
	}
}

func TestReload_InvalidConfig(t *testing.T) {
	sched := newTestScheduler(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// Empty config has no targets, which is invalid.
	if err := os.WriteFile(cfgPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx := context.Background()
	if err := reload(ctx, cfgPath, sched); err == nil {
		t.Error("reload() with invalid config should return error")
	}
}

func TestReloadEndpoint_PostSuccess(t *testing.T) {
	sched := newTestScheduler(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `targets:
  - name: reloaded
    endpoint: https://example.com
    model: gpt-4
    api_format: openai
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/-/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := reload(ctx, cfgPath, sched); err != nil {
			http.Error(w, fmt.Sprintf("reload failed: %v", err), http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/-/reload", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestReloadEndpoint_PostInvalidConfig(t *testing.T) {
	sched := newTestScheduler(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/-/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := reload(ctx, cfgPath, sched); err != nil {
			http.Error(w, fmt.Sprintf("reload failed: %v", err), http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/-/reload", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestLogTargets_NoPanic(t *testing.T) {
	cfg := &config.Config{
		ListenAddr: ":9101",
		Targets: []config.Target{
			{Name: "t1", Model: "m1", APIFormat: "openai", Interval: 5 * time.Minute},
			{Name: "t2", Model: "m2", APIFormat: "anthropic", Interval: 10 * time.Minute},
		},
	}
	// Should not panic
	logTargets(cfg)
}
