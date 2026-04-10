# LLM Exporter

**English** | [中文](README_zh.md)

> **100% AI-Coded** — All code in this project was written entirely by AI (Claude Code). Human involvement was limited to product direction, requirements, and code review.

A Prometheus exporter for monitoring LLM API availability and performance. Periodically sends streaming probe requests to LLM API endpoints, collecting network latency, Time to First Token (TTFT), and token usage metrics.

Think of it as [blackbox_exporter](https://github.com/prometheus/blackbox_exporter) for LLM APIs — end-to-end probing tailored for large language models.

## Features

- **Streaming TTFT measurement** — All APIs probed via streaming requests for precise Time to First Token measurement
- **Multi-provider support** — OpenAI / Anthropic / Google Gemini / Azure OpenAI, plus any OpenAI-compatible service
- **Wide provider coverage** — DashScope (Alibaba), Volcengine Ark (ByteDance), DeepSeek, Mistral, OpenRouter, local Ollama, and more
- **Lightweight** — Only depends on `prometheus/client_golang`, `gopkg.in/yaml.v3`, and `fsnotify/fsnotify` — no LLM SDKs
- **Single binary** — Go compiled, deploys as binary / Docker / Kubernetes
- **Flexible config** — YAML with `${ENV_VAR}` expansion, custom headers, and custom API paths
- **Hot reload** — SIGHUP signal, HTTP `/-/reload` endpoint, or `--watch-config` file watcher
- **Webhook alerting** — Sends webhook notifications (Slack/Teams/etc.) after consecutive probe failures
- **Prompt rotation** — Cycle through multiple prompts to avoid provider-side caching
- **Response validation** — Regex match on model output to verify the model is actually working
- **Adaptive probe interval** — Automatically reduces probe frequency when stable, resets to base interval on failure
- **Non-streaming mode** — `stream: false` for APIs that don't support streaming
- **Ops-friendly** — `--validate` config check, `--version` info, `/api/v1/targets` status API

## Quick Start

### Binary

```bash
# Build
make build

# Configure
cp config.example.yaml config.yaml
# Edit config.yaml, fill in your API keys

# Set environment variables (or write directly in config.yaml)
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."

# Run
./llm-exporter --config config.yaml

# Verify
curl http://localhost:9101/metrics | grep llm_probe
curl http://localhost:9101/healthz
```

### Docker Compose (with Prometheus + Grafana)

```bash
# Configure API keys
cp .env.example .env
# Edit .env

# Start the full monitoring stack
docker compose up --build -d

# Access
# LLM Exporter:  http://localhost:9101/metrics
# Prometheus:     http://localhost:9090
# Grafana:        http://localhost:3000  (admin/admin)
```

Grafana auto-loads a pre-built LLM monitoring dashboard on startup.

## Deployment

### Systemd

Suitable for direct deployment on Linux servers.

**1. Install binary**

```bash
make build
sudo cp llm-exporter /usr/local/bin/
sudo chmod +x /usr/local/bin/llm-exporter

sudo mkdir -p /etc/llm-exporter
sudo cp config.example.yaml /etc/llm-exporter/config.yaml
sudo vim /etc/llm-exporter/config.yaml
```

**2. Create system user**

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin llm-exporter
```

**3. Create systemd service**

```bash
sudo cat > /etc/systemd/system/llm-exporter.service << 'EOF'
[Unit]
Description=LLM Exporter - Prometheus LLM API Probe Exporter
Documentation=https://github.com/oh-my-vibe-coding/llm-exporter
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=llm-exporter
Group=llm-exporter
ExecStart=/usr/local/bin/llm-exporter --config /etc/llm-exporter/config.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadOnlyPaths=/etc/llm-exporter
PrivateTmp=yes

EnvironmentFile=-/etc/llm-exporter/env

[Install]
WantedBy=multi-user.target
EOF
```

**4. Environment file (optional)**

```bash
sudo cat > /etc/llm-exporter/env << 'EOF'
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
EOF
sudo chmod 600 /etc/llm-exporter/env
sudo chown llm-exporter:llm-exporter /etc/llm-exporter/env
```

**5. Start**

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now llm-exporter
sudo systemctl status llm-exporter
sudo journalctl -u llm-exporter -f
```

### Docker

```bash
docker run -d \
  --name llm-exporter \
  --restart unless-stopped \
  -p 9101:9101 \
  -v $(pwd)/config.yaml:/etc/llm-exporter/config.yaml:ro \
  --env-file .env \
  llm-exporter
```

**Docker Compose (full stack):**

```bash
docker compose up --build -d
```

| Service | Port | Description |
|---------|------|-------------|
| llm-exporter | 9101 | Exporter metrics endpoint |
| prometheus | 9090 | Prometheus server |
| grafana | 3000 | Grafana (admin/admin), auto-provisioned dashboard |

### Kubernetes

**1. ConfigMap and Secret**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: llm-exporter-config
  namespace: monitoring
data:
  config.yaml: |
    listen_addr: ":9101"
    targets:
      - name: openai-gpt4o
        endpoint: "https://api.openai.com"
        api_key: "${OPENAI_API_KEY}"
        model: "gpt-4o"
        api_format: openai
        timeout: 30s
        interval: 60s
---
apiVersion: v1
kind: Secret
metadata:
  name: llm-exporter-secrets
  namespace: monitoring
type: Opaque
stringData:
  OPENAI_API_KEY: "sk-..."
  ANTHROPIC_API_KEY: "sk-ant-..."
```

**2. Deployment + Service**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: llm-exporter
  namespace: monitoring
  labels:
    app: llm-exporter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: llm-exporter
  template:
    metadata:
      labels:
        app: llm-exporter
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9101"
        prometheus.io/path: "/metrics"
    spec:
      containers:
        - name: llm-exporter
          image: llm-exporter:latest
          args: ["--config", "/etc/llm-exporter/config.yaml", "--watch-config"]
          ports:
            - containerPort: 9101
              name: metrics
          envFrom:
            - secretRef:
                name: llm-exporter-secrets
          volumeMounts:
            - name: config
              mountPath: /etc/llm-exporter
              readOnly: true
          livenessProbe:
            httpGet:
              path: /healthz
              port: 9101
            initialDelaySeconds: 5
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /healthz
              port: 9101
            initialDelaySeconds: 3
            periodSeconds: 10
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
      volumes:
        - name: config
          configMap:
            name: llm-exporter-config
---
apiVersion: v1
kind: Service
metadata:
  name: llm-exporter
  namespace: monitoring
  labels:
    app: llm-exporter
spec:
  selector:
    app: llm-exporter
  ports:
    - port: 9101
      targetPort: metrics
      name: metrics
```

**3. ServiceMonitor (Prometheus Operator)**

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: llm-exporter
  namespace: monitoring
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      app: llm-exporter
  endpoints:
    - port: metrics
      interval: 15s
      path: /metrics
```

**4. Deploy**

```bash
kubectl apply -f llm-exporter-config.yaml
kubectl apply -f llm-exporter-deployment.yaml
kubectl apply -f llm-exporter-servicemonitor.yaml
```

## Hot Reload

Three ways to reload config without restarting:

**SIGHUP signal:**
```bash
sudo systemctl reload llm-exporter
# or: kill -HUP $(pidof llm-exporter)
# Docker: docker kill --signal=HUP llm-exporter
```

**HTTP endpoint:**
```bash
curl -X POST http://localhost:9101/-/reload
```

**File watcher (recommended for Kubernetes):**
```bash
llm-exporter --config /etc/llm-exporter/config.yaml --watch-config
```

Handles Kubernetes ConfigMap symlink swaps with 2-second debounce. On reload failure, existing probes continue running unaffected.

## Configuration

### Config Format

```yaml
listen_addr: ":9101"      # Listen address, default :9101

# Webhook alerting (optional)
# webhook:
#   url: "https://hooks.slack.com/services/xxx"
#   consecutive_failures: 3  # default 3

targets:
  - name: openai-gpt4o     # Target name (required, used as provider label)
    endpoint: "https://api.openai.com"   # API endpoint (required)
    api_key: "${OPENAI_API_KEY}"         # API key, supports env vars
    model: "gpt-4o"                      # Model name (required)
    prompt: "Hi"                         # Probe prompt, default "Hi"
    api_format: openai                   # API format: openai / anthropic / google / azure
    timeout: 30s                         # Probe timeout, default 30s
    interval: 300s                       # Probe interval, default 300s
    max_tokens: 20                       # Max output tokens, default 20
    chat_path: "/v1/chat/completions"    # Custom API path (openai format only)
    extra_headers:                       # Custom request headers
      X-Custom-Auth: "token"
    # stream: false                      # Non-streaming mode (default true)
    # api_version: "2024-10-21"          # Azure API version (azure format only)
    # prompts: ["Hi", "Hello", "Hey"]    # Prompt rotation (mutually exclusive with prompt)
    # expect_pattern: "\\d+"             # Response validation regex
    # adaptive_interval: true            # Adaptive interval (default off)
    # max_interval: 1200s                # Adaptive cap, default 4x interval
    # backoff_after: 5                   # Consecutive successes before backoff, default 5
```

### Provider Examples

<details>
<summary>OpenAI</summary>

```yaml
- name: openai-gpt4o
  endpoint: "https://api.openai.com"
  api_key: "${OPENAI_API_KEY}"
  model: "gpt-4o"
  api_format: openai
```
</details>

<details>
<summary>Anthropic Claude</summary>

```yaml
- name: anthropic-claude
  endpoint: "https://api.anthropic.com"
  api_key: "${ANTHROPIC_API_KEY}"
  model: "claude-sonnet-4-20250514"
  api_format: anthropic
```
</details>

<details>
<summary>Google Gemini</summary>

```yaml
- name: google-gemini
  endpoint: "https://generativelanguage.googleapis.com"
  api_key: "${GOOGLE_API_KEY}"
  model: "gemini-2.0-flash"
  api_format: google
```
</details>

<details>
<summary>Azure OpenAI</summary>

```yaml
- name: azure-gpt4o
  endpoint: "${AZURE_OPENAI_ENDPOINT}"
  api_key: "${AZURE_OPENAI_API_KEY}"
  model: "gpt-4o"
  api_format: azure
  # api_version: "2024-10-21"  # default
```
</details>

<details>
<summary>DashScope (Alibaba)</summary>

```yaml
- name: dashscope-qwen
  endpoint: "https://dashscope.aliyuncs.com/compatible-mode"
  api_key: "${DASHSCOPE_API_KEY}"
  model: "qwen-plus"
  api_format: openai
```
</details>

<details>
<summary>Volcengine Ark (ByteDance)</summary>

```yaml
- name: ark-pro
  endpoint: "https://ark.cn-beijing.volces.com/api"
  api_key: "${ARK_API_KEY}"
  model: "${ARK_ENDPOINT_ID}"
  api_format: openai
```
</details>

<details>
<summary>DeepSeek / Mistral / OpenRouter / Ollama</summary>

```yaml
# DeepSeek
- name: deepseek-chat
  endpoint: "https://api.deepseek.com"
  api_key: "${DEEPSEEK_API_KEY}"
  model: "deepseek-chat"
  api_format: openai

# Mistral
- name: mistral-large
  endpoint: "https://api.mistral.ai"
  api_key: "${MISTRAL_API_KEY}"
  model: "mistral-large-latest"
  api_format: openai

# OpenRouter
- name: openrouter-claude
  endpoint: "https://openrouter.ai/api"
  api_key: "${OPENROUTER_API_KEY}"
  model: "anthropic/claude-sonnet-4"
  api_format: openai

# Local Ollama
- name: local-ollama
  endpoint: "http://localhost:11434"
  model: "llama3"
  api_format: openai
  interval: 30s
```
</details>

<details>
<summary>Custom proxy</summary>

```yaml
- name: my-proxy
  endpoint: "https://my-proxy.example.com"
  api_key: "${PROXY_API_KEY}"
  model: "gpt-4o"
  api_format: openai
  chat_path: "/api/v1/chat"
  extra_headers:
    X-Proxy-Token: "${PROXY_TOKEN}"
```
</details>

### Environment Variable Expansion

All `${VAR_NAME}` values in the config file are automatically replaced with the corresponding environment variable at load time. If the variable is not set, the literal string is preserved.

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `llm_probe_success` | Gauge | Whether the last probe succeeded (1=success, 0=failure) |
| `llm_probe_duration_seconds` | Histogram | Full request duration (from request start to stream end) |
| `llm_probe_connect_duration_seconds` | Histogram | Connection setup time (DNS + TCP + TLS) |
| `llm_probe_ttft_seconds` | Histogram | Time to First Token (from request start to first content token) |
| `llm_probe_input_tokens` | Gauge | Input tokens for the last probe |
| `llm_probe_output_tokens` | Gauge | Output tokens for the last probe |
| `llm_probe_total_tokens` | Gauge | Total tokens consumed by the last probe |
| `llm_probe_token_rate` | Gauge | Token generation rate (output_tokens / generation_time, tok/s) |
| `llm_probe_last_success_timestamp_seconds` | Gauge | Unix timestamp of the last successful probe |
| `llm_probe_errors_total` | Counter | Cumulative error count by `error_type` |

### Labels

All metrics carry these labels:

| Label | Description |
|-------|-------------|
| `provider` | Target name (from config `name` field) |
| `model` | Model name |
| `endpoint` | API endpoint URL |
| `api_format` | API format (openai / anthropic / google / azure) |

`llm_probe_errors_total` has an additional `error_type` label:

| error_type | Description |
|------------|-------------|
| `timeout` | Request timed out |
| `canceled` | Request canceled (during reload or shutdown) |
| `auth` | Authentication failure (HTTP 401/403) |
| `rate_limit` | Rate limited (HTTP 429) |
| `network` | Network error (DNS failure, connection refused, etc.) |
| `api_error` | Other HTTP error status codes |
| `parse_error` | Response parse failure or empty stream |
| `validation_error` | Response didn't match `expect_pattern` regex |

### Histogram Buckets

```
llm_probe_duration_seconds:         0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60
llm_probe_connect_duration_seconds: 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5
llm_probe_ttft_seconds:             0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20
```

## Prometheus Config

```yaml
scrape_configs:
  - job_name: llm-exporter
    scrape_interval: 15s
    static_configs:
      - targets: ["localhost:9101"]
```

<details>
<summary>Alert rules examples</summary>

```yaml
groups:
  - name: llm-probe
    rules:
      - alert: LLMProbeDown
        expr: llm_probe_success == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "LLM endpoint {{ $labels.provider }} is down"

      - alert: LLMHighTTFT
        expr: histogram_quantile(0.99, sum(rate(llm_probe_ttft_seconds_bucket[10m])) by (le, provider, model)) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High TTFT for {{ $labels.provider }}"

      - alert: LLMRateLimited
        expr: increase(llm_probe_errors_total{error_type="rate_limit"}[10m]) > 3
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.provider }} is being rate limited"
```
</details>

## PromQL Examples

```promql
# Availability
llm_probe_success                                    # Current status
avg_over_time(llm_probe_success[24h])                # 24h availability

