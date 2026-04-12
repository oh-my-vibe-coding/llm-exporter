package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/oh-my-vibe-coding/llm-exporter/internal/config"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/metrics"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/scheduler"
	"github.com/oh-my-vibe-coding/llm-exporter/internal/version"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	watchConfig := flag.Bool("watch-config", false, "watch config file for changes and auto-reload (useful in Kubernetes)")
	validate := flag.Bool("validate", false, "validate config file and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("llm-exporter %s (commit=%s, built=%s)\n", version.Version, version.GitCommit, version.BuildTime)
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	if *validate {
		log.Printf("config is valid: %d targets", len(cfg.Targets))
		logTargets(cfg)
		os.Exit(0)
	}

	logTargets(cfg)

	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	metrics.Register(reg)

	sched, err := scheduler.New(cfg.Targets, cfg.Webhook)
	if err != nil {
		log.Fatalf("failed to create scheduler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sched.Run(ctx)

	if *watchConfig {
		go watchConfigFile(ctx, *configPath, sched)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/-/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := reload(ctx, *configPath, sched); err != nil {
			log.Printf("reload failed: %v", err)
			http.Error(w, fmt.Sprintf("reload failed: %v", err), http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/api/v1/targets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sched.GetStatuses())
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"version":    version.Version,
			"git_commit": version.GitCommit,
			"build_time": version.BuildTime,
		})
	})

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		for sig := range sigCh {
			switch sig {
			case syscall.SIGHUP:
				if err := reload(ctx, *configPath, sched); err != nil {
					log.Printf("reload failed: %v", err)
				}
			case syscall.SIGINT, syscall.SIGTERM:
				log.Println("shutting down...")
				cancel()
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutdownCancel()
				server.Shutdown(shutdownCtx)
				return
			}
		}
	}()

	log.Printf("listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

// watchConfigFile monitors the config file for changes and triggers a reload.
// It watches the parent directory to handle Kubernetes ConfigMap symlink swaps.
func watchConfigFile(ctx context.Context, configPath string, sched *scheduler.Scheduler) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		log.Printf("watch-config: failed to resolve path: %v", err)
		return
	}
	dir := filepath.Dir(absPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("watch-config: failed to create watcher: %v", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(dir); err != nil {
		log.Printf("watch-config: failed to watch %s: %v", dir, err)
		return
	}

	log.Printf("watching config file %s for changes", absPath)

	// Debounce: K8s ConfigMap updates can fire multiple events in quick succession.
	var debounce *time.Timer
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Respond to writes to the config file itself, or CREATE events
			// on the directory (Kubernetes symlink swap shows up as CREATE).
			if event.Name == absPath || event.Op&fsnotify.Create != 0 {
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(2*time.Second, func() {
					if err := reload(ctx, configPath, sched); err != nil {
						log.Printf("watch-config: reload failed: %v", err)
					}
				})
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watch-config: watcher error: %v", err)
		}
	}
}

func reload(ctx context.Context, configPath string, sched *scheduler.Scheduler) error {
	log.Printf("reloading config from %s", configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := sched.Reload(ctx, cfg.Targets, cfg.Webhook); err != nil {
		return fmt.Errorf("reload scheduler: %w", err)
	}
	logTargets(cfg)
	log.Printf("reload complete")
	return nil
}

func logTargets(cfg *config.Config) {
	log.Printf("loaded %d targets", len(cfg.Targets))
	for _, t := range cfg.Targets {
		log.Printf("  target: %s (model=%s, format=%s, interval=%s)", t.Name, t.Model, t.APIFormat, t.Interval)
	}
}