# TTFT percentiles
histogram_quantile(0.50, sum(rate(llm_probe_ttft_seconds_bucket[5m])) by (le, provider, model))
histogram_quantile(0.95, sum(rate(llm_probe_ttft_seconds_bucket[5m])) by (le, provider, model))
histogram_quantile(0.99, sum(rate(llm_probe_ttft_seconds_bucket[5m])) by (le, provider, model))

# Errors by type
sum(rate(llm_probe_errors_total[5m])) by (provider, error_type)

# Token rate
llm_probe_token_rate

# Connection latency P95
histogram_quantile(0.95, sum(rate(llm_probe_connect_duration_seconds_bucket[5m])) by (le, provider, model))
```

## Grafana Dashboard

A pre-built Grafana dashboard is included at `grafana/dashboards/llm-exporter.json`.

| Section | Description |
|---------|-------------|
| **Overview** | 9 stat panels summarizing all targets |
| **Target Detail** | Per-provider: 11 stats + 8 time series (repeated by provider) |
| **Cross-Provider** | 6 side-by-side comparisons across providers |
| **TTFT Heatmap** | Collapsed by default, shows TTFT distribution patterns |

Supports `provider` and `model` template variables for filtering. Auto-provisioned when using Docker Compose.

## TTFT Measurement

All APIs use **streaming** mode. TTFT is measured from the moment the HTTP request is sent:

| API Format | TTFT Trigger |
|------------|-------------|
| OpenAI | First SSE event with non-empty `choices[0].delta.content` |
| Anthropic | First `content_block_delta` with non-empty `text_delta` |
| Google | First candidate containing text content |
| Azure | Same as OpenAI |

## Probe Methodology

| Strategy | Purpose |
|----------|---------|
| **No connection reuse** | Each probe creates a fresh TCP connection (`DisableKeepAlives`) for accurate DNS + TCP + TLS timing |
| **Prompt randomization** | Appends `[t=<unix_millis>]` to prevent proxy/CDN caching |
| **Configurable output** | Default `max_tokens=20`, increase for more accurate token rate measurement |
| **Token rate threshold** | Only calculated when output >= 5 tokens and generation time >= 100ms |
| **Three-phase timing** | Connect (DNS+TCP+TLS) -> Wait (TTFT - Connect) -> Generation (Duration - TTFT) |

## Adaptive Probe Interval

When `adaptive_interval: true`:

- **Stable state**: After `backoff_after` (default 5) consecutive successes, interval doubles each time (e.g., 5m -> 10m -> 20m), capped at `max_interval` (default 4x base)
- **On failure**: Immediately resets to base interval

Combine with light probe mode for maximum savings: `interval=300s` + `adaptive_interval=true` + `full_probe_every=10` reduces token usage by 80%+.

## Build

```bash
make build               # Build with version injection
make test                # Run tests
make docker              # Build Docker image
./llm-exporter --version # Check version
./llm-exporter --validate --config config.yaml  # Validate config
```

## HTTP Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/metrics` | GET | Prometheus metrics |
| `/healthz` | GET | Health check, returns "ok" |
| `/-/reload` | POST | Trigger config hot reload |
| `/api/v1/targets` | GET | Current status of all probe targets (JSON) |
| `/version` | GET | Version info (JSON) |

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.yaml` | Config file path |
| `--watch-config` | `false` | Watch config file for changes and auto-reload |
| `--validate` | `false` | Validate config and exit |
| `--version` | `false` | Print version and exit |

## License

MIT
